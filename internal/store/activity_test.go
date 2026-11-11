package store

import (
	"context"
	"testing"
)

// TestActivitySeq (#300): workspace lists carry the seq of the latest
// collaboration event by someone other than the caller, ignoring presence
// and the caller's own activity.
func TestActivitySeq(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	alex, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "1", Name: "Alex"})
	bob, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "2", Name: "Bob"})
	ws, _, _ := s.CreateWorkspaceFor(ctx, alex.ID, "W", "", "")
	a, _, _, _, err := s.JoinAsUser(ctx, ws.ID, alex.ID, "Alex", "")
	if err != nil {
		t.Fatal(err)
	}
	seq := func(userID int64) int64 {
		t.Helper()
		list, err := s.ListWorkspacesFor(ctx, userID)
		if err != nil || len(list) != 1 {
			t.Fatalf("workspaces = %+v, %v", list, err)
		}
		return list[0].ActivitySeq
	}
	if got := seq(alex.ID); got != 0 {
		t.Errorf("Alex's activity in a workspace only Alex touched = %d, want 0", got)
	}

	// An agent joins and posts: that's activity for Alex.
	agent, _, _, err := s.Join(ctx, ws.ID, "Tern", "")
	if err != nil {
		t.Fatal(err)
	}
	_, evs, err := s.PostMessage(ctx, agent, "hi", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	posted := evs[0].Seq
	if got := seq(alex.ID); got != posted {
		t.Errorf("after the agent's message, activity = %d, want %d", got, posted)
	}
	// Status updates and Alex's own work don't move it.
	if _, _, err := s.SetStatus(ctx, agent, "thinking"); err != nil {
		t.Fatal(err)
	}
	item, _, err := s.AddItem(ctx, a, "Mine", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := seq(alex.ID); got != posted {
		t.Errorf("after a status and Alex's own item, activity = %d, want %d", got, posted)
	}
	// The agent claiming Alex's item does.
	_, evs, err = s.StartItem(ctx, agent, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := seq(alex.ID); got != evs[len(evs)-1].Seq {
		t.Errorf("after the agent's claim, activity = %d, want %d", got, evs[len(evs)-1].Seq)
	}

	// For Bob, once a member, Alex's item counts too.
	if _, _, _, _, err := s.JoinAsUser(ctx, ws.ID, bob.ID, "Bob", ""); err != nil {
		t.Fatal(err)
	}
	if got := seq(bob.ID); got <= posted {
		t.Errorf("Bob's activity = %d, want past %d", got, posted)
	}
	if all, _ := s.ListWorkspaces(ctx); len(all) != 1 || all[0].ActivitySeq == 0 {
		t.Errorf("open-mode list = %+v", all)
	}
}
