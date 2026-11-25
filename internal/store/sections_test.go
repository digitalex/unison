package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSectionsLifecycleAndQueueIsolation(t *testing.T) {
	f := newActionFixture(t)
	ctx := context.Background()

	// All 15 supported Lucide icons are accepted; unknown icons are rejected.
	for _, icon := range SectionIcons {
		if got, err := validSectionIcon(icon); err != nil || got != icon {
			t.Fatalf("validSectionIcon(%q) = %q, %v", icon, got, err)
		}
	}
	if _, _, err := f.s.AddSection(ctx, f.alex, SectionInput{Name: "Bad", Icon: "not-an-icon"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("unsupported icon = %v, want ErrInvalid", err)
	}
	if _, _, err := f.s.AddSection(ctx, f.alex, SectionInput{Name: "Bad", Slug: "all"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("reserved slug 'all' = %v, want ErrInvalid", err)
	}
	if _, _, err := f.s.AddSection(ctx, f.alex, SectionInput{Name: "Bad", Slug: "123"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("numeric slug = %v, want ErrInvalid", err)
	}

	triage, evs, err := f.s.AddSection(ctx, f.alex, SectionInput{Name: "Triage", Icon: "inbox"})
	if err != nil {
		t.Fatalf("AddSection(Triage): %v", err)
	}
	if triage.Slug != "triage" || triage.Icon != "inbox" || triage.Notify || triage.Position != 1 || len(evs) != 1 || evs[0].Type != "section.created" {
		t.Fatalf("triage = %+v, evs = %+v", triage, evs)
	}

	decisions, _, err := f.s.AddSection(ctx, f.alex, SectionInput{Name: "Tech Decisions!", Icon: "scale", Notify: true})
	if err != nil {
		t.Fatalf("AddSection(Decisions): %v", err)
	}
	if decisions.Slug != "tech-decisions" || !decisions.Notify || decisions.Position != 2 {
		t.Fatalf("decisions = %+v", decisions)
	}

	research, _, err := f.s.AddSection(ctx, f.alex, SectionInput{Name: "Research", Icon: "flask-conical"})
	if err != nil {
		t.Fatalf("AddSection(Research): %v", err)
	}

	// Duplicate slug rejected with ErrConflict.
	if _, _, err := f.s.AddSection(ctx, f.alex, SectionInput{Name: "Triage"}); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate slug = %v, want ErrConflict", err)
	}

	// Reorder: move Research to position 1.
	moved, evs, err := f.s.MoveSection(ctx, f.alex, "research", 1)
	if err != nil || moved.Position != 1 || len(evs) != 1 || evs[0].Type != "section.moved" {
		t.Fatalf("MoveSection = %+v, %+v, %v", moved, evs, err)
	}
	secs, err := f.s.ListSections(ctx, f.ws.ID)
	if err != nil || len(secs) != 3 || secs[0].ID != research.ID || secs[1].ID != triage.ID || secs[2].ID != decisions.ID {
		t.Fatalf("reordered sections = %+v, %v", secs, err)
	}

	// Edit section by slug and by ID.
	newName, newSlug, newIcon := "Decisions", "decisions", "lightbulb"
	edited, evs, err := f.s.UpdateSection(ctx, f.alex, "tech-decisions", SectionPatch{Name: &newName, Slug: &newSlug, Icon: &newIcon})
	if err != nil || edited.Name != "Decisions" || edited.Slug != "decisions" || edited.Icon != "lightbulb" || !edited.Notify || len(evs) != 1 || evs[0].Type != "section.updated" {
		t.Fatalf("UpdateSection = %+v, %+v, %v", edited, evs, err)
	}

	// Independent positions per section + ClaimNext skipping custom sections.
	tr1, _, err := f.s.AddItemWithOptions(ctx, f.alex, ItemInput{Title: "Triage bug A", Section: "triage"})
	if err != nil {
		t.Fatal(err)
	}
	tr2, _, err := f.s.AddItemWithOptions(ctx, f.alex, ItemInput{Title: "Triage bug B", Section: "triage"})
	if err != nil {
		t.Fatal(err)
	}
	if tr1.Position != 1 || tr2.Position != 2 {
		t.Errorf("triage positions = %d, %d; want 1, 2", tr1.Position, tr2.Position)
	}

	// Main Backlog is empty, so ClaimNext returns ErrNotFound despite open Triage items!
	if claimed, _, err := f.s.ClaimNext(ctx, f.wren); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ClaimNext with only section items = %+v, %v; want ErrNotFound", claimed, err)
	}

	main1, _, err := f.s.AddItem(ctx, f.alex, "Ship feature 1", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if main1.Position != 1 || main1.SectionID != nil {
		t.Errorf("main1 = %+v, want position=1 section_id=nil", main1)
	}

	// ListBacklog defaults to main Backlog only; ListBacklogSection filters by slug or "all".
	mainList, _, err := f.s.ListBacklog(ctx, f.ws.ID, "")
	if err != nil || len(mainList) != 1 || mainList[0].ID != main1.ID {
		t.Fatalf("ListBacklog = %+v, %v; want only main1", mainList, err)
	}
	triageList, _, err := f.s.ListBacklogSection(ctx, f.ws.ID, "", "triage")
	if err != nil || len(triageList) != 2 || triageList[0].ID != tr1.ID || triageList[1].ID != tr2.ID {
		t.Fatalf("ListBacklogSection(triage) = %+v, %v", triageList, err)
	}
	allList, _, err := f.s.ListBacklogSection(ctx, f.ws.ID, "", "all")
	if err != nil || len(allList) != 3 {
		t.Fatalf("ListBacklogSection(all) = %+v, %v; want 3 items", allList, err)
	}

	// ClaimNext claims main1 (skipping tr1 & tr2).
	claimed, _, err := f.s.ClaimNext(ctx, f.wren)
	if err != nil || claimed.ID != main1.ID {
		t.Fatalf("ClaimNext = %+v, %v; want main1", claimed, err)
	}

	// DeleteSection moves remaining Triage items to the bottom of main Backlog and emits section.deleted.
	delSec, evs, err := f.s.DeleteSection(ctx, f.alex, "triage")
	if err != nil || delSec.ID != triage.ID || len(evs) != 1 || evs[0].Type != "section.deleted" {
		t.Fatalf("DeleteSection = %+v, %+v, %v", delSec, evs, err)
	}
	mainAfter, _, err := f.s.ListBacklog(ctx, f.ws.ID, "")
	if err != nil || len(mainAfter) != 3 {
		t.Fatalf("main backlog after DeleteSection = %+v, %v; want 3 items", mainAfter, err)
	}
	if mainAfter[0].ID != main1.ID || mainAfter[0].Position != 1 ||
		mainAfter[1].ID != tr1.ID || mainAfter[1].Position != 2 || mainAfter[1].SectionID != nil ||
		mainAfter[2].ID != tr2.ID || mainAfter[2].Position != 3 || mainAfter[2].SectionID != nil {
		t.Errorf("main backlog order/positions after DeleteSection = %+v", mainAfter)
	}
}

func TestSectionNotifyAndStructuredChoices(t *testing.T) {
	f := newActionFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 19, 0, 0, 0, time.UTC)
	f.s.SetClock(func() time.Time { return now })

	decSec, _, err := f.s.AddSection(ctx, f.alex, SectionInput{Name: "Decisions", Icon: "scale", Notify: true})
	if err != nil {
		t.Fatal(err)
	}
	quietSec, _, err := f.s.AddSection(ctx, f.alex, SectionInput{Name: "Research", Icon: "flask-conical", Notify: false})
	if err != nil {
		t.Fatal(err)
	}

	// Quiet section item assigned to human does NOT surface in CountOpenTodos / ListTodos / ListUserTodos.
	if _, _, err := f.s.AddItemWithOptions(ctx, f.wren, ItemInput{
		Title:      "Read paper",
		SectionID:  &quietSec.ID,
		AssigneeID: f.alex.ID,
	}); err != nil {
		t.Fatal(err)
	}
	// Notify section item assigned to an agent does NOT surface in human TODOs.
	if _, _, err := f.s.AddItemWithOptions(ctx, f.alex, ItemInput{
		Title:      "Agent decision",
		SectionID:  &decSec.ID,
		AssigneeID: f.wren.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if n, err := f.s.CountOpenTodos(ctx, f.alex); err != nil || n != 0 {
		t.Fatalf("CountOpenTodos(Alex) before notify item = %d, %v; want 0", n, err)
	}
	if n, err := f.s.CountOpenTodos(ctx, f.wren); err != nil || n != 0 {
		t.Fatalf("CountOpenTodos(Wren) = %d, %v; want 0", n, err)
	}

	// Add two decision items in notify=true section assigned to human Alex:
	// 1. close_on_answer = false
	openDec, _, err := f.s.AddItemWithOptions(ctx, f.wren, ItemInput{
		Title:         "Pick theme accent",
		Body:          "Choose primary palette",
		Section:       "decisions",
		AssigneeID:    f.alex.ID,
		Choices:       []string{"Warm neutral", "Cool slate", "Forest"},
		CloseOnAnswer: false,
	})
	if err != nil {
		t.Fatalf("AddItemWithOptions(openDec): %v", err)
	}
	// 2. close_on_answer = true
	autoDec, _, err := f.s.AddItemWithOptions(ctx, f.wren, ItemInput{
		Title:         "Approve schema rollout",
		Section:       "decisions",
		AssigneeID:    f.alex.ID,
		Choices:       []string{"Approve", "Hold"},
		CloseOnAnswer: true,
	})
	if err != nil {
		t.Fatalf("AddItemWithOptions(autoDec): %v", err)
	}

	// Both surface in CountOpenTodos, ListTodos, and ListUserTodos!
	if n, err := f.s.CountOpenTodos(ctx, f.alex); err != nil || n != 2 {
		t.Fatalf("CountOpenTodos(Alex) = %d, %v; want 2", n, err)
	}
	wsTodos, err := f.s.ListTodos(ctx, f.ws.ID, TodoFilterActive, f.alex.ID)
	if err != nil || len(wsTodos) != 2 || wsTodos[0].Kind != TodoKindSectionItem {
		t.Fatalf("ListTodos(Alex) = %+v, %v; want 2 section_item entries", wsTodos, err)
	}
	userTodos, err := f.s.ListUserTodos(ctx, f.alex.UserID, TodoFilterActive)
	if err != nil || len(userTodos) != 2 || userTodos[0].SectionSlug != "decisions" {
		t.Fatalf("ListUserTodos(Alex) = %+v, %v; want 2 entries", userTodos, err)
	}

	// Invalid choice is rejected.
	if _, _, err := f.s.AnswerItem(ctx, f.alex, openDec.ID, "Nonexistent"); !errors.Is(err, ErrInvalid) {
		t.Errorf("invalid choice = %v, want ErrInvalid", err)
	}

	// Answering openDec (close_on_answer=false) by 1-based index records the answer, adds a comment, and keeps it open!
	ans1, evs1, err := f.s.AnswerItem(ctx, f.alex, openDec.ID, "2")
	if err != nil {
		t.Fatalf("AnswerItem(openDec): %v", err)
	}
	if ans1.Status != StatusOpen || ans1.AnsweredChoice != "Cool slate" || ans1.AnsweredByID == nil || *ans1.AnsweredByID != f.alex.ID || ans1.CommentCount != 1 {
		t.Fatalf("answered openDec = %+v", ans1)
	}
	var hasAnsweredEv, hasClosedEv bool
	for _, e := range evs1 {
		if e.Type == "backlog.answered" {
			hasAnsweredEv = true
		}
		if e.Type == "backlog.closed" {
			hasClosedEv = true
		}
	}
	if !hasAnsweredEv || hasClosedEv {
		t.Errorf("openDec events = %+v, want backlog.answered without backlog.closed", evs1)
	}
	// Still open, so CountOpenTodos is still 2.
	if n, _ := f.s.CountOpenTodos(ctx, f.alex); n != 2 {
		t.Errorf("CountOpenTodos after non-closing answer = %d, want 2", n)
	}

	// Answering autoDec (close_on_answer=true) resolves AND atomically closes the item!
	ans2, evs2, err := f.s.AnswerItem(ctx, f.alex, autoDec.ID, "Approve")
	if err != nil {
		t.Fatalf("AnswerItem(autoDec): %v", err)
	}
	if ans2.Status != StatusDone || ans2.AnsweredChoice != "Approve" || !strings.Contains(ans2.CloseNote, "Decision: Approve") {
		t.Fatalf("answered autoDec = %+v", ans2)
	}
	hasAnsweredEv, hasClosedEv = false, false
	for _, e := range evs2 {
		if e.Type == "backlog.answered" {
			hasAnsweredEv = true
		}
		if e.Type == "backlog.closed" {
			hasClosedEv = true
		}
	}
	if !hasAnsweredEv || !hasClosedEv {
		t.Errorf("autoDec events = %+v, want both backlog.answered and backlog.closed", evs2)
	}
	// Closed item drops out of CountOpenTodos and active ListUserTodos!
	if n, _ := f.s.CountOpenTodos(ctx, f.alex); n != 1 {
		t.Errorf("CountOpenTodos after auto-close answer = %d, want 1", n)
	}
	if activeUser, _ := f.s.ListUserTodos(ctx, f.alex.UserID, TodoFilterActive); len(activeUser) != 1 || activeUser[0].ID != openDec.ID {
		t.Errorf("active ListUserTodos after auto-close = %+v, want only openDec", activeUser)
	}
	if doneUser, _ := f.s.ListUserTodos(ctx, f.alex.UserID, TodoFilterDone); len(doneUser) != 1 || doneUser[0].ID != autoDec.ID {
		t.Errorf("done ListUserTodos after auto-close = %+v, want autoDec", doneUser)
	}
}
