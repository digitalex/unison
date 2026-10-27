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
	if len(m.Members) != 1 || len(m.Members[0].Participants) != 1 {
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

	// Each use (Touch, MarkActive) slides the expiry: 160 days of steady use
	// (every 4 hours, within AgentIdleTimeout) is fine.
	for step := 0; step < 160*6; step++ {
		*now = now.Add(4 * time.Hour)
		a, err := s.Authenticate(ctx, token)
		if err != nil {
			t.Fatalf("after %v of use: %v", time.Duration(step+1)*4*time.Hour, err)
		}
		s.Touch(ctx, agent.ID)
		s.MarkActive(ctx, a)
	}
	if a, _ := s.Authenticate(ctx, token); a.TokenExpiresAt != now.UnixMilli()+AgentCredentialTTL.Milliseconds() {
		t.Errorf("expiry did not slide: %d", a.TokenExpiresAt)
	}
	// Long idle: logged out (AgentIdleTimeout comes long before the 90-day
	// lapse), with a message to ask for a new pass.
	*now = now.Add(AgentCredentialTTL + time.Second)
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrRevoked) || !errors.Is(err, ErrIdle) || !strings.Contains(err.Error(), "new pass") {
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

func TestAgentRejoin(t *testing.T) {
	s, _, ws, alex, _ := passSetup(t)
	ctx := context.Background()

	// 1. Initial join as an agent.
	pass1, _, err := s.MintPass(ctx, ws.ID, alex.ID)
	if err != nil {
		t.Fatal(err)
	}
	p1, token1, evs1, err := s.JoinWithPass(ctx, ws.ID, pass1, "Quill", "🪶")
	if err != nil {
		t.Fatal(err)
	}
	if p1.Name != "Quill" || p1.Emoji != "🪶" {
		t.Fatalf("agent p1 = %+v", p1)
	}
	if !sameTypes(evs1, "participant.joined") {
		t.Fatalf("events = %v", eventTypes(evs1))
	}

	// 2. Quill is revoked (e.g. idle or sponsor revokes it).
	if _, _, err := s.RevokeAgent(ctx, alex.ID, p1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, token1); !errors.Is(err, ErrRevoked) {
		t.Fatalf("revoked agent token1 Authenticate err = %v, want ErrRevoked", err)
	}

	// 3. Quill rejoins with a new pass using the same name and a new emoji.
	pass2, _, err := s.MintPass(ctx, ws.ID, alex.ID)
	if err != nil {
		t.Fatal(err)
	}
	p2, token2, evs2, err := s.JoinWithPass(ctx, ws.ID, pass2, "Quill", "🖋️")
	if err != nil {
		t.Fatalf("rejoin err = %v", err)
	}
	if p2.ID != p1.ID {
		t.Errorf("p2.ID = %d, want existing p1.ID = %d", p2.ID, p1.ID)
	}
	if p2.Emoji != "🖋️" {
		t.Errorf("p2.Emoji = %q, want '🖋️'", p2.Emoji)
	}
	if p2.RevokedAt != 0 || p2.RevokeReason != "" {
		t.Errorf("p2 still marked revoked: revoked_at=%d reason=%q", p2.RevokedAt, p2.RevokeReason)
	}
	if !sameTypes(evs2, "participant.joined") {
		t.Fatalf("rejoin events = %v", eventTypes(evs2))
	}

	// Old token is invalidated, new token authenticates.
	if _, err := s.Authenticate(ctx, token1); !errors.Is(err, ErrNotFound) {
		t.Errorf("old token Authenticate err = %v, want ErrNotFound", err)
	}
	a, err := s.Authenticate(ctx, token2)
	if err != nil || a.ID != p1.ID || a.RevokedAt != 0 {
		t.Errorf("new token Authenticate = %+v %v", a, err)
	}

	// 4. Rejoin with same name case-insensitively and empty emoji (retains existing emoji).
	pass3, _, err := s.MintPass(ctx, ws.ID, alex.ID)
	if err != nil {
		t.Fatal(err)
	}
	p3, token3, _, err := s.JoinWithPass(ctx, ws.ID, pass3, "quill", "")
	if err != nil {
		t.Fatalf("case-insensitive rejoin err = %v", err)
	}
	if p3.ID != p1.ID {
		t.Errorf("p3.ID = %d, want %d", p3.ID, p1.ID)
	}
	if p3.Emoji != "🖋️" {
		t.Errorf("p3.Emoji = %q, want '🖋️'", p3.Emoji)
	}
	if _, err := s.Authenticate(ctx, token3); err != nil {
		t.Errorf("token3 Authenticate err = %v", err)
	}

	// 5. Trying to join with pass using a human member's name fails with ErrConflict.
	pass4, _, err := s.MintPass(ctx, ws.ID, alex.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = s.JoinWithPass(ctx, ws.ID, pass4, "Alex", "")
	if !errors.Is(err, ErrConflict) {
		t.Errorf("join with human member's name err = %v, want ErrConflict", err)
	}

	// 6. Only one participant row exists for Quill in ListParticipants.
	parts, err := s.ListParticipants(ctx, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	var quillCount int
	for _, p := range parts {
		if strings.EqualFold(p.Name, "quill") {
			quillCount++
		}
	}
	if quillCount != 1 {
		t.Errorf("quill participant count = %d, want 1", quillCount)
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
