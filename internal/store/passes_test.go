package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/digitalex/unison/internal/store/storetest"
)

// passSetup: a workspace with member Alex (and his participant), and Bob,
// signed in but not a member.
func passSetup(t *testing.T) (s *Store, now *time.Time, ws *Workspace, alex, bob *User) {
	t.Helper()
	s, now = newStore(t)
	ctx := context.Background()
	ws, _, _ = s.CreateWorkspace(ctx, "W", "", "")
	alex, _ = s.LoginWithProvider(ctx, "github", Profile{Subject: "1", Name: "Alex"})
	bob, _ = s.LoginWithProvider(ctx, "github", Profile{Subject: "2", Name: "Bob"})
	if _, _, _, _, err := s.JoinAsUser(ctx, ws.ID, alex.ID, "Alex", "🧑"); err != nil {
		t.Fatal(err)
	}
	return s, now, ws, alex, bob
}

func wantForbidden(t *testing.T, what string, err error, reason string) {
	t.Helper()
	if !errors.Is(err, ErrForbidden) || !strings.Contains(err.Error(), reason) {
		t.Errorf("%s: err = %v, want ErrForbidden mentioning %q", what, err, reason)
	}
}

func TestMintAndExchangePass(t *testing.T) {
	s, now, ws, alex, bob := passSetup(t)
	ctx := context.Background()
	if _, _, err := s.MintPass(ctx, ws.ID, bob.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("non-member mint err = %v", err)
	}
	if _, _, err := s.MintPass(ctx, "ws_missing", alex.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown workspace mint err = %v", err)
	}
	pass, expires, err := s.MintPass(ctx, ws.ID, alex.ID)
	if err != nil || !strings.HasPrefix(pass, AgentPassPrefix) || expires != now.UnixMilli()+AgentPassTTL.Milliseconds() {
		t.Fatalf("MintPass = %q %d %v", pass, expires, err)
	}
	var stored int
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_passes WHERE pass_hash = ?`, pass).Scan(&stored)
	if stored != 0 {
		t.Error("the raw pass is stored")
	}

	p, token, evs, err := s.JoinWithPass(ctx, ws.ID, pass, "Quill", "🪶")
	if err != nil {
		t.Fatal(err)
	}
	if p.Sponsor == nil || p.Sponsor.UserID != alex.ID || p.Sponsor.DisplayName != "Alex" || p.UserID != 0 ||
		p.TokenExpiresAt != now.UnixMilli()+AgentCredentialTTL.Milliseconds() || !sameTypes(evs, "participant.joined") {
		t.Fatalf("agent = %+v, events %v", p, eventTypes(evs))
	}
	if !strings.Contains(string(evs[0].Data), `"sponsor":{"user_id"`) {
		t.Errorf("participant.joined data lacks the sponsor: %s", evs[0].Data)
	}
	if a, err := s.Authenticate(ctx, token); err != nil || a.ID != p.ID || a.Sponsor.DisplayName != "Alex" {
		t.Errorf("Authenticate agent = %+v %v", a, err)
	}
	// Agents are never members or folded into their sponsor.
	m, _ := s.ListMembers(ctx, ws.ID)
	if len(m.Members) != 1 || len(m.Members[0].Participants) != 1 || len(m.Unlinked) != 1 || m.Unlinked[0].Sponsor == nil {
		t.Errorf("members = %+v", m)
	}

	_, _, _, err = s.JoinWithPass(ctx, ws.ID, pass, "Again", "")
	wantForbidden(t, "reused pass", err, "already used")
	_, _, _, err = s.JoinWithPass(ctx, ws.ID, "up_unknown", "X", "")
	wantForbidden(t, "unknown pass", err, "unknown pass")

	other, _, _ := s.CreateWorkspace(ctx, "Other", "", "")
	wrong, _, _ := s.MintPass(ctx, ws.ID, alex.ID)
	_, _, _, err = s.JoinWithPass(ctx, other.ID, wrong, "X", "")
	wantForbidden(t, "wrong workspace", err, "another workspace")
	// A failed exchange does not consume the pass, nor leave a participant.
	if _, _, _, err := s.JoinWithPass(ctx, ws.ID, wrong, "Lark", ""); err != nil {
		t.Errorf("pass after a wrong-workspace attempt: %v", err)
	}
	if list, _ := s.ListParticipants(ctx, other.ID); len(list) != 0 {
		t.Errorf("participants left in the other workspace: %+v", list)
	}

	late, _, _ := s.MintPass(ctx, ws.ID, alex.ID)
	*now = now.Add(AgentPassTTL + time.Second)
	_, _, _, err = s.JoinWithPass(ctx, ws.ID, late, "X", "")
	wantForbidden(t, "expired pass", err, "expired")

	orphan, _, _ := s.MintPass(ctx, ws.ID, alex.ID)
	s.JoinAsUser(ctx, ws.ID, bob.ID, "Bob", "")
	if _, err := s.RemoveMember(ctx, ws.ID, bob.ID, alex.ID); err != nil {
		t.Fatal(err)
	}
	_, _, _, err = s.JoinWithPass(ctx, ws.ID, orphan, "X", "")
	wantForbidden(t, "sponsor removed before exchange", err, "no longer in the workspace")
	if _, _, err := s.MintPass(ctx, ws.ID, alex.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("removed member mint err = %v", err)
	}
}

func TestAgentCredential(t *testing.T) {
	s, now, ws, alex, bob := passSetup(t)
	ctx := context.Background()
	_, legacyToken, _, _ := s.Join(ctx, ws.ID, "Old", "")
	pass, _, _ := s.MintPass(ctx, ws.ID, alex.ID)
	agent, token, _, _ := s.JoinWithPass(ctx, ws.ID, pass, "Quill", "🪶")

	// Each use (Touch) slides the expiry: 80 + 80 days of steady use is fine.
	for i := 0; i < 2; i++ {
		*now = now.Add(80 * 24 * time.Hour)
		if _, err := s.Authenticate(ctx, token); err != nil {
			t.Fatalf("after %d×80 days of use: %v", i+1, err)
		}
		s.Touch(ctx, agent.ID)
	}
	if a, _ := s.Authenticate(ctx, token); a.TokenExpiresAt != now.UnixMilli()+AgentCredentialTTL.Milliseconds() {
		t.Errorf("expiry did not slide: %d", a.TokenExpiresAt)
	}
	// 90 days idle: revoked, with a message to ask for a new invite.
	*now = now.Add(AgentCredentialTTL + time.Second)
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrRevoked) || !strings.Contains(err.Error(), "new invite") {
		t.Errorf("idle agent err = %v", err)
	}
	// Legacy participants never expire (until #17 decides otherwise).
	if p, err := s.Authenticate(ctx, legacyToken); err != nil || p.TokenExpiresAt != 0 {
		t.Errorf("legacy participant after 250 days = %+v %v", p, err)
	}

	// Sponsor leaving revokes their agents; coming back restores them.
	pass, _, _ = s.MintPass(ctx, ws.ID, alex.ID)
	_, token2, _, _ := s.JoinWithPass(ctx, ws.ID, pass, "Lark", "🐦")
	s.JoinAsUser(ctx, ws.ID, bob.ID, "Bob", "")
	s.RemoveMember(ctx, ws.ID, bob.ID, alex.ID)
	if _, err := s.Authenticate(ctx, token2); !errors.Is(err, ErrRevoked) || !strings.Contains(err.Error(), "Alex, who invited this agent, is no longer a member") {
		t.Errorf("agent of a removed sponsor err = %v", err)
	}
	if _, err := s.Authenticate(ctx, legacyToken); err != nil {
		t.Errorf("legacy participant after a member left: %v", err)
	}
	s.JoinAsUser(ctx, ws.ID, alex.ID, "", "")
	if _, err := s.Authenticate(ctx, token2); err != nil {
		t.Errorf("agent after its sponsor rejoined: %v", err)
	}
}

// TestAgentColumnsMigration opens a database from before agent passes
// (participants with user_id but no sponsor_user_id/token_expires_at, no
// agent_passes table) and checks the columns are added once, existing
// participants keep working without expiry, and passes work after boot.
func TestAgentColumnsMigration(t *testing.T) {
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
			token TEXT NOT NULL UNIQUE, created_at BIGINT NOT NULL, last_seen_at BIGINT NOT NULL, user_id BIGINT REFERENCES users(id))`,
		`INSERT INTO workspaces (id, name, created_at) VALUES ('ws_legacy', 'Legacy', 1)`,
		`INSERT INTO users (display_name, created_at) VALUES ('Alex', 1)`,
		`INSERT INTO memberships (workspace_id, user_id, created_at) VALUES ('ws_legacy', 1, 1)`,
		`INSERT INTO participants (workspace_id, name, emoji, token, created_at, last_seen_at, user_id) VALUES ('ws_legacy', 'Alex', '🧑', 'alextoken', 1, 1, 1)`,
		`INSERT INTO participants (workspace_id, name, emoji, token, created_at, last_seen_at) VALUES ('ws_legacy', 'Old', '🦊', 'oldtoken', 1, 1)`,
	} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	raw.Close()

	var agentToken string
	for boot := 1; boot <= 3; boot++ {
		s, err := Open(dsn)
		if err != nil {
			t.Fatalf("boot %d: %v", boot, err)
		}
		for _, tok := range []string{"alextoken", "oldtoken"} {
			p, err := s.Authenticate(ctx, tok)
			if err != nil || p.Sponsor != nil || p.TokenExpiresAt != 0 {
				t.Fatalf("boot %d: %s = %+v %v", boot, tok, p, err)
			}
			s.Touch(ctx, p.ID) // Must not give legacy participants an expiry.
		}
		if boot == 2 {
			pass, _, err := s.MintPass(ctx, "ws_legacy", 1)
			if err != nil {
				t.Fatalf("mint after migration: %v", err)
			}
			if _, agentToken, _, err = s.JoinWithPass(ctx, "ws_legacy", pass, "Quill", ""); err != nil {
				t.Fatalf("exchange after migration: %v", err)
			}
		}
		if boot == 3 {
			if p, err := s.Authenticate(ctx, agentToken); err != nil || p.Sponsor == nil || p.Sponsor.DisplayName != "Alex" {
				t.Errorf("boot 3: agent = %+v %v", p, err)
			}
		}
		s.Close()
	}
}
