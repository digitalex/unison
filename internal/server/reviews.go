package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Pending review (#127): the open pull requests in a workspace's GitHub
// repositories that wait for a person, i.e. those not set to auto-merge. The
// server reads them with the workspace's GitHub token (a PAT pasted in
// Settings; see store.GitHubToken), so the token never reaches clients.

// githubAPI is GitHub's REST API (tests point it at a fake).
var githubAPI = "https://api.github.com"

// reviewsTTL is how long a workspace's list is reused, to stay well within
// GitHub's rate limit however many tabs poll it.
const reviewsTTL = time.Minute

var githubClient = &http.Client{Timeout: 10 * time.Second}

// PendingPR is an open pull request that needs a review.
type PendingPR struct {
	Repo      string `json:"repo"` // owner/name
	Number    int64  `json:"number"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Author    string `json:"author"`
	Draft     bool   `json:"draft"`
	CreatedAt int64  `json:"created_at"`
	// MergeState (#134) is whether it can be merged: MergeReady, or why not.
	MergeState string `json:"merge_state"`
}

// Merge states of a pending pull request (#134).
const (
	MergeReady    = "ready"          // GitHub would merge it now
	MergeConflict = "conflict"       // it conflicts with its base branch
	ChecksFailing = "checks_failing" // a check failed, or something else blocks it
	ChecksPending = "checks_pending" // checks are still running
	MergeUnknown  = "unknown"        // GitHub is still working it out (or it's a draft)
)

// RepoError is why a repository's pull requests couldn't be read.
type RepoError struct {
	Repo  string `json:"repo"`
	Error string `json:"error"`
}

// PendingReviews is GET /api/workspaces/{ws}/reviews. Configured is false
// when the workspace has no GitHub token.
type PendingReviews struct {
	Configured bool        `json:"configured"`
	Pulls      []PendingPR `json:"pulls"`
	Errors     []RepoError `json:"errors"`
}

// reviewCache keeps each workspace's last list for reviewsTTL. An entry is
// only reused for the same token and repositories.
type reviewCache struct {
	mu      sync.Mutex
	entries map[string]reviewEntry
}

type reviewEntry struct {
	key string
	at  time.Time
	out PendingReviews
}

func (c *reviewCache) get(ws, key string) (PendingReviews, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[ws]
	return e.out, ok && e.key == key && time.Since(e.at) < reviewsTTL
}

func (c *reviewCache) put(ws, key string, out PendingReviews) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]reviewEntry{}
	}
	c.entries[ws] = reviewEntry{key: key, at: time.Now(), out: out}
}

func (s *Server) listReviews(w http.ResponseWriter, r *http.Request) {
	ws := r.PathValue("ws")
	token, err := s.store.GitHubToken(r.Context(), ws)
	if err != nil || token == "" {
		s.respond(w, http.StatusOK, PendingReviews{Pulls: []PendingPR{}, Errors: []RepoError{}}, nil, err)
		return
	}
	repos, err := s.store.ListRepos(r.Context(), ws)
	if err != nil {
		s.respond(w, http.StatusOK, nil, nil, err)
		return
	}
	var slugs []string
	for _, repo := range repos {
		if slug := githubSlug(repo.URL); slug != "" {
			slugs = append(slugs, slug)
		}
	}
	key := token + "\n" + strings.Join(slugs, "\n")
	if out, ok := s.reviews.get(ws, key); ok {
		s.respond(w, http.StatusOK, out, nil, nil)
		return
	}
	out := fetchPendingReviews(r.Context(), token, slugs)
	s.reviews.put(ws, key, out)
	s.respond(w, http.StatusOK, out, nil, nil)
}

// githubSlug is "owner/name" for a GitHub repository URL, "" for others.
func githubSlug(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Host, "github.com") {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return parts[0] + "/" + strings.TrimSuffix(parts[1], ".git")
}

// fetchPendingReviews reads every repository's open pull requests in
// parallel and lists them oldest first. A repository that can't be read is
// reported in Errors; the others still show.
func fetchPendingReviews(ctx context.Context, token string, slugs []string) PendingReviews {
	out := PendingReviews{Configured: true, Pulls: []PendingPR{}, Errors: []RepoError{}}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, slug := range slugs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pulls, err := openPulls(ctx, token, slug)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				out.Errors = append(out.Errors, RepoError{Repo: slug, Error: err.Error()})
				return
			}
			out.Pulls = append(out.Pulls, pulls...)
		}()
	}
	wg.Wait()
	sort.Slice(out.Pulls, func(i, j int) bool { return out.Pulls[i].CreatedAt < out.Pulls[j].CreatedAt })
	sort.Slice(out.Errors, func(i, j int) bool { return out.Errors[i].Repo < out.Errors[j].Repo })
	return out
}

// githubGet GETs path from GitHub's API with the token.
func githubGet(ctx context.Context, token, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", githubAPI+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return githubClient.Do(req)
}

// githubJSON GETs path and decodes a 200 answer into out.
func githubJSON(ctx context.Context, token, path string, out any) error {
	resp, err := githubGet(ctx, token, path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub said %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// openPulls lists a repository's open pull requests that aren't set to
// auto-merge (up to 100, GitHub's page size), with their merge state.
func openPulls(ctx context.Context, token, slug string) ([]PendingPR, error) {
	resp, err := githubGet(ctx, token, "/repos/"+slug+"/pulls?state=open&per_page=100")
	if err != nil {
		return nil, fmt.Errorf("GitHub is unreachable: %v", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, fmt.Errorf("GitHub rejected the token (expired or revoked?)")
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("the token can't read this repository's pull requests (GitHub said %d)", resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("GitHub said %s", resp.Status)
	}
	var pulls []struct {
		Number    int64           `json:"number"`
		Title     string          `json:"title"`
		HTMLURL   string          `json:"html_url"`
		Draft     bool            `json:"draft"`
		CreatedAt time.Time       `json:"created_at"`
		AutoMerge json.RawMessage `json:"auto_merge"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pulls); err != nil {
		return nil, fmt.Errorf("reading GitHub's answer: %v", err)
	}
	out := []PendingPR{}
	for _, p := range pulls {
		if len(p.AutoMerge) > 0 && string(p.AutoMerge) != "null" {
			continue
		}
		out = append(out, PendingPR{Repo: slug, Number: p.Number, Title: p.Title, URL: p.HTMLURL, Author: p.User.Login, Draft: p.Draft, CreatedAt: p.CreatedAt.UnixMilli()})
	}
	var wg sync.WaitGroup
	for i := range out {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i].MergeState = mergeState(ctx, token, slug, out[i].Number)
		}()
	}
	wg.Wait()
	return out, nil
}

// mergeState asks GitHub whether pull request n can be merged. The list
// doesn't say, so this reads the pull request, and its check runs when
// something blocks it. Anything unreadable is MergeUnknown.
func mergeState(ctx context.Context, token, slug string, n int64) string {
	var pr struct {
		MergeableState string `json:"mergeable_state"`
		Head           struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if githubJSON(ctx, token, fmt.Sprintf("/repos/%s/pulls/%d", slug, n), &pr) != nil {
		return MergeUnknown
	}
	switch pr.MergeableState {
	case "clean", "has_hooks":
		return MergeReady
	case "dirty":
		return MergeConflict
	case "blocked", "unstable", "behind":
		return checksState(ctx, token, slug, pr.Head.SHA)
	}
	return MergeUnknown // "unknown" (GitHub computes it lazily), "draft"
}

// checksState is ChecksPending while a check on sha runs and none has
// failed, else ChecksFailing (also when the checks can't be read: the pull
// request is blocked either way).
func checksState(ctx context.Context, token, slug, sha string) string {
	var runs struct {
		CheckRuns []struct {
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"check_runs"`
	}
	if sha == "" || githubJSON(ctx, token, "/repos/"+slug+"/commits/"+sha+"/check-runs?per_page=100", &runs) != nil {
		return ChecksFailing
	}
	running := false
	for _, r := range runs.CheckRuns {
		switch {
		case r.Status != "completed":
			running = true
		case r.Conclusion != "success" && r.Conclusion != "neutral" && r.Conclusion != "skipped":
			return ChecksFailing
		}
	}
	if running {
		return ChecksPending
	}
	return ChecksFailing
}
