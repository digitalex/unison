package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTodosLifecycleAndValidation(t *testing.T) {
	f := newActionFixture(t)
	ctx := context.Background()

	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	f.s.SetClock(func() time.Time { return now })

	it, _, err := f.s.AddItem(ctx, f.wren, "Set up billing", "Configure webhook secret", 0)
	if err != nil {
		t.Fatal(err)
	}

	// Reject agent assignees, missing assignees, non-existent items, and bad PR URLs.
	if _, _, err := f.s.CreateTodo(ctx, f.wren, TodoInput{Title: "Agent task", AssigneeID: f.wren.ID}); !errors.Is(err, ErrInvalid) {
		t.Errorf("assigning TODO to agent = %v, want ErrInvalid", err)
	}
	if _, _, err := f.s.CreateTodo(ctx, f.wren, TodoInput{Title: "Agent by name", Assignee: "Wren"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("assigning TODO to agent by name = %v, want ErrInvalid", err)
	}
	if _, _, err := f.s.CreateTodo(ctx, f.wren, TodoInput{Title: "No assignee"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("missing assignee = %v, want ErrInvalid", err)
	}
	missingItem := int64(9999)
	if _, _, err := f.s.CreateTodo(ctx, f.wren, TodoInput{Title: "Bad item", AssigneeID: f.alex.ID, ItemID: &missingItem}); !errors.Is(err, ErrInvalid) {
		t.Errorf("non-existent item = %v, want ErrInvalid", err)
	}
	if _, _, err := f.s.CreateTodo(ctx, f.wren, TodoInput{Title: "Bad PR", AssigneeID: f.alex.ID, PRURL: "https://github.com/acme/repo/issues/1"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("non-PR GitHub URL = %v, want ErrInvalid", err)
	}

	// Create a TODO linked to a backlog item and normalized GitHub PR URL.
	td, evs, err := f.s.CreateTodo(ctx, f.wren, TodoInput{
		Title:    "Review billing PR",
		Body:     "Check webhook verification",
		Assignee: "@alex",
		ItemID:   &it.ID,
		PRURL:    "https://github.com/digitalex/unison.git/pull/42/files#diff-1",
	})
	if err != nil {
		t.Fatalf("CreateTodo: %v", err)
	}
	if td.AssigneeID != f.alex.ID || td.AssigneeName != "Alex" || td.CreatorID == nil || *td.CreatorID != f.wren.ID {
		t.Errorf("created todo = %+v", td)
	}
	if td.ItemID == nil || *td.ItemID != it.ID || td.Item == nil || td.Item.Title != "Set up billing" {
		t.Errorf("item link = %+v", td)
	}
	if td.PRURL != "https://github.com/digitalex/unison/pull/42" {
		t.Errorf("normalized PRURL = %q, want https://github.com/digitalex/unison/pull/42", td.PRURL)
	}
	if len(evs) != 1 || evs[0].Type != "todo.created" {
		t.Errorf("events = %+v, want [todo.created]", evs)
	}

	if n, err := f.s.CountOpenTodos(ctx, f.alex); err != nil || n != 1 {
		t.Errorf("CountOpenTodos(Alex) = %d, %v; want 1", n, err)
	}
	if n, err := f.s.CountOpenTodos(ctx, f.bob); err != nil || n != 0 {
		t.Errorf("CountOpenTodos(Bob) = %d, %v; want 0", n, err)
	}

	// Snooze 1d: hidden from active & CountOpenTodos, visible in snoozed & open.
	snoozed, evs, err := f.s.SnoozeTodo(ctx, f.alex, td.ID, "1d")
	if err != nil {
		t.Fatalf("SnoozeTodo(1d): %v", err)
	}
	if snoozed.SnoozedUntil == nil || *snoozed.SnoozedUntil != now.Add(24*time.Hour).UnixMilli() || len(evs) != 1 || evs[0].Type != "todo.snoozed" {
		t.Errorf("snoozed 1d = %+v, events = %+v", snoozed, evs)
	}
	if n, _ := f.s.CountOpenTodos(ctx, f.alex); n != 0 {
		t.Errorf("CountOpenTodos while snoozed = %d, want 0", n)
	}
	if active, _ := f.s.ListTodos(ctx, f.ws.ID, TodoFilterActive, 0); len(active) != 0 {
		t.Errorf("active list while snoozed = %+v, want empty", active)
	}
	if sn, _ := f.s.ListTodos(ctx, f.ws.ID, TodoFilterSnoozed, 0); len(sn) != 1 {
		t.Errorf("snoozed list = %+v, want 1", sn)
	}
	if open, _ := f.s.ListTodos(ctx, f.ws.ID, TodoFilterOpen, 0); len(open) != 1 {
		t.Errorf("open list = %+v, want 1", open)
	}

	// Advance clock 24h: snooze elapses and TODO reappears in active & CountOpenTodos!
	now = now.Add(24*time.Hour + time.Second)
	if n, _ := f.s.CountOpenTodos(ctx, f.alex); n != 1 {
		t.Errorf("CountOpenTodos after 1d elapsed = %d, want 1", n)
	}
	if active, _ := f.s.ListTodos(ctx, f.ws.ID, TodoFilterActive, 0); len(active) != 1 {
		t.Errorf("active list after 1d elapsed = %+v, want 1", active)
	}

	// Snooze 1w, then unsnooze ("none"), then verify 1w expiry as well.
	if sn1w, _, err := f.s.SnoozeTodo(ctx, f.alex, td.ID, "1w"); err != nil || sn1w.SnoozedUntil == nil {
		t.Fatalf("SnoozeTodo(1w) = %+v, %v", sn1w, err)
	}
	now = now.Add(6 * 24 * time.Hour)
	if active, _ := f.s.ListTodos(ctx, f.ws.ID, TodoFilterActive, 0); len(active) != 0 {
		t.Errorf("active list after 6d of 1w snooze = %+v, want empty", active)
	}
	now = now.Add(25 * time.Hour)
	if active, _ := f.s.ListTodos(ctx, f.ws.ID, TodoFilterActive, 0); len(active) != 1 {
		t.Errorf("active list after 1w elapsed = %+v, want 1", active)
	}
	if cleared, _, err := f.s.SnoozeTodo(ctx, f.alex, td.ID, "none"); err != nil || cleared.SnoozedUntil != nil {
		t.Errorf("SnoozeTodo(none) = %+v, %v", cleared, err)
	}

	// Agents must provide a non-empty note on DoneTodo; humans may omit it.
	if _, _, err := f.s.DoneTodo(ctx, f.wren, td.ID, "   "); !errors.Is(err, ErrInvalid) {
		t.Errorf("agent DoneTodo without note = %v, want ErrInvalid", err)
	}
	doneByAgent, evs, err := f.s.DoneTodo(ctx, f.wren, td.ID, "Verified by Wren")
	if err != nil {
		t.Fatalf("agent DoneTodo with note: %v", err)
	}
	if doneByAgent.Status != TodoStatusDone || doneByAgent.DoneNote != "Verified by Wren" || len(evs) != 1 || evs[0].Type != "todo.done" {
		t.Errorf("doneByAgent = %+v, events = %+v", doneByAgent, evs)
	}
	if _, _, err := f.s.SnoozeTodo(ctx, f.alex, td.ID, "1d"); !errors.Is(err, ErrConflict) {
		t.Errorf("snoozing done TODO = %v, want ErrConflict", err)
	}

	// Reopen, then human marks done without a note.
	reopened, evs, err := f.s.ReopenTodo(ctx, f.alex, td.ID)
	if err != nil || reopened.Status != TodoStatusOpen || reopened.DoneNote != "" || len(evs) != 1 || evs[0].Type != "todo.reopened" {
		t.Fatalf("ReopenTodo = %+v, %v, events = %+v", reopened, err, evs)
	}
	doneByHuman, _, err := f.s.DoneTodo(ctx, f.alex, td.ID, "")
	if err != nil || doneByHuman.Status != TodoStatusDone || doneByHuman.DoneNote != "" {
		t.Fatalf("human DoneTodo without note = %+v, %v", doneByHuman, err)
	}

	// Deleting the linked backlog item unlinks item_id cleanly and records todo.updated.
	if evs, err := f.s.DeleteItem(ctx, f.alex, it.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	} else {
		hasUpdated := false
		for _, e := range evs {
			if e.Type == "todo.updated" {
				hasUpdated = true
			}
		}
		if !hasUpdated {
			t.Errorf("DeleteItem events = %+v, expected todo.updated", evs)
		}
	}
	if all, _ := f.s.ListTodos(ctx, f.ws.ID, TodoFilterAll, 0); len(all) != 1 || all[0].ItemID != nil {
		t.Errorf("after DeleteItem, todo = %+v, want item_id nil", all)
	}

	// Delete TODO.
	if evs, err := f.s.DeleteTodo(ctx, f.alex, td.ID); err != nil || len(evs) != 1 || evs[0].Type != "todo.deleted" {
		t.Errorf("DeleteTodo = %+v, %v", evs, err)
	}
	if all, _ := f.s.ListTodos(ctx, f.ws.ID, TodoFilterAll, 0); len(all) != 0 {
		t.Errorf("after DeleteTodo = %+v, want empty", all)
	}
}

func TestCrossWorkspaceUserTodosAndAutoClose(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 14, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return now })

	alexUser, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "alex", Name: "Alex"})
	bobUser, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "bob", Name: "Bob"})

	ws1, _, _ := s.CreateWorkspaceFor(ctx, alexUser.ID, "Alpha", "", "")
	ws2, _, _ := s.CreateWorkspaceFor(ctx, alexUser.ID, "Beta", "", "")
	alex1, _, _, _, _ := s.JoinAsUser(ctx, ws1.ID, alexUser.ID, "Alex", "🧑")
	bob1, _, _, _, _ := s.JoinAsUser(ctx, ws1.ID, bobUser.ID, "Bob", "🐙")
	alex2, _, _, _, _ := s.JoinAsUser(ctx, ws2.ID, alexUser.ID, "Alex", "🧑")

	t1, _, _ := s.CreateTodo(ctx, bob1, TodoInput{Title: "Approve Alpha release", AssigneeID: alex1.ID, PRURL: "https://github.com/acme/alpha/pull/9"})
	t2, _, _ := s.CreateTodo(ctx, alex2, TodoInput{Title: "Rotate Beta keys", AssigneeID: alex2.ID})
	_, _, _ = s.CreateTodo(ctx, alex1, TodoInput{Title: "Bob's task", AssigneeID: bob1.ID})

	list, err := s.ListUserTodos(ctx, alexUser.ID, TodoFilterActive)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListUserTodos(Alex, active) = %+v, %v; want 2", list, err)
	}
	// Snoozing t2 hides it from active cross-workspace list, keeps it in open.
	if _, _, err := s.SnoozeTodo(ctx, alex2, t2.ID, "1d"); err != nil {
		t.Fatal(err)
	}
	if active, _ := s.ListUserTodos(ctx, alexUser.ID, TodoFilterActive); len(active) != 1 || active[0].ID != t1.ID || active[0].WorkspaceName != "Alpha" {
		t.Errorf("active ListUserTodos = %+v, want only Alpha TODO", active)
	}
	if open, _ := s.ListUserTodos(ctx, alexUser.ID, TodoFilterOpen); len(open) != 2 {
		t.Errorf("open ListUserTodos = %+v, want 2", open)
	}

	// AutoClosePRTodos closes matching open TODOs case-insensitively.
	evs, err := s.AutoClosePRTodos(ctx, ws1.ID, "https://github.com/ACME/alpha/pull/9", "Auto-closed: PR merged")
	if err != nil || len(evs) != 1 || evs[0].Type != "todo.done" {
		t.Fatalf("AutoClosePRTodos = %+v, %v; want 1 todo.done event", evs, err)
	}
	doneList, _ := s.ListTodos(ctx, ws1.ID, TodoFilterDone, alex1.ID)
	if len(doneList) != 1 || doneList[0].DoneNote != "Auto-closed: PR merged" {
		t.Errorf("doneList after AutoClosePRTodos = %+v", doneList)
	}
}
