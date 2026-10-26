package store

import (
	"context"
	"errors"
	"testing"

	"github.com/digitalex/unison/internal/store/storetest"
)

func TestWorkspaceRepos(t *testing.T) {
	s, _, a, _ := setup(t) // Created with repo https://github.com/digitalex/unison.
	ctx := context.Background()
	ws, _ := s.GetWorkspace(ctx, a.WorkspaceID)
	if len(ws.Repos) != 1 || ws.RepoURL != "https://github.com/digitalex/unison" || ws.Repos[0].Position != 1 {
		t.Fatalf("created workspace repos = %+v, repo_url %q", ws.Repos, ws.RepoURL)
	}

	web, evs, err := s.AddRepo(ctx, a, RepoInput{URL: " https://github.com/digitalex/unison-web "})
	if err != nil || web.URL != "https://github.com/digitalex/unison-web" || web.Position != 2 || len(evs) != 1 || evs[0].Type != "repo.added" {
		t.Fatalf("AddRepo = %+v, %+v, %v", web, evs, err)
	}
	if _, _, err := s.AddRepo(ctx, a, RepoInput{URL: "https://github.com/digitalex/unison-web"}); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate repo err = %v, want ErrConflict", err)
	}
	for _, bad := range []string{"", "git@github.com:x/y.git", "github.com/x/y"} {
		if _, _, err := s.AddRepo(ctx, a, RepoInput{URL: bad}); !errors.Is(err, ErrInvalid) {
			t.Errorf("AddRepo(%q) err = %v, want ErrInvalid", bad, err)
		}
	}

	backend := ws.Repos[0]
	got, evs, err := s.EditRepo(ctx, a, backend.ID, RepoPatch{URL: ptr("https://github.com/digitalex/unison-api")})
	if err != nil || got.URL != "https://github.com/digitalex/unison-api" || evs[0].Type != "repo.updated" {
		t.Fatalf("EditRepo = %+v, %v", got, err)
	}
	if ws, _ = s.GetWorkspace(ctx, a.WorkspaceID); ws.RepoURL != "https://github.com/digitalex/unison-api" {
		t.Errorf("repo_url = %q, want the edited first repo", ws.RepoURL)
	}
	if _, _, err := s.EditRepo(ctx, a, backend.ID, RepoPatch{URL: ptr(web.URL)}); !errors.Is(err, ErrConflict) {
		t.Errorf("edit onto another repo's URL err = %v, want ErrConflict", err)
	}

	// Removing the first repo promotes the next one to repo_url.
	if evs, err := s.RemoveRepo(ctx, a, backend.ID); err != nil || evs[0].Type != "repo.removed" {
		t.Fatalf("RemoveRepo = %+v, %v", evs, err)
	}
	repos, _ := s.ListRepos(ctx, a.WorkspaceID)
	ws, _ = s.GetWorkspace(ctx, a.WorkspaceID)
	if len(repos) != 1 || repos[0].Position != 1 || ws.RepoURL != web.URL {
		t.Errorf("after remove: repos %+v, repo_url %q", repos, ws.RepoURL)
	}
	if _, err := s.RemoveRepo(ctx, a, backend.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing twice err = %v, want ErrNotFound", err)
	}
}

func TestCreateWorkspaceWithRepos(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	ws, _, err := s.CreateWorkspace(ctx, "Multi", "https://github.com/acme/api", "",
		RepoInput{URL: "https://github.com/acme/web"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ws.Repos) != 2 || ws.Repos[0].URL != "https://github.com/acme/api" || ws.Repos[1].URL != "https://github.com/acme/web" || ws.RepoURL != "https://github.com/acme/api" {
		t.Errorf("workspace = %+v", ws)
	}
	if _, _, err := s.CreateWorkspace(ctx, "Bad", "not a url", ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("invalid repo_url err = %v, want ErrInvalid", err)
	}
	list, _ := s.ListWorkspaces(ctx)
	if len(list) != 1 || len(list[0].Repos) != 2 {
		t.Errorf("ListWorkspaces = %+v (the invalid create must leave nothing behind)", list)
	}
}

func TestLegacyRepoURLPatch(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()
	s.AddRepo(ctx, a, RepoInput{URL: "https://github.com/digitalex/unison-web"})
	ws, evs, err := s.UpdateWorkspace(ctx, a.WorkspaceID, a.ID, WorkspacePatch{RepoURL: ptr("https://github.com/digitalex/unison-api")})
	if err != nil || ws.RepoURL != "https://github.com/digitalex/unison-api" || len(ws.Repos) != 2 || ws.Repos[1].URL != "https://github.com/digitalex/unison-web" {
		t.Fatalf("PATCH repo_url = %+v, %v", ws, err)
	}
	if len(evs) != 2 || evs[0].Type != "repo.updated" || evs[1].Type != "workspace.updated" {
		t.Errorf("events = %+v", evs)
	}
	ws, _, _ = s.UpdateWorkspace(ctx, a.WorkspaceID, a.ID, WorkspacePatch{RepoURL: ptr("")})
	if len(ws.Repos) != 1 || ws.RepoURL != "https://github.com/digitalex/unison-web" {
		t.Errorf("clearing repo_url removes the first repo: %+v", ws)
	}
}

// TestRepoURLMigration simulates a database from before workspace_repos
// (repo_url set, no repo rows) and reopens it: the URL is migrated once, and
// later boots neither duplicate it nor resurrect a removed repo.
func TestRepoURLMigration(t *testing.T) {
	dsn := storetest.DSN(t)
	ctx := context.Background()
	s, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	legacy, _, _ := s.CreateWorkspace(ctx, "Legacy", "", "")
	empty, _, _ := s.CreateWorkspace(ctx, "No repo", "", "")
	// Before this change: repo_url set and no workspace_repos table at all.
	if _, err := s.db.ExecContext(ctx, `UPDATE workspaces SET repo_url = ? WHERE id = ?`, "https://github.com/acme/legacy", legacy.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `DROP TABLE workspace_repos`); err != nil {
		t.Fatal(err)
	}
	s.Close()

	for boot := 1; boot <= 3; boot++ {
		s, err = Open(dsn)
		if err != nil {
			t.Fatalf("boot %d: %v", boot, err)
		}
		ws, _ := s.GetWorkspace(ctx, legacy.ID)
		if len(ws.Repos) != 1 || ws.Repos[0].URL != "https://github.com/acme/legacy" || ws.Repos[0].Position != 1 || ws.RepoURL != ws.Repos[0].URL {
			t.Fatalf("boot %d: legacy workspace = %+v", boot, ws)
		}
		if ws, _ := s.GetWorkspace(ctx, empty.ID); len(ws.Repos) != 0 || ws.RepoURL != "" {
			t.Fatalf("boot %d: workspace without repo_url = %+v", boot, ws)
		}
		s.Close()
	}

	// Remove the migrated repo: repo_url is cleared and stays empty on reboot.
	s, _ = Open(dsn)
	p, _, _, _ := s.Join(ctx, legacy.ID, "Ada", "")
	ws, _ := s.GetWorkspace(ctx, legacy.ID)
	if _, err := s.RemoveRepo(ctx, p, ws.Repos[0].ID); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, _ = Open(dsn)
	defer s.Close()
	if ws, _ := s.GetWorkspace(ctx, legacy.ID); len(ws.Repos) != 0 || ws.RepoURL != "" {
		t.Errorf("removed repo came back after a reboot: %+v", ws)
	}
}
