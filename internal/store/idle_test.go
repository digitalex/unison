package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// newAgent makes a member (Alex) with a participant, and an agent of Alex's.
func newAgent(t *testing.T) (*Store, *time.Time, *Participant, *Participant, string) {
	t.Helper()
	s, now := newStore(t)
	ctx := context.Background()
	alex, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "1", Name: "Alex"})
	ws, _, _ := s.CreateWorkspaceFor(ctx, alex.ID, "W", "", "")
	human, _, _, _, err := s.JoinAsUser(ctx, ws.ID, alex.ID, "Alex", "")
	if err != nil {
		t.Fatal(err)
	}
	pass, _, _ := s.MintPass(ctx, ws.ID, alex.ID)
	agent, tok, _, err := s.JoinWithPass(ctx, ws.ID, pass, "Wren", "")
	if err != nil {
		t.Fatal(err)
	}
	return s, now, human, agent, tok
}

// TestAgentIdleLogout: an agent active within AgentIdleTimeout stays in;
// past it, it is logged out for good (row kept, reason "idle", an event).
func TestAgentIdleLogout(t *testing.T) {
	s, now, _, agent, tok := newAgent(t)
	ctx := context.Background()
	if agent.LastActiveAt != now.UnixMilli() {
		t.Fatalf("a new agent's last_active_at = %d, want now", agent.LastActiveAt)
	}
	// 5h59m later it still works; activity resets the clock.
	*now = now.Add(5*time.Hour + 59*time.Minute)
	p, err := s.Authenticate(ctx, tok)
	if err != nil {
		t.Fatalf("at 5h59m: %v", err)
	}
	s.MarkActive(ctx, p)
	*now = now.Add(5*time.Hour + 59*time.Minute)
	if _, err := s.Authenticate(ctx, tok); err != nil {
		t.Fatalf("5h59m after the last activity: %v", err)
	}
	// 6h01m without activity: logged out.
	*now = now.Add(2 * time.Minute)
	if _, err := s.Authenticate(ctx, tok); !errors.Is(err, ErrIdle) || !errors.Is(err, ErrRevoked) {
		t.Fatalf("at 6h01m idle: err = %v, want ErrIdle (and ErrRevoked)", err)
	}
	got, _ := s.GetParticipant(ctx, agent.WorkspaceID, agent.ID)
	if got.RevokedAt != now.UnixMilli() || got.RevokeReason != "idle" || got.Name != "Wren" {
		t.Errorf("logged-out agent = %+v (the row stays, revoked as idle)", got)
	}
	evs, _ := s.EventsAfter(ctx, agent.WorkspaceID, 0, 0)
	if last := evs[len(evs)-1]; last.Type != "participant.logged_out" {
		t.Errorf("last event = %s, want participant.logged_out", last.Type)
	}
	// It stays logged out, activity or not.
	*now = now.Add(time.Minute)
	if _, err := s.Authenticate(ctx, tok); !errors.Is(err, ErrIdle) {
		t.Errorf("after logout: err = %v", err)
	}
}

// TestMarkActiveThrottle: at most one write a minute per agent.
func TestMarkActiveThrottle(t *testing.T) {
	s, now, _, agent, _ := newAgent(t)
	ctx := context.Background()
	start := agent.LastActiveAt
	*now = now.Add(30 * time.Second)
	s.MarkActive(ctx, agent)
	if got, _ := s.GetParticipant(ctx, agent.WorkspaceID, agent.ID); got.LastActiveAt != start {
		t.Errorf("written after 30s: %d, want %d", got.LastActiveAt, start)
	}
	*now = now.Add(31 * time.Second)
	s.MarkActive(ctx, agent)
	if got, _ := s.GetParticipant(ctx, agent.WorkspaceID, agent.ID); got.LastActiveAt != now.UnixMilli() {
		t.Errorf("not written after 61s: %d", got.LastActiveAt)
	}
}

// TestHumansNeverIdle: people's participants aren't tracked or logged out.
func TestHumansNeverIdle(t *testing.T) {
	s, now, human, _, _ := newAgent(t)
	ctx := context.Background()
	tok := humanToken(t, s, human)
	*now = now.Add(100 * time.Hour)
	p, err := s.Authenticate(ctx, tok)
	if err != nil {
		t.Fatalf("human after 100h: %v", err)
	}
	s.MarkActive(ctx, p)
	if got, _ := s.GetParticipant(ctx, human.WorkspaceID, human.ID); got.LastActiveAt != 0 || got.RevokedAt != 0 {
		t.Errorf("human = %+v", got)
	}
}

func humanToken(t *testing.T, s *Store, p *Participant) string {
	t.Helper()
	var tok string
	if err := s.db.QueryRowContext(context.Background(), `SELECT token FROM participants WHERE id = ?`, p.ID).Scan(&tok); err != nil {
		t.Fatal(err)
	}
	return tok
}

// TestIdleUntrackedAgent: agents from before tracking (NULL last_active_at,
// as on deploy) aren't logged out on sight: their clock starts then.
func TestIdleUntrackedAgent(t *testing.T) {
	s, now, _, agent, tok := newAgent(t)
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `UPDATE participants SET last_active_at = NULL WHERE id = ?`, agent.ID); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(100 * time.Hour)
	if _, err := s.Authenticate(ctx, tok); err != nil {
		t.Fatalf("first sight after deploy: %v", err)
	}
	if got, _ := s.GetParticipant(ctx, agent.WorkspaceID, agent.ID); got.LastActiveAt != now.UnixMilli() {
		t.Errorf("clock not started: %d", got.LastActiveAt)
	}
	*now = now.Add(6*time.Hour + time.Minute)
	if _, err := s.Authenticate(ctx, tok); !errors.Is(err, ErrIdle) {
		t.Errorf("6h01m after first sight: err = %v", err)
	}
}
