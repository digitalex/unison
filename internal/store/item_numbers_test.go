package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digitalex/unison/internal/store/storetest"
)

func TestPerWorkspaceItemNumberingAndLegacyLookup(t *testing.T) {
	dsn := storetest.DSN(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	s, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SetClock(func() time.Time { return now })

	user, err := s.LoginWithProvider(ctx, "github", Profile{Subject: "alex", Name: "Alex"})
	if err != nil {
		t.Fatal(err)
	}
	wsA, _, err := s.CreateWorkspaceFor(ctx, user.ID, "Alpha", "", "")
	if err != nil {
		t.Fatal(err)
	}
	wsB, _, err := s.CreateWorkspaceFor(ctx, user.ID, "Beta", "", "")
	if err != nil {
		t.Fatal(err)
	}
	alexA, _, _, _, err := s.JoinAsUser(ctx, wsA.ID, user.ID, "Alex", "🧑")
	if err != nil {
		t.Fatal(err)
	}
	alexB, _, _, _, err := s.JoinAsUser(ctx, wsB.ID, user.ID, "Alex", "🧑")
	if err != nil {
		t.Fatal(err)
	}

	// Add 3 items in Alpha (#1, #2, #3 -> global IDs 1, 2, 3).
	for want := int64(1); want <= 3; want++ {
		it, _, err := s.AddItem(ctx, alexA, "Alpha item", "", 0)
		if err != nil {
			t.Fatal(err)
		}
		if it.Number != want {
			t.Fatalf("Alpha item Number = %d, want %d", it.Number, want)
		}
	}

	// Beta starts its own sequential numbering at #1 even though global IDs are >= 4.
	b1, _, err := s.AddItem(ctx, alexB, "Beta first", "Body 1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if b1.Number != 1 || b1.ID <= 3 {
		t.Fatalf("Beta first: ID=%d Number=%d, want ID>3 Number=1", b1.ID, b1.Number)
	}
	b2, _, err := s.AddItem(ctx, alexB, "Beta second", "Body 2", 0)
	if err != nil {
		t.Fatal(err)
	}
	if b2.Number != 2 {
		t.Fatalf("Beta second Number = %d, want 2", b2.Number)
	}

	// Deleting #2 in Beta must NOT reuse #2 — next item gets #3!
	if _, err := s.DeleteItem(ctx, alexB, 2); err != nil {
		t.Fatalf("DeleteItem(#2): %v", err)
	}
	b3, _, err := s.AddItem(ctx, alexB, "Beta third", "Body 3", 0)
	if err != nil {
		t.Fatal(err)
	}
	if b3.Number != 3 {
		t.Fatalf("after deleting #2, next item Number = %d, want 3", b3.Number)
	}

	// Scheduled recurring items (RunSchedule and MaterializeSchedules) allocate sequential numbers.
	sc, _, err := s.AddSchedule(ctx, alexB, ScheduleInput{Title: "Hourly check", Cadence: CadenceHourly})
	if err != nil {
		t.Fatal(err)
	}
	ranItem, added, _, err := s.RunSchedule(ctx, alexB, sc.ID)
	if err != nil || !added {
		t.Fatalf("RunSchedule: item=%+v added=%v err=%v", ranItem, added, err)
	}
	if ranItem.Number != 4 {
		t.Fatalf("RunSchedule item Number = %d, want 4", ranItem.Number)
	}
	if _, _, err := s.CloseItem(ctx, alexB, ranItem.Number, "done"); err != nil {
		t.Fatalf("CloseItem scheduled run: %v", err)
	}
	now = now.Add(2 * time.Hour)
	evs, err := s.MaterializeSchedules(ctx)
	if err != nil || len(evs) == 0 {
		t.Fatalf("MaterializeSchedules: evs=%d err=%v", len(evs), err)
	}
	listB, _, err := s.ListBacklog(ctx, wsB.ID, "open")
	if err != nil {
		t.Fatal(err)
	}
	var matItem *Item
	for i := range listB {
		if listB[i].ScheduleID != nil && *listB[i].ScheduleID == sc.ID {
			matItem = &listB[i]
			break
		}
	}
	if matItem == nil || matItem.Number != 5 {
		t.Fatalf("materialized scheduled item = %+v, want Number=5", matItem)
	}

	// Note: ranItem had Number == 4, which also equals b1.ID (4)!
	// ResolveItemID must prefer per-workspace number (ranItem) when ref == 4,
	// while still resolving legacy global IDs that have no colliding item number (e.g. b3.ID).
	got4, err := s.ResolveItemID(ctx, wsB.ID, 4)
	if err != nil || got4 != ranItem.ID {
		t.Fatalf("ResolveItemID(wsB, 4) = %d, %v; want ranItem.ID=%d (number takes precedence)", got4, err, ranItem.ID)
	}
	gotLegacyB3, err := s.ResolveItemID(ctx, wsB.ID, b3.ID)
	if err != nil || gotLegacyB3 != b3.ID {
		t.Fatalf("ResolveItemID(wsB, legacy b3.ID=%d) = %d, %v; want %d", b3.ID, gotLegacyB3, err, b3.ID)
	}
	// Cross-workspace isolation: Alpha's global ID 1 must resolve to Beta's #1, not Alpha's item,
	// and an unassigned global ID from Alpha (if Beta had 0 items) returns ErrNotFound.
	wsC, _, _ := s.CreateWorkspaceFor(ctx, user.ID, "Gamma", "", "")
	if _, err := s.ResolveItemID(ctx, wsC.ID, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ResolveItemID on empty workspace = %v, want ErrNotFound", err)
	}

	// PromoteIdea assigns the next sequential number (#6) and exposes PromotedItemNumber.
	idea, _, err := s.AddIdea(ctx, alexB, "Great idea", "Details")
	if err != nil {
		t.Fatal(err)
	}
	prom, _, err := s.PromoteIdea(ctx, alexB, idea.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if prom.Item.Number != 6 || prom.Idea.PromotedItemNumber == nil || *prom.Idea.PromotedItemNumber != 6 {
		t.Fatalf("PromoteIdea: item.Number=%d idea.PromotedItemNumber=%v, want 6", prom.Item.Number, prom.Idea.PromotedItemNumber)
	}

	// Operations by BOTH per-workspace number (#3) and legacy global ID (prom.Item.ID) work:
	// 1. Comments
	c3, _, err := s.AddComment(ctx, alexB, 3, "Comment via #3")
	if err != nil || c3.ItemID == nil || *c3.ItemID != b3.ID || c3.ItemNumber != 3 {
		t.Fatalf("AddComment by number: c=%+v err=%v", c3, err)
	}
	if c, _, err := s.AddComment(ctx, alexB, prom.Item.ID, "Comment via legacy ID"); err != nil || c.ItemID == nil || *c.ItemID != prom.Item.ID || c.ItemNumber != 6 {
		t.Fatalf("AddComment by legacy ID: c=%+v err=%v", c, err)
	}
	if comments, err := s.ListComments(ctx, wsB.ID, 6); err != nil || len(comments) != 1 {
		t.Fatalf("ListComments(#6) = %+v, %v", comments, err)
	}

	// 2. Chat messages linked to item by #number and legacy ID
	numRef := int64(3)
	msg1, _, err := s.Post(ctx, alexB, NewMessage{Body: "Working on #3", ItemID: &numRef})
	if err != nil || msg1.ItemID == nil || *msg1.ItemID != b3.ID || msg1.Item == nil || msg1.Item.Number != 3 {
		t.Fatalf("Post with item number 3 = %+v, %v", msg1, err)
	}
	legacyRef := prom.Item.ID
	msg2, _, err := s.Post(ctx, alexB, NewMessage{Body: "Working on promoted", ItemID: &legacyRef})
	if err != nil || msg2.ItemID == nil || *msg2.ItemID != prom.Item.ID || msg2.Item == nil || msg2.Item.Number != 6 {
		t.Fatalf("Post with legacy item ID = %+v, %v", msg2, err)
	}
	if itemMsgs, err := s.ItemMessages(ctx, wsB.ID, 3); err != nil || len(itemMsgs) != 2 || itemMsgs[0].ID != c3.ID || itemMsgs[1].ID != msg1.ID {
		t.Fatalf("ItemMessages(#3) = %+v, %v", itemMsgs, err)
	}

	// 3. TODOs linked by #number and legacy ID
	td1, _, err := s.CreateTodo(ctx, alexB, TodoInput{Title: "Check #3", AssigneeID: alexB.ID, ItemID: &numRef})
	if err != nil || td1.ItemID == nil || *td1.ItemID != b3.ID || td1.ItemNumber != 3 {
		t.Fatalf("CreateTodo with number 3 = %+v, %v", td1, err)
	}
	td2, _, err := s.CreateTodo(ctx, alexB, TodoInput{Title: "Check #6", AssigneeID: alexB.ID, ItemID: &legacyRef})
	if err != nil || td2.ItemID == nil || *td2.ItemID != prom.Item.ID || td2.ItemNumber != 6 {
		t.Fatalf("CreateTodo with legacy ID = %+v, %v", td2, err)
	}
	userTodos, err := s.ListUserTodos(ctx, user.ID, TodoFilterActive)
	if err != nil || len(userTodos) != 2 {
		t.Fatalf("ListUserTodos = %+v, %v", userTodos, err)
	}

	// 4. Artifacts linked and filtered by #number and legacy ID
	art1, _, err := s.AddArtifact(ctx, alexB, ArtifactInput{Name: "spec.md", Data: []byte("# Spec"), ItemID: &numRef})
	if err != nil || art1.ItemID == nil || *art1.ItemID != b3.ID || art1.ItemNumber != 3 {
		t.Fatalf("AddArtifact with number 3 = %+v, %v", art1, err)
	}
	if arts, err := s.ListArtifacts(ctx, wsB.ID, 3); err != nil || len(arts) != 1 || arts[0].Name != "spec.md" {
		t.Fatalf("ListArtifacts(number=3) = %+v, %v", arts, err)
	}
	artUpdated, _, err := s.UpdateArtifact(ctx, alexB, "spec.md", ArtifactPatch{ItemID: &legacyRef})
	if err != nil || artUpdated.ItemNumber != 6 {
		t.Fatalf("UpdateArtifact to legacy ID = %+v, %v", artUpdated, err)
	}
	if arts, err := s.ListArtifacts(ctx, wsB.ID, legacyRef); err != nil || len(arts) != 1 || arts[0].Name != "spec.md" {
		t.Fatalf("ListArtifacts(legacyID=%d) = %+v, %v", legacyRef, arts, err)
	}
}
