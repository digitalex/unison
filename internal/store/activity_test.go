package store

import (
	"context"
	"slices"
	"testing"
)

// TestWorkspaceActivitySeq: a workspace list carries each workspace's
// latest collaboration by anyone but the caller (#300).
func TestWorkspaceActivitySeq(t *testing.T) {
	s, _, ws, alex, bob := passSetup(t)
	ctx := context.Background()
	seqFor := func(userID int64, wsID string) int64 {
		t.Helper()
		list, err := s.ListWorkspacesFor(ctx, userID)
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(list, func(w Workspace) bool { return w.ID == wsID })
		if i < 0 {
			t.Fatalf("user %d has no workspace %s in %+v", userID, wsID, list)
		}
		return list[i].ActivitySeq
	}
	last := func(evs []Event, err error) int64 {
		t.Helper()
		if err != nil || len(evs) == 0 {
			t.Fatalf("no event: %v", err)
		}
		return evs[len(evs)-1].Seq
	}
	alexP, _, _, _, err := s.JoinAsUser(ctx, ws.ID, alex.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Alex joining is his own doing: nothing for him to catch up on.
	if got := seqFor(alex.ID, ws.ID); got != 0 {
		t.Errorf("after only his own join Alex sees activity %d, want 0", got)
	}
	// What Alex does himself never counts for him, but does for the others.
	_, evs, err := s.PostMessage(ctx, alexP, "hello", nil, nil)
	hello := last(evs, err)
	if got := seqFor(alex.ID, ws.ID); got != 0 {
		t.Errorf("after his own message Alex sees activity %d, want 0", got)
	}
	// Joining also records member.added, which isn't collaboration.
	bobP, _, _, evs, err := s.JoinAsUser(ctx, ws.ID, bob.ID, "Bob", "🐻")
	if err != nil || !sameTypes(evs, "participant.joined", "member.added") {
		t.Fatalf("Bob joining: %v %v", eventTypes(evs), err)
	}
	joined := evs[0].Seq
	if got := seqFor(bob.ID, ws.ID); got != hello {
		t.Errorf("Bob sees activity %d, want Alex's message %d", got, hello)
	}
	if got := seqFor(alex.ID, ws.ID); got != joined {
		t.Errorf("Alex sees activity %d, want Bob joining (%d)", got, joined)
	}

	// Each kind of collaboration by someone else moves it on.
	item, evs, err := s.AddItem(ctx, bobP, "Ship it", "", 0)
	added := last(evs, err)
	if got := seqFor(alex.ID, ws.ID); got != added {
		t.Errorf("after Bob's item Alex sees %d, want %d", got, added)
	}
	_, evs, err = s.AddComment(ctx, bobP, item.ID, "on it")
	commented := last(evs, err)
	if got := seqFor(alex.ID, ws.ID); got != commented {
		t.Errorf("after Bob's comment Alex sees %d, want %d", got, commented)
	}
	msg, evs, err := s.PostMessage(ctx, bobP, "ping", nil, nil)
	ping := last(evs, err)
	if got := seqFor(alex.ID, ws.ID); got != ping {
		t.Errorf("after Bob's message Alex sees %d, want %d", got, ping)
	}

	// Noise doesn't: a status, a reaction, renewing a lease.
	if _, _, err := s.StartItem(ctx, bobP, item.ID); err != nil {
		t.Fatal(err)
	}
	started := seqFor(alex.ID, ws.ID)
	if started <= ping {
		t.Errorf("Bob starting the item left Alex at %d", started)
	}
	status := "reviewing"
	if _, _, err := s.UpdateParticipant(ctx, bobP, ParticipantPatch{Status: &status}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.React(ctx, bobP, msg.ID, "👍", true); err != nil {
		t.Fatal(err)
	}
	if _, evs, err := s.StartItem(ctx, bobP, item.ID); err != nil || !sameTypes(evs, "backlog.renewed") {
		t.Fatalf("renewing: %v %v", eventTypes(evs), err)
	}
	if got := seqFor(alex.ID, ws.ID); got != started {
		t.Errorf("after noise Alex sees %d, want it still at %d", got, started)
	}

	// Alex's own agent is a teammate to hear from.
	pass, _, err := s.MintPass(ctx, ws.ID, alex.ID)
	if err != nil {
		t.Fatal(err)
	}
	agent, _, _, err := s.JoinWithPass(ctx, ws.ID, pass, "Quill", "🪶")
	if err != nil {
		t.Fatal(err)
	}
	_, evs, err = s.PostMessage(ctx, agent, "done", nil, nil)
	done := last(evs, err)
	if got := seqFor(alex.ID, ws.ID); got != done {
		t.Errorf("after his agent's message Alex sees %d, want %d", got, done)
	}

	// Workspaces don't leak into each other, and a quiet one reads 0.
	solo, _, err := s.CreateWorkspaceFor(ctx, alex.ID, "Solo", "", "")
	if err != nil {
		t.Fatal(err)
	}
	soloP, _, _, _, err := s.JoinAsUser(ctx, solo.ID, alex.ID, "Alex", "🧑")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PostMessage(ctx, soloP, "notes to self", nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := seqFor(alex.ID, solo.ID); got != 0 {
		t.Errorf("Alex's solo workspace has activity %d, want 0", got)
	}
	if got := seqFor(alex.ID, ws.ID); got != done {
		t.Errorf("the other workspace moved to %d, want %d", got, done)
	}

	// With no caller (open mode) everybody's activity counts.
	all, err := s.ListWorkspaces(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range all {
		if w.ID == solo.ID && w.ActivitySeq == 0 {
			t.Error("ListWorkspaces left out the solo workspace's only message")
		}
	}
	// A single workspace doesn't carry it.
	if got, err := s.GetWorkspace(ctx, ws.ID); err != nil || got.ActivitySeq != 0 {
		t.Errorf("GetWorkspace activity = %d, %v", got.ActivitySeq, err)
	}
}
