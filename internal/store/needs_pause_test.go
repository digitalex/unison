package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestItemNeedsAndReadyAt(t *testing.T) {
	s, now, a, b := setup(t)
	ctx := context.Background()

	// Create 10 items in ws1 so ws2's global PK IDs (11..14) are distinct from its per-workspace numbers (1..4).
	for range 10 {
		_, _, _ = s.AddItem(ctx, a, "Unrelated", "", 0)
	}

	ws2Owner, err := s.LoginWithProvider(ctx, "github", Profile{Subject: "gh-alex", Name: "Alex"})
	if err != nil {
		t.Fatal(err)
	}
	ws2, _, err := s.CreateWorkspaceFor(ctx, ws2Owner.ID, "Second", "", "")
	if err != nil {
		t.Fatal(err)
	}
	p2, _, _, _, err := s.JoinAsUser(ctx, ws2.ID, ws2Owner.ID, "Alex", "🧑")
	if err != nil {
		t.Fatal(err)
	}

	// Items in ws2 have Number 1, 2, 3, 4 while their PK IDs are 11, 12, 13, 14.
	it1, _, _ := s.AddItem(ctx, p2, "Foundation", "", 0)
	*now = now.Add(10 * time.Second)
	it2, _, _ := s.AddItem(ctx, p2, "API layer", "", 0)
	*now = now.Add(10 * time.Second)
	it3, _, _ := s.AddItem(ctx, p2, "CLI layer", "", 0)
	*now = now.Add(10 * time.Second)
	it4, _, _ := s.AddItem(ctx, p2, "End-to-end", "", 0)

	if it1.Number != 1 || it1.ID == it1.Number {
		t.Fatalf("expected ws2 item #1 to have Number=1 != ID=%d", it1.ID)
	}
	if it1.ReadyAt == nil || *it1.ReadyAt != it1.CreatedAt {
		t.Fatalf("unblocked item ready_at = %v, want %d", it1.ReadyAt, it1.CreatedAt)
	}

	// Validation: self-dependency, invalid IDs, cross-workspace / missing items.
	if _, _, err := s.AddItemNeeds(ctx, p2, it1.Number, it1.Number); !errors.Is(err, ErrInvalid) {
		t.Errorf("self-dependency by number err = %v, want ErrInvalid", err)
	}
	if _, _, err := s.AddItemNeeds(ctx, p2, it1.ID, it1.ID); !errors.Is(err, ErrInvalid) {
		t.Errorf("self-dependency by ID err = %v, want ErrInvalid", err)
	}
	if _, _, err := s.AddItemNeeds(ctx, p2, it1.Number); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty needs err = %v, want ErrInvalid", err)
	}
	if _, _, err := s.AddItemNeeds(ctx, p2, it1.Number, 0); !errors.Is(err, ErrInvalid) {
		t.Errorf("zero need err = %v, want ErrInvalid", err)
	}
	if _, _, err := s.AddItemNeeds(ctx, p2, it1.Number, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing need err = %v, want ErrNotFound", err)
	}
	// Item 1 in ws1 has ID=1; referencing 1 in ws2 resolves to ws2's #1 (self-dep),
	// while referencing an item from another workspace that doesn't exist in ws2 fails.
	otherWSItem, _, _ := s.AddItem(ctx, b, "Other WS item", "", 0)
	if _, _, err := s.AddItemNeeds(ctx, p2, it1.Number, otherWSItem.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-workspace need err = %v, want ErrNotFound", err)
	}

	// Link diamond DAG: #4 needs #2 and #3; #2 needs #1; #3 needs #1 (mixing number and legacy ID).
	got4, evs, err := s.AddItemNeeds(ctx, p2, it4.Number, it2.Number, it3.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Type != "backlog.needs_updated" {
		t.Fatalf("AddItemNeeds events = %+v, want [backlog.needs_updated]", evs)
	}
	if len(got4.Needs) != 2 || got4.Needs[0].Number != 2 || got4.Needs[1].Number != 3 || got4.ReadyAt != nil {
		t.Fatalf("got4 = %+v (needs=%+v, ready_at=%v)", got4, got4.Needs, got4.ReadyAt)
	}

	// Re-adding an already-linked prerequisite is idempotent and emits no duplicate event.
	got4Again, evsAgain, err := s.AddItemNeeds(ctx, p2, it4.Number, it2.Number)
	if err != nil || len(evsAgain) != 0 || len(got4Again.Needs) != 2 {
		t.Fatalf("idempotent AddItemNeeds = %+v, evs=%+v, err=%v", got4Again.Needs, evsAgain, err)
	}

	if _, _, err := s.AddItemNeeds(ctx, p2, it2.Number, it1.Number); err != nil {
		t.Fatalf("link #2 -> #1: %v", err)
	}
	if _, _, err := s.AddItemNeeds(ctx, p2, it3.Number, it1.Number); err != nil {
		t.Fatalf("link #3 -> #1 (diamond DAG): %v", err)
	}

	// Direct cycle (#1 -> #2) and multi-hop cycle (#1 -> #4 where #4 -> #2 -> #1) return ErrConflict.
	if _, _, err := s.AddItemNeeds(ctx, p2, it1.Number, it2.Number); !errors.Is(err, ErrConflict) {
		t.Errorf("direct cycle err = %v, want ErrConflict", err)
	}
	if _, _, err := s.AddItemNeeds(ctx, p2, it1.Number, it4.Number); !errors.Is(err, ErrConflict) {
		t.Errorf("multi-hop cycle err = %v, want ErrConflict", err)
	}

	// Put blocked #4 at position 1: ClaimNext skips #4, #2, #3 (all blocked) and claims #1!
	if _, _, err := s.MoveItem(ctx, p2, it4.Number, 1); err != nil {
		t.Fatal(err)
	}
	claimed, _, err := s.ClaimNext(ctx, p2)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != it1.ID {
		t.Fatalf("ClaimNext picked #%d (%s), want #1 (%s)", claimed.Number, claimed.Title, it1.Title)
	}

	// Close #1 at t+60s: #2 and #3 become ready with ready_at = max(created_at, #1.closed_at),
	// while #4 is still blocked on #2 and #3.
	*now = now.Add(60 * time.Second)
	closed1, _, err := s.CloseItem(ctx, p2, it1.Number, "done")
	if err != nil {
		t.Fatal(err)
	}
	got2, _, _ := s.GetItem(ctx, ws2.ID, it2.Number)
	if got2.ReadyAt == nil || *got2.ReadyAt != *closed1.ClosedAt {
		t.Fatalf("#2 ready_at = %v, want %d", got2.ReadyAt, *closed1.ClosedAt)
	}
	got4, _, _ = s.GetItem(ctx, ws2.ID, it4.Number)
	if got4.ReadyAt != nil || len(got4.Blockers()) != 2 {
		t.Fatalf("#4 should still be blocked on #2 and #3: ready_at=%v blockers=%+v", got4.ReadyAt, got4.Blockers())
	}

	// Close #2 and remove #3 from #4's prerequisites -> #4 becomes ready!
	*now = now.Add(30 * time.Second)
	closed2, _, _ := s.CloseItem(ctx, p2, it2.Number, "")
	got4, removeEvs, err := s.RemoveItemNeed(ctx, p2, it4.Number, it3.Number)
	if err != nil {
		t.Fatal(err)
	}
	if len(removeEvs) != 1 || removeEvs[0].Type != "backlog.needs_updated" {
		t.Fatalf("RemoveItemNeed events = %+v", removeEvs)
	}
	if got4.ReadyAt == nil || *got4.ReadyAt != *closed2.ClosedAt {
		t.Fatalf("#4 ready_at after unblocking = %v, want %d", got4.ReadyAt, *closed2.ClosedAt)
	}
	if _, _, err := s.RemoveItemNeed(ctx, p2, it4.Number, it3.Number); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing absent prerequisite err = %v, want ErrNotFound", err)
	}

	// Reopening #2 blocks #4 again (ready_at = nil).
	if _, _, err := s.ReopenItem(ctx, p2, it2.Number); err != nil {
		t.Fatal(err)
	}
	got4, _, _ = s.GetItem(ctx, ws2.ID, it4.Number)
	if got4.ReadyAt != nil {
		t.Fatalf("#4 ready_at after reopening #2 = %v, want nil", got4.ReadyAt)
	}

	// Deleting prerequisite #2 removes its item_needs edge so #4 is ready with ReadyAt = CreatedAt.
	if _, err := s.DeleteItem(ctx, p2, it2.Number); err != nil {
		t.Fatal(err)
	}
	got4, _, _ = s.GetItem(ctx, ws2.ID, it4.Number)
	if len(got4.Needs) != 0 || got4.ReadyAt == nil || *got4.ReadyAt != got4.CreatedAt {
		t.Fatalf("#4 after deleting prerequisite #2: needs=%+v ready_at=%v want %d", got4.Needs, got4.ReadyAt, got4.CreatedAt)
	}

	// Deleting the workspace succeeds cleanly with remaining item_needs rows.
	if err := s.DeleteWorkspace(ctx, ws2.ID, ws2Owner.ID); err != nil {
		t.Fatalf("DeleteWorkspace with item_needs: %v", err)
	}
}

func TestPauseAndResumeItems(t *testing.T) {
	s, _, a, b := setup(t)
	ctx := context.Background()

	it1, _, _ := s.AddItem(ctx, a, "First", "", 0)
	it2, _, _ := s.AddItem(ctx, a, "Second", "", 0)

	// Start #1 (in_progress with lease and assignee a), then pause it.
	started, _, err := s.StartItem(ctx, a, it1.Number)
	if err != nil || started.LeaseExpiresAt == nil {
		t.Fatalf("StartItem = %+v, %v", started, err)
	}
	paused, evs, err := s.PauseItem(ctx, b, it1.Number)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Status != StatusPaused || paused.LeaseExpiresAt != nil || paused.AssigneeID == nil || *paused.AssigneeID != a.ID || paused.Position != 1 {
		t.Fatalf("paused = %+v, want status=paused, lease=nil, assignee=%d, pos=1", paused, a.ID)
	}
	if len(evs) != 1 || evs[0].Type != "backlog.paused" {
		t.Fatalf("PauseItem events = %+v, want [backlog.paused]", evs)
	}

	// Pausing again or starting a paused item fails with ErrConflict.
	if _, _, err := s.PauseItem(ctx, a, it1.Number); !errors.Is(err, ErrConflict) {
		t.Errorf("pausing already paused item err = %v, want ErrConflict", err)
	}
	if _, _, err := s.StartItem(ctx, a, it1.Number); !errors.Is(err, ErrConflict) {
		t.Errorf("starting paused item err = %v, want ErrConflict", err)
	}

	// ListBacklog("") includes paused items in priority order; --status paused filters to paused.
	queued, _, _ := s.ListBacklog(ctx, a.WorkspaceID, "")
	if len(queued) != 2 || queued[0].ID != it1.ID || queued[0].Status != StatusPaused || queued[1].ID != it2.ID {
		t.Fatalf("queued list = %+v", queued)
	}
	onlyPaused, _, err := s.ListBacklog(ctx, a.WorkspaceID, StatusPaused)
	if err != nil || len(onlyPaused) != 1 || onlyPaused[0].ID != it1.ID {
		t.Fatalf("ListBacklog(paused) = %+v, %v", onlyPaused, err)
	}

	// ClaimNext skips paused #1 and claims #2.
	claimed, _, err := s.ClaimNext(ctx, b)
	if err != nil || claimed.ID != it2.ID {
		t.Fatalf("ClaimNext = %+v, %v; want #2", claimed, err)
	}

	// Resume #1 -> back to open, keeping assignee a.
	resumed, revEvs, err := s.ResumeItem(ctx, a, it1.Number)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != StatusOpen || resumed.AssigneeID == nil || *resumed.AssigneeID != a.ID {
		t.Fatalf("resumed = %+v", resumed)
	}
	if len(revEvs) != 1 || revEvs[0].Type != "backlog.resumed" {
		t.Fatalf("ResumeItem events = %+v", revEvs)
	}
	if _, _, err := s.ResumeItem(ctx, a, it1.Number); !errors.Is(err, ErrConflict) {
		t.Errorf("resuming open item err = %v, want ErrConflict", err)
	}

	// Pausing an unassigned open item and releasing a paused assigned item.
	if _, _, err := s.PauseItem(ctx, a, it1.Number); err != nil {
		t.Fatal(err)
	}
	released, _, err := s.ReleaseItem(ctx, a, it1.Number)
	if err != nil || released.Status != StatusOpen || released.AssigneeID != nil {
		t.Fatalf("ReleaseItem on paused item = %+v, %v", released, err)
	}

	// Closing a paused item works; pausing or resuming a closed item returns ErrConflict.
	if _, _, err := s.PauseItem(ctx, a, it1.Number); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CloseItem(ctx, a, it1.Number, "wrapped up"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PauseItem(ctx, a, it1.Number); !errors.Is(err, ErrConflict) {
		t.Errorf("PauseItem on closed item err = %v, want ErrConflict", err)
	}
	if _, _, err := s.ResumeItem(ctx, a, it1.Number); !errors.Is(err, ErrConflict) {
		t.Errorf("ResumeItem on closed item err = %v, want ErrConflict", err)
	}

	// Activity feed includes backlog.paused and backlog.resumed.
	act, err := s.RecentActivityBefore(ctx, a.WorkspaceID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	seenTypes := map[string]bool{}
	for _, e := range act {
		seenTypes[e.Type] = true
	}
	if !seenTypes["backlog.paused"] || !seenTypes["backlog.resumed"] {
		t.Errorf("activity missing paused/resumed events: %v", seenTypes)
	}
}
