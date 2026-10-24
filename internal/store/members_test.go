package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/digitalex/unison/internal/store/storetest"
)

func eventTypes(evs []Event) []string {
	out := []string{}
	for _, e := range evs {
		out = append(out, e.Type)
	}
	return out
}

func sameTypes(got []Event, want ...string) bool {
	types := eventTypes(got)
	if len(types) != len(want) {
		return false
	}
	for i := range want {
		if types[i] != want[i] {
			return false
		}
	}
	return true
}

func TestJoinAsUserIsIdempotent(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	ws, _, _ := s.CreateWorkspace(ctx, "W", "", "")
	u, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "1", Name: "Alex"})

	p, token, existing, evs, err := s.JoinAsUser(ctx, ws.ID, u.ID, "Alex", "🧑")
	if err != nil || existing || p.UserID != u.ID || token == "" {
		t.Fatalf("first JoinAsUser = %+v %q %v %v", p, token, existing, err)
	}
	if !sameTypes(evs, "participant.joined", "member.added") {
		t.Errorf("events = %v", eventTypes(evs))
	}
	if auth, _ := s.Authenticate(ctx, token); auth.UserID != u.ID {
		t.Errorf("Authenticate = %+v, want linked", auth)
	}

	// Joining again (another device, another name) returns the same
	// participant and token: no duplicate, no events.
	*now = now.Add(time.Second)
	again, token2, existing, evs, err := s.JoinAsUser(ctx, ws.ID, u.ID, "AlexPhone", "")
	if err != nil || !existing || again.ID != p.ID || token2 != token || len(evs) != 0 {
		t.Fatalf("second JoinAsUser = %+v %q %v %v %v", again, token2, existing, eventTypes(evs), err)
	}
	m, _ := s.ListMembers(ctx, ws.ID)
	if len(m.Members) != 1 || len(m.Members[0].Participants) != 1 || len(m.Unlinked) != 0 {
		t.Errorf("members = %+v", m)
	}
	if _, _, _, _, err := s.JoinAsUser(ctx, "ws_missing", u.ID, "", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown workspace err = %v", err)
	}
	// Plain joins (agents) are unchanged: unlinked, no membership.
	agent, _, evs, _ := s.Join(ctx, ws.ID, "Quill", "🦉")
	if agent.UserID != 0 || !sameTypes(evs, "participant.joined") {
		t.Errorf("agent join = %+v %v", agent, eventTypes(evs))
	}
}

func TestClaim(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	ws, _, _ := s.CreateWorkspace(ctx, "W", "", "")
	other, _, _ := s.CreateWorkspace(ctx, "Other", "", "")
	alex, alexTok, _, _ := s.Join(ctx, ws.ID, "Alex", "🧑")
	*now = now.Add(time.Second)
	mobile, mobileTok, _, _ := s.Join(ctx, ws.ID, "Alexmobile", "📱")
	_, agentTok, _, _ := s.Join(ctx, ws.ID, "Quill", "🦉")
	_, otherTok, _, _ := s.Join(ctx, other.ID, "Alex", "")
	u, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "1", Name: "Alex L."})
	bob, _ := s.LoginWithProvider(ctx, "google", Profile{Subject: "2", Name: "Bob"})

	for name, tok := range map[string]string{"empty": "", "wrong": "0123456789abcdef0123456789abcdef", "other workspace": otherTok, "user token": "us_x"} {
		if _, _, err := s.Claim(ctx, ws.ID, u.ID, tok); !errors.Is(err, ErrForbidden) {
			t.Errorf("claim with %s token err = %v, want ErrForbidden", name, err)
		}
	}
	p, evs, err := s.Claim(ctx, ws.ID, u.ID, alexTok)
	if err != nil || p.ID != alex.ID || p.UserID != u.ID || !sameTypes(evs, "participant.claimed", "member.added") {
		t.Fatalf("claim Alex = %+v %v %v", p, eventTypes(evs), err)
	}
	if p, evs, err = s.Claim(ctx, ws.ID, u.ID, mobileTok); err != nil || p.ID != mobile.ID || !sameTypes(evs, "participant.claimed") {
		t.Fatalf("claim Alexmobile = %+v %v %v", p, eventTypes(evs), err)
	}
	if _, evs, err = s.Claim(ctx, ws.ID, u.ID, alexTok); err != nil || len(evs) != 0 {
		t.Errorf("re-claim = %v %v, want a no-op", eventTypes(evs), err)
	}
	if _, _, err := s.Claim(ctx, ws.ID, bob.ID, alexTok); !errors.Is(err, ErrConflict) {
		t.Errorf("claiming someone else's participant err = %v", err)
	}

	m, err := s.ListMembers(ctx, ws.ID)
	if err != nil || len(m.Members) != 1 {
		t.Fatalf("members = %+v %v", m, err)
	}
	got := m.Members[0]
	if got.UserID != u.ID || got.DisplayName != "Alex L." || len(got.Participants) != 2 || got.Participants[0].ID != mobile.ID {
		t.Errorf("member = %+v (want both participants, most recently active first)", got)
	}
	if len(m.Unlinked) != 1 || m.Unlinked[0].Name != "Quill" {
		t.Errorf("unlinked = %+v", m.Unlinked)
	}
	// The claimed participants keep working with their own tokens and names.
	if a, _ := s.Authenticate(ctx, mobileTok); a.Name != "Alexmobile" || a.UserID != u.ID {
		t.Errorf("Alexmobile after claim = %+v", a)
	}
	if a, _ := s.Authenticate(ctx, agentTok); a.UserID != 0 {
		t.Errorf("agent got linked: %+v", a)
	}
	// Joining while signed in now finds the claimed participant.
	if p, tok, existing, _, _ := s.JoinAsUser(ctx, ws.ID, u.ID, "", ""); !existing || p.ID != mobile.ID || tok != mobileTok {
		t.Errorf("JoinAsUser after claims = %+v existing=%v", p, existing)
	}
}

func TestRemoveMember(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	ws, _, _ := s.CreateWorkspace(ctx, "W", "", "")
	alex, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "1", Name: "Alex"})
	bob, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "2", Name: "Bob"})
	carol, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "3", Name: "Carol"})
	ap, _, _, _, _ := s.JoinAsUser(ctx, ws.ID, alex.ID, "Alex", "")
	bp, _, _, _, _ := s.JoinAsUser(ctx, ws.ID, bob.ID, "Bob", "")

	if _, err := s.RemoveMember(ctx, ws.ID, carol.ID, bob.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("non-member removing err = %v", err)
	}
	evs, err := s.RemoveMember(ctx, ws.ID, alex.ID, bob.ID)
	if err != nil || !sameTypes(evs, "member.removed") || evs[0].ActorID != ap.ID {
		t.Fatalf("remove = %v %v", evs, err)
	}
	if _, err := s.RemoveMember(ctx, ws.ID, alex.ID, bob.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing twice err = %v", err)
	}
	if ok, _ := s.IsMember(ctx, ws.ID, bob.ID); ok {
		t.Error("Bob is still a member")
	}
	// Bob's participant stays (history), still linked, listed as unlinked
	// from the membership's point of view.
	m, _ := s.ListMembers(ctx, ws.ID)
	if len(m.Members) != 1 || len(m.Unlinked) != 1 || m.Unlinked[0].ID != bp.ID || m.Unlinked[0].UserID != bob.ID {
		t.Errorf("members after removal = %+v", m)
	}
	// Any member may remove any member, themselves included.
	if _, err := s.RemoveMember(ctx, ws.ID, alex.ID, alex.ID); err != nil {
		t.Errorf("leaving err = %v", err)
	}
}

// TestParticipantUserIDMigration opens a database created before membership
// (participants without user_id, no users table), as on production, and
// checks the column is added once, existing participants stay unlinked and
// working, and later boots are no-ops.
func TestParticipantUserIDMigration(t *testing.T) {
	dsn := storetest.DSN(t)
	ctx := context.Background()
	raw, err := openRaw(dsn)
	if err != nil {
		t.Fatal(err)
	}
	ddl := []string{
		`CREATE TABLE workspaces (id TEXT PRIMARY KEY, name TEXT NOT NULL, repo_url TEXT NOT NULL DEFAULT '', canvas_url TEXT NOT NULL DEFAULT '', created_at BIGINT NOT NULL)`,
		`CREATE TABLE participants (id ` + map[bool]string{true: "BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY", false: "INTEGER PRIMARY KEY AUTOINCREMENT"}[raw.pg] +
			`, workspace_id TEXT NOT NULL REFERENCES workspaces(id), name TEXT NOT NULL, emoji TEXT NOT NULL, status TEXT NOT NULL DEFAULT '', token TEXT NOT NULL UNIQUE, created_at BIGINT NOT NULL, last_seen_at BIGINT NOT NULL)`,
		`INSERT INTO workspaces (id, name, created_at) VALUES ('ws_legacy', 'Legacy', 1)`,
		`INSERT INTO participants (workspace_id, name, emoji, token, created_at, last_seen_at) VALUES ('ws_legacy', 'Alex', '🧑', 'legacytoken', 1, 1)`,
	}
	for _, q := range ddl {
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
		p, err := s.Authenticate(ctx, "legacytoken")
		if err != nil || p.Name != "Alex" || (boot == 1 && p.UserID != 0) {
			t.Fatalf("boot %d: legacy participant = %+v %v", boot, p, err)
		}
		if boot == 2 {
			u, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "1", Name: "Alex"})
			if _, _, err := s.Claim(ctx, "ws_legacy", u.ID, "legacytoken"); err != nil {
				t.Fatalf("claiming a legacy participant: %v", err)
			}
		}
		if boot == 3 {
			if m, _ := s.ListMembers(ctx, "ws_legacy"); len(m.Members) != 1 || m.Members[0].Participants[0].Name != "Alex" {
				t.Errorf("boot 3: members = %+v (the claim must survive reboots)", m)
			}
		}
		s.Close()
	}
}

// openRaw connects without applying the schema (to build legacy databases).
func openRaw(dsn string) (db, error) {
	if IsPostgresDSN(dsn) {
		conn, err := sql.Open("pgx", dsn)
		return db{DB: conn, pg: true}, err
	}
	conn, err := sql.Open("sqlite", dsn)
	return db{DB: conn}, err
}
