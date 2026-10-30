package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

func TestReviewsAPI(t *testing.T) {
	var calls atomic.Int32
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer ghp_secret" {
			http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/repos/acme/api/pulls":
			w.Write([]byte(`[
				{"number": 7, "title": "Needs a look", "html_url": "https://github.com/acme/api/pull/7", "draft": false,
				 "created_at": "2026-10-28T10:00:00Z", "auto_merge": null, "user": {"login": "ada"}},
				{"number": 8, "title": "Merges itself", "html_url": "https://github.com/acme/api/pull/8", "draft": false,
				 "created_at": "2026-10-27T10:00:00Z", "auto_merge": {"merge_method": "squash"}, "user": {"login": "ada"}}]`))
		case "/repos/acme/web/pulls":
			w.Write([]byte(`[{"number": 3, "title": "Older one", "html_url": "https://github.com/acme/web/pull/3", "draft": true,
				"created_at": "2026-10-20T10:00:00Z", "user": {"login": "bo"}}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer gh.Close()
	defer func(old string) { githubAPI = old }(githubAPI)
	githubAPI = gh.URL

	ts, st := newServer(t)
	u, _ := st.LoginWithProvider(t.Context(), "github", store.Profile{Subject: "ada", Name: "Ada"})
	userTok, _, _ := st.CreateSession(t.Context(), u.ID, store.SessionCLI, "")
	_, ws := call(t, "POST", ts.URL+"/api/workspaces", userTok,
		`{"name":"Multi","repo_url":"https://github.com/acme/api","repos":[{"url":"https://github.com/acme/web.git"},{"url":"https://github.com/acme/gone"},{"url":"https://gitlab.com/acme/other"}]}`)
	wsID := ws["id"].(string)
	_, token, _, _, _ := st.JoinAsUser(t.Context(), wsID, u.ID, "Ada", "")
	wsURL := ts.URL + "/api/workspaces/" + wsID

	// No token: not configured, and GitHub isn't asked.
	if status, got := call(t, "GET", wsURL+"/reviews", token, ""); status != http.StatusOK || got["configured"] != false {
		t.Fatalf("without a token = %d %v", status, got)
	}
	if ws["has_github_token"] != false || calls.Load() != 0 {
		t.Fatalf("has_github_token = %v, GitHub calls = %d", ws["has_github_token"], calls.Load())
	}

	// Setting it: write-only (never in a response or an event).
	if status, _ := call(t, "PATCH", wsURL, "", `{"github_token":"ghp_secret"}`); status != http.StatusUnauthorized {
		t.Errorf("unauthenticated set = %d, want 401", status)
	}
	status, got := call(t, "PATCH", wsURL, token, `{"github_token":" ghp_secret "}`)
	if status != http.StatusOK || got["has_github_token"] != true {
		t.Fatalf("set token = %d %v", status, got)
	}
	if _, body := getAs(t, wsURL, token); strings.Contains(body, "ghp_secret") {
		t.Errorf("the workspace leaks the token: %s", body)
	}
	if _, body := getAs(t, wsURL+"/events?after=0", token); !strings.Contains(body, "workspace.updated") || strings.Contains(body, "ghp_secret") {
		t.Errorf("events: want workspace.updated without the token: %s", body)
	}

	status, got = call(t, "GET", wsURL+"/reviews", token, "")
	pulls, _ := got["pulls"].([]any)
	errs, _ := got["errors"].([]any)
	if status != http.StatusOK || got["configured"] != true || len(pulls) != 2 {
		t.Fatalf("reviews = %d %v", status, got)
	}
	first, second := pulls[0].(map[string]any), pulls[1].(map[string]any)
	if first["repo"] != "acme/web" || first["number"] != 3.0 || first["draft"] != true || second["url"] != "https://github.com/acme/api/pull/7" || second["author"] != "ada" {
		t.Errorf("pulls = %v, want web#3 (older) then api#7, without auto-merge #8", pulls)
	}
	if len(errs) != 1 || errs[0].(map[string]any)["repo"] != "acme/gone" {
		t.Errorf("errors = %v, want acme/gone (404); gitlab skipped", errs)
	}

	// Cached for a minute: no new GitHub calls.
	before := calls.Load()
	call(t, "GET", wsURL+"/reviews", token, "")
	if calls.Load() != before {
		t.Errorf("second read called GitHub again (%d → %d)", before, calls.Load())
	}

	// A new token is a new key; a bad one is reported per repository.
	call(t, "PATCH", wsURL, token, `{"github_token":"ghp_wrong"}`)
	_, got = call(t, "GET", wsURL+"/reviews", token, "")
	if errs, _ := got["errors"].([]any); len(errs) != 3 || !strings.Contains(errs[0].(map[string]any)["error"].(string), "rejected the token") {
		t.Errorf("bad token: %v", got)
	}

	// Removing it.
	if _, got := call(t, "PATCH", wsURL, token, `{"github_token":""}`); got["has_github_token"] != false {
		t.Errorf("remove token: %v", got)
	}
	if _, got := call(t, "GET", wsURL+"/reviews", token, ""); got["configured"] != false {
		t.Errorf("after removing: %v", got)
	}
}

func TestGitHubSlug(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/digitalex/unison":      "digitalex/unison",
		"https://github.com/digitalex/unison.git/": "digitalex/unison",
		"https://GitHub.com/a/b/tree/main":         "a/b",
		"https://gitlab.com/a/b":                   "",
		"https://github.com/a":                     "",
	} {
		if got := githubSlug(in); got != want {
			t.Errorf("githubSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
