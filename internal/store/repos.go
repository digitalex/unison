package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
)

// Repo is a code repository connected to a workspace. A workspace can have
// several (e.g. backend and frontend); the first (lowest Position) is also
// exposed as the workspace's legacy repo_url.
type Repo struct {
	ID          int64  `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	URL         string `json:"url"`
	Label       string `json:"label"`
	Position    int64  `json:"position"`
	CreatedAt   int64  `json:"created_at"`
}

// RepoInput holds the fields for adding a repository.
type RepoInput struct {
	URL   string `json:"url"`
	Label string `json:"label"`
}

// RepoPatch holds optional repository field updates.
type RepoPatch struct {
	URL   *string `json:"url"`
	Label *string `json:"label"`
}

// migrateRepoURLs copies a workspace's legacy repo_url into workspace_repos
// when the workspace has no repositories yet. It runs at every startup and is
// idempotent: once a workspace has any repo row it is skipped, and repo_url is
// kept in sync with the first repo (syncRepoURL), so removing every repo also
// clears repo_url and nothing is resurrected on the next boot.
const migrateRepoURLs = `INSERT INTO workspace_repos (workspace_id, url, label, position, created_at)
SELECT w.id, w.repo_url, '', 1, w.created_at FROM workspaces w
WHERE w.repo_url <> '' AND NOT EXISTS (SELECT 1 FROM workspace_repos r WHERE r.workspace_id = w.id)`

const repoCols = `id, workspace_id, url, label, position, created_at`

func scanRepos(rows *sql.Rows) ([]Repo, error) {
	defer rows.Close()
	out := []Repo{}
	for rows.Next() {
		var r Repo
		if err := rows.Scan(&r.ID, &r.WorkspaceID, &r.URL, &r.Label, &r.Position, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func listReposTx(t *tx, wsID string) ([]Repo, error) {
	rows, err := t.Query(`SELECT `+repoCols+` FROM workspace_repos WHERE workspace_id = ? ORDER BY position, id`, wsID)
	if err != nil {
		return nil, err
	}
	return scanRepos(rows)
}

func validRepoURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("%w: a repository URL must be an http(s) URL, got %q", ErrInvalid, raw)
	}
	return raw, nil
}

func repoConflict(err error, u string) error {
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: %s is already connected to this workspace", ErrConflict, u)
	}
	return err
}

// insertRepo appends a repository to a workspace (no event) and syncs
// repo_url.
func insertRepo(t *tx, wsID string, in RepoInput, now int64) (*Repo, error) {
	u, err := validRepoURL(in.URL)
	if err != nil {
		return nil, err
	}
	r := &Repo{WorkspaceID: wsID, URL: u, Label: strings.TrimSpace(in.Label), CreatedAt: now}
	if err := t.QueryRow(`SELECT COALESCE(MAX(position), 0) + 1 FROM workspace_repos WHERE workspace_id = ?`, wsID).Scan(&r.Position); err != nil {
		return nil, err
	}
	if r.ID, err = t.insert(`INSERT INTO workspace_repos (workspace_id, url, label, position, created_at) VALUES (?, ?, ?, ?, ?)`, "id",
		r.WorkspaceID, r.URL, r.Label, r.Position, r.CreatedAt); err != nil {
		return nil, repoConflict(err, u)
	}
	return r, syncRepoURL(t, wsID)
}

func getRepo(t *tx, wsID string, id int64) (*Repo, error) {
	rows, err := t.Query(`SELECT `+repoCols+` FROM workspace_repos WHERE workspace_id = ? AND id = ?`, wsID, id)
	if err != nil {
		return nil, err
	}
	list, err := scanRepos(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("repository %d: %w", id, ErrNotFound)
	}
	return &list[0], nil
}

func updateRepo(t *tx, r *Repo, p RepoPatch) error {
	if p.URL != nil {
		u, err := validRepoURL(*p.URL)
		if err != nil {
			return err
		}
		r.URL = u
	}
	if p.Label != nil {
		r.Label = strings.TrimSpace(*p.Label)
	}
	if _, err := t.Exec(`UPDATE workspace_repos SET url = ?, label = ? WHERE id = ?`, r.URL, r.Label, r.ID); err != nil {
		return repoConflict(err, r.URL)
	}
	return syncRepoURL(t, r.WorkspaceID)
}

// deleteRepo removes a repository and closes the gap in positions.
func deleteRepo(t *tx, r *Repo) error {
	if _, err := t.Exec(`DELETE FROM workspace_repos WHERE id = ?`, r.ID); err != nil {
		return err
	}
	rest, err := listReposTx(t, r.WorkspaceID)
	if err != nil {
		return err
	}
	for i, other := range rest {
		if _, err := t.Exec(`UPDATE workspace_repos SET position = ? WHERE id = ?`, i+1, other.ID); err != nil {
			return err
		}
	}
	return syncRepoURL(t, r.WorkspaceID)
}

// syncRepoURL keeps workspaces.repo_url equal to the first repository's URL
// ("" when there are none), for older clients and for the startup migration.
func syncRepoURL(t *tx, wsID string) error {
	_, err := t.Exec(`UPDATE workspaces SET repo_url = COALESCE(
		(SELECT url FROM workspace_repos WHERE workspace_id = ? ORDER BY position, id LIMIT 1), '') WHERE id = ?`, wsID, wsID)
	return err
}

// ListRepos returns a workspace's repositories in order.
func (s *Store) ListRepos(ctx context.Context, wsID string) ([]Repo, error) {
	if _, err := s.GetWorkspace(ctx, wsID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+repoCols+` FROM workspace_repos WHERE workspace_id = ? ORDER BY position, id`, wsID)
	if err != nil {
		return nil, err
	}
	return scanRepos(rows)
}

// AddRepo connects a repository to the actor's workspace and records
// repo.added.
func (s *Store) AddRepo(ctx context.Context, actor *Participant, in RepoInput) (*Repo, []Event, error) {
	var r *Repo
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var err error
		if r, err = insertRepo(t, actor.WorkspaceID, in, log.now); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "repo.added", actor.ID, r)
	})
	if err != nil {
		return nil, nil, err
	}
	return r, evs, nil
}

// EditRepo changes a repository's URL and/or label and records repo.updated.
func (s *Store) EditRepo(ctx context.Context, actor *Participant, id int64, p RepoPatch) (*Repo, []Event, error) {
	var r *Repo
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var err error
		if r, err = getRepo(t, actor.WorkspaceID, id); err != nil {
			return err
		}
		if err := updateRepo(t, r, p); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "repo.updated", actor.ID, r)
	})
	if err != nil {
		return nil, nil, err
	}
	return r, evs, nil
}

// RemoveRepo disconnects a repository and records repo.removed.
func (s *Store) RemoveRepo(ctx context.Context, actor *Participant, id int64) ([]Event, error) {
	return s.tx(ctx, func(t *tx, log *eventLog) error {
		r, err := getRepo(t, actor.WorkspaceID, id)
		if err != nil {
			return err
		}
		if err := deleteRepo(t, r); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "repo.removed", actor.ID, r)
	})
}

// setPrimaryRepoURL applies a legacy repo_url write: it edits the first
// repository, adds one if there are none, or removes the first when url is
// empty. It records the matching repo.* event.
func setPrimaryRepoURL(t *tx, log *eventLog, wsID string, actor int64, raw string) error {
	u := strings.TrimSpace(raw)
	repos, err := listReposTx(t, wsID)
	if err != nil {
		return err
	}
	switch {
	case u == "" && len(repos) > 0:
		if err := deleteRepo(t, &repos[0]); err != nil {
			return err
		}
		return log.add(wsID, "repo.removed", actor, repos[0])
	case u != "" && len(repos) == 0:
		r, err := insertRepo(t, wsID, RepoInput{URL: u}, log.now)
		if err != nil {
			return err
		}
		return log.add(wsID, "repo.added", actor, r)
	case u != "" && repos[0].URL != u:
		r := &repos[0]
		if err := updateRepo(t, r, RepoPatch{URL: &u}); err != nil {
			return err
		}
		return log.add(wsID, "repo.updated", actor, r)
	}
	return nil
}

// attachRepos fills in the Repos of workspaces.
func (s *Store) attachRepos(ctx context.Context, list []*Workspace) error {
	if len(list) == 0 {
		return nil
	}
	query, args := `SELECT `+repoCols+` FROM workspace_repos ORDER BY workspace_id, position, id`, []any{}
	if len(list) == 1 {
		query, args = `SELECT `+repoCols+` FROM workspace_repos WHERE workspace_id = ? ORDER BY position, id`, []any{list[0].ID}
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	repos, err := scanRepos(rows)
	if err != nil {
		return err
	}
	byID := map[string]*Workspace{}
	for _, w := range list {
		byID[w.ID] = w
	}
	for _, r := range repos {
		if w := byID[r.WorkspaceID]; w != nil {
			w.Repos = append(w.Repos, r)
		}
	}
	return nil
}

func getWorkspaceTx(t *tx, id string) (*Workspace, error) {
	w, err := scanWorkspace(t.QueryRow(`SELECT `+workspaceCols+` FROM workspaces WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	if w.Repos, err = listReposTx(t, id); err != nil {
		return nil, err
	}
	return w, nil
}
