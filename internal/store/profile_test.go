package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store/storetest"
)

func TestListAndRevokeAgents(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	ws1, _, _ := s.CreateWorkspace(ctx, "One", "", "")
	ws2, _, _ := s.CreateWorkspace(ctx, "Two", "", "")
	alex, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "1", Name: "Alex"})
	bob, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "2", Name: "Bob"})
	alexP, _, _, _, _ := s.JoinAsUser(ctx, ws1.ID, alex.ID, "Alex", "")
	s.JoinAsUser(ctx, ws2.ID, alex.ID, "Alex", "")
	s.JoinAsUser(ctx, ws1.ID, bob.ID, "Bob", "")
	join := func(ws string, sponsor int64, name string) (*Participant, string) {
		t.Helper()
		pass, _, err := s.MintPass(ctx, ws, sponsor)
		if err != nil {
			t.Fatal(err)
		}
		p, tok, _, err := s.JoinWithPass(ctx, ws, pass, name, "")
		if err != nil {
			t.Fatal(err)
		}
		return p, tok
	}
	quill, quillTok := join(ws1.ID, alex.ID, "Quill")
	_, larkTok := join(ws2.ID, alex.ID, "Lark")
	join(ws1.ID, bob.ID, "Wren")
	legacy, _, _, _ := s.Join(ctx, ws1.ID, "Old", "")

	agents, err := s.ListMyAgents(ctx, alex.ID)
	if err != nil || len(agents) != 2 {
		t.Fatalf("ListMyAgents = %+v, %v (want Quill and Lark only)", agents, err)
	}
	names := map[string]string{}
	for _, a := range agents {
		names[a.Name] = a.WorkspaceName
	}
	if names["Quill"] != "One" || names["Lark"] != "Two" {
		t.Errorf("agents by workspace = %v", names)
	}

	if _, _, err := s.RevokeAgent(ctx, bob.ID, quill.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("another member revoking err = %v, want ErrForbidden (only the sponsor)", err)
	}
	if _, _, err := s.RevokeAgent(ctx, alex.ID, legacy.ID); !errors.Is(err, ErrInvalid) {
		t.Errorf("revoking a non-agent err = %v", err)
	}
	if _, _, err := s.RevokeAgent(ctx, alex.ID, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoking an unknown participant err = %v", err)
	}
	p, evs, err := s.RevokeAgent(ctx, alex.ID, quill.ID)
	if err != nil || p.RevokedAt == 0 || !sameTypes(evs, "participant.revoked") || evs[0].ActorID != alexP.ID || evs[0].WorkspaceID != ws1.ID {
		t.Fatalf("revoke = %+v %v %v", p, evs, err)
	}
	if _, err := s.Authenticate(ctx, quillTok); !errors.Is(err, ErrRevoked) || !strings.Contains(err.Error(), "Alex revoked this agent credential") {
		t.Errorf("revoked agent err = %v", err)
	}
	if _, evs, err := s.RevokeAgent(ctx, alex.ID, quill.ID); err != nil || len(evs) != 0 {
		t.Errorf("revoking twice = %v %v, want a no-op", evs, err)
	}
	if _, err := s.Authenticate(ctx, larkTok); err != nil {
		t.Errorf("other agent after a revoke: %v", err)
	}
	// Revoked agents stay listed, marked.
	agents, _ = s.ListMyAgents(ctx, alex.ID)
	for _, a := range agents {
		if (a.Name == "Quill") != (a.RevokedAt != 0) {
			t.Errorf("agent %s revoked_at = %d", a.Name, a.RevokedAt)
		}
	}
}

func TestLinkIdentity(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	alex, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "1", Name: "Alex", Email: "alex@example.com", EmailVerified: true})
	bob, _ := s.LoginWithProvider(ctx, "google", Profile{Subject: "g-bob", Name: "Bob"})

	u, err := s.LinkIdentity(ctx, alex.ID, "google", Profile{Subject: "g-alex", Email: "alex@gmail.example", EmailVerified: true})
	if err != nil || len(u.Identities) != 2 || u.Identities[1].Provider != "google" {
		t.Fatalf("link = %+v, %v", u, err)
	}
	if u, err := s.LinkIdentity(ctx, alex.ID, "google", Profile{Subject: "g-alex", Email: "new@gmail.example", EmailVerified: true}); err != nil || len(u.Identities) != 2 || u.Identities[1].Email != "new@gmail.example" {
		t.Errorf("relink = %+v, %v (a refresh)", u, err)
	}
	if _, err := s.LinkIdentity(ctx, alex.ID, "google", Profile{Subject: "g-bob"}); !errors.Is(err, ErrConflict) {
		t.Errorf("linking Bob's identity err = %v, want ErrConflict", err)
	}
	if b, _ := s.GetUser(ctx, bob.ID); len(b.Identities) != 1 {
		t.Errorf("Bob lost his identity: %+v", b)
	}
	// Signing in with the linked identity now reaches Alex.
	if got, _ := s.LoginWithProvider(ctx, "google", Profile{Subject: "g-alex"}); got.ID != alex.ID {
		t.Errorf("sign-in with the linked identity = user %d, want %d", got.ID, alex.ID)
	}
	if _, err := s.LinkIdentity(ctx, alex.ID, "github", Profile{}); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty subject err = %v", err)
	}
}

func TestRevokeOtherSessions(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	alex, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "1"})
	bob, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "2"})
	keep, kept, _ := s.CreateSession(ctx, alex.ID, SessionBrowser, "this one")
	other1, _, _ := s.CreateSession(ctx, alex.ID, SessionBrowser, "laptop")
	other2, _, _ := s.CreateSession(ctx, alex.ID, SessionCLI, "cli")
	bobTok, _, _ := s.CreateSession(ctx, bob.ID, SessionBrowser, "")
	n, err := s.RevokeOtherSessions(ctx, alex.ID, kept.ID)
	if err != nil || n != 2 {
		t.Fatalf("RevokeOtherSessions = %d, %v", n, err)
	}
	if _, _, err := s.SessionByToken(ctx, keep); err != nil {
		t.Errorf("the kept session: %v", err)
	}
	for _, tok := range []string{other1, other2} {
		if _, _, err := s.SessionByToken(ctx, tok); !errors.Is(err, ErrNotFound) {
			t.Errorf("revoked session err = %v", err)
		}
	}
	if _, _, err := s.SessionByToken(ctx, bobTok); err != nil {
		t.Errorf("another user's session was revoked: %v", err)
	}
}

// TestProfileColumnsMigration opens a database from before #18
// (participants without revoked_at, oauth_states without link_user_id), as
// on production: the columns are added once, an existing agent keeps
// working until revoked, and later boots are no-ops.
func TestProfileColumnsMigration(t *testing.T) {
	dsn := storetest.DSN(t)
	ctx := context.Background()
	raw, err := openRaw(dsn)
	if err != nil {
		t.Fatal(err)
	}
	id := map[bool]string{true: "BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY", false: "INTEGER PRIMARY KEY AUTOINCREMENT"}[raw.pg]
	for _, q := range []string{
		`CREATE TABLE workspaces (id TEXT PRIMARY KEY, name TEXT NOT NULL, repo_url TEXT NOT NULL DEFAULT '', canvas_url TEXT NOT NULL DEFAULT '', created_at BIGINT NOT NULL)`,
		`CREATE TABLE users (id ` + id + `, display_name TEXT NOT NULL DEFAULT '', avatar_url TEXT NOT NULL DEFAULT '', created_at BIGINT NOT NULL)`,
		`CREATE TABLE memberships (id ` + id + `, workspace_id TEXT NOT NULL REFERENCES workspaces(id), user_id BIGINT NOT NULL REFERENCES users(id), created_at BIGINT NOT NULL, UNIQUE (workspace_id, user_id))`,
		`CREATE TABLE participants (id ` + id + `, workspace_id TEXT NOT NULL REFERENCES workspaces(id), name TEXT NOT NULL, emoji TEXT NOT NULL, status TEXT NOT NULL DEFAULT '',
			token TEXT NOT NULL UNIQUE, created_at BIGINT NOT NULL, last_seen_at BIGINT NOT NULL, user_id BIGINT REFERENCES users(id),
			sponsor_user_id BIGINT REFERENCES users(id), token_expires_at BIGINT)`,
		`CREATE TABLE oauth_states (state_hash TEXT PRIMARY KEY, provider TEXT NOT NULL, verifier TEXT NOT NULL, return_to TEXT NOT NULL, expires_at BIGINT NOT NULL)`,
		`INSERT INTO workspaces (id, name, created_at) VALUES ('ws_legacy', 'Legacy', 1)`,
		`INSERT INTO users (display_name, created_at) VALUES ('Alex', 1)`,
		`INSERT INTO memberships (workspace_id, user_id, created_at) VALUES ('ws_legacy', 1, 1)`,
		`INSERT INTO participants (workspace_id, name, emoji, token, created_at, last_seen_at, sponsor_user_id, token_expires_at)
			VALUES ('ws_legacy', 'Quill', '🪶', 'agenttoken', 1, 1, 1, 99999999999999)`,
	} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	raw.Close()

	for boot := 1; boot <= 3; boot++ {
		s, err := Open(dsn)
		if err != nil {
			t.Fatalf("boot %d: %v", boot, err)
		}
		p, err := s.Authenticate(ctx, "agenttoken")
		switch boot {
		case 1:
			if err != nil || p.RevokedAt != 0 {
				t.Fatalf("boot 1: agent = %+v %v", p, err)
			}
			if err := s.SaveOAuthState(ctx, "st", OAuthState{Provider: "github", LinkUserID: 1}); err != nil {
				t.Fatalf("link state after migration: %v", err)
			}
			if st, err := s.TakeOAuthState(ctx, "st"); err != nil || st.LinkUserID != 1 {
				t.Errorf("link state = %+v %v", st, err)
			}
		case 2:
			if _, _, err := s.RevokeAgent(ctx, 1, p.ID); err != nil {
				t.Fatalf("revoke after migration: %v", err)
			}
		case 3:
			if !errors.Is(err, ErrRevoked) {
				t.Errorf("boot 3: revoked agent err = %v (the revoke must survive reboots)", err)
			}
		}
		s.Close()
	}
}
