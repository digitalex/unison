package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNextDue(t *testing.T) {
	loc := time.FixedZone("server", 2*3600)
	wed := time.Date(2026, 10, 28, 15, 30, 0, 0, loc) // a Wednesday
	for _, c := range []struct {
		cadence, weekday string
		now, want        time.Time
	}{
		{CadenceDaily, "", wed, time.Date(2026, 10, 29, 0, 0, 0, 0, loc)},
		{CadenceDaily, "", time.Date(2026, 10, 28, 0, 0, 0, 0, loc), time.Date(2026, 10, 29, 0, 0, 0, 0, loc)},
		{CadenceWeekly, "fri", wed, time.Date(2026, 10, 30, 0, 0, 0, 0, loc)},
		{CadenceWeekly, "wed", wed, time.Date(2026, 11, 4, 0, 0, 0, 0, loc)},
		{CadenceWeekly, "thu", wed, time.Date(2026, 10, 29, 0, 0, 0, 0, loc)},
		{CadenceWeekly, "sun", time.Date(2026, 12, 31, 9, 0, 0, 0, loc), time.Date(2027, 1, 3, 0, 0, 0, 0, loc)},
	} {
		if got := nextDue(c.now, c.cadence, c.weekday); !got.Equal(c.want) {
			t.Errorf("nextDue(%v, %s %s) = %v, want %v", c.now, c.cadence, c.weekday, got, c.want)
		}
	}
}

func TestSchedules(t *testing.T) {
	s, now, a, b := setup(t)
	ctx := context.Background()
	for _, in := range []ScheduleInput{
		{Title: " ", Cadence: CadenceDaily},
		{Title: "x", Cadence: "hourly"},
		{Title: "x", Cadence: CadenceWeekly},
		{Title: "x", Cadence: CadenceWeekly, Weekday: "someday"},
		{Title: "x", Cadence: CadenceDaily, Weekday: "mon"},
	} {
		if _, _, err := s.AddSchedule(ctx, a, in); !errors.Is(err, ErrInvalid) {
			t.Errorf("AddSchedule(%+v) err = %v, want ErrInvalid", in, err)
		}
	}
	daily, evs, err := s.AddSchedule(ctx, a, ScheduleInput{Title: "Triage", Body: "Look at new issues", Cadence: CadenceDaily})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Type != "schedule.added" {
		t.Errorf("add events = %+v", evs)
	}
	if want := nextDue(*now, CadenceDaily, "").UnixMilli(); daily.NextDueAt != want {
		t.Errorf("next due = %d, want %d", daily.NextDueAt, want)
	}
	weekly, _, err := s.AddSchedule(ctx, b, ScheduleInput{Title: "Dependency updates", Cadence: CadenceWeekly, Weekday: "Monday"})
	if err != nil {
		t.Fatal(err)
	}
	if weekly.Weekday != "mon" {
		t.Errorf("weekday = %q, want mon", weekly.Weekday)
	}

	// Editing the cadence moves the next run; editing the title doesn't.
	title := "Release notes"
	got, _, err := s.EditSchedule(ctx, a, weekly.ID, SchedulePatch{Title: &title})
	if err != nil || got.Title != title || got.NextDueAt != weekly.NextDueAt {
		t.Fatalf("edit title = %+v, %v", got, err)
	}
	daily2 := CadenceDaily
	got, evs, err = s.EditSchedule(ctx, a, weekly.ID, SchedulePatch{Cadence: &daily2})
	if err != nil || got.Cadence != CadenceDaily || got.Weekday != "" || got.NextDueAt != daily.NextDueAt {
		t.Fatalf("edit cadence = %+v, %v", got, err)
	}
	if len(evs) != 1 || evs[0].Type != "schedule.updated" {
		t.Errorf("edit events = %+v", evs)
	}

	list, err := s.ListSchedules(ctx, a.WorkspaceID)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if evs, err := s.RemoveSchedule(ctx, a, daily.ID); err != nil || len(evs) != 1 || evs[0].Type != "schedule.removed" {
		t.Fatalf("remove = %+v, %v", evs, err)
	}
	if _, err := s.RemoveSchedule(ctx, a, daily.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second remove err = %v, want ErrNotFound", err)
	}
	if list, _ := s.ListSchedules(ctx, a.WorkspaceID); len(list) != 1 {
		t.Errorf("after remove list = %+v", list)
	}
}

// TestMaterializeSchedules: a due schedule becomes an item at the top of the
// backlog (backlog.added, linked by schedule_id), once; while that run is
// open, later runs are noted as missed instead of piling up; after it's done
// the next run adds a new item; a weekly one waits for its day; removing a
// schedule leaves its items and stops its runs.
func TestMaterializeSchedules(t *testing.T) {
	s, now, a, _ := setup(t)
	ctx := context.Background()
	sweep := func() []string {
		t.Helper()
		evs, err := s.MaterializeSchedules(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return eventTypes(evs)
	}
	advanceTo := func(ms int64) { *now = time.UnixMilli(ms) }
	queued := func() []Item {
		t.Helper()
		items, _, err := s.ListBacklog(ctx, a.WorkspaceID, "")
		if err != nil {
			t.Fatal(err)
		}
		return items
	}

	s.AddItem(ctx, a, "Existing work", "", 0)
	daily, _, _ := s.AddSchedule(ctx, a, ScheduleInput{Title: "Triage", Body: "Look at new issues", Cadence: CadenceDaily})
	if got := sweep(); len(got) != 0 {
		t.Fatalf("before it's due: events %v", got)
	}

	// Due: one item, on top, linked to the schedule; sweeping again adds nothing.
	advanceTo(daily.NextDueAt + 1000)
	if got := sweep(); !equal(got, []string{"backlog.added"}) {
		t.Fatalf("due: events %v, want one backlog.added", got)
	}
	items := queued()
	run := items[0]
	if run.Title != "Triage" || run.Body != "Look at new issues" || run.Position != 1 || run.ScheduleID == nil || *run.ScheduleID != daily.ID || run.CreatedBy != a.ID {
		t.Fatalf("run = %+v, want Triage on top, from schedule %d, by its creator", run, daily.ID)
	}
	if len(items) != 2 || items[1].Title != "Existing work" {
		t.Errorf("queue = %+v, want the run above the existing item", items)
	}
	if got := sweep(); len(got) != 0 {
		t.Errorf("same tick again: events %v", got)
	}
	list, _ := s.ListSchedules(ctx, a.WorkspaceID)
	if list[0].NextDueAt != nextDue(*now, CadenceDaily, "").UnixMilli() {
		t.Errorf("next due = %d, want the next midnight", list[0].NextDueAt)
	}

	// Next day, the run is still open: no second item, a missed-run comment.
	advanceTo(list[0].NextDueAt + 1000)
	if got := sweep(); !equal(got, []string{"comment.added"}) {
		t.Fatalf("missed: events %v, want a comment", got)
	}
	if n := len(queued()); n != 2 {
		t.Errorf("missed: %d queued items, want 2 (no pile-up)", n)
	}
	comments, _ := s.ListComments(ctx, a.WorkspaceID, run.ID)
	if len(comments) != 1 || !strings.Contains(comments[0].Body, "Missed") {
		t.Errorf("comments = %+v, want one missed-run note", comments)
	}

	// Done, then the next run adds a new item again.
	if _, _, err := s.CloseItem(ctx, a, run.ID, "triaged"); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListSchedules(ctx, a.WorkspaceID)
	advanceTo(list[0].NextDueAt + 1000)
	if got := sweep(); !equal(got, []string{"backlog.added"}) {
		t.Fatalf("after done: events %v", got)
	}
	if again := queued()[0]; again.ID == run.ID || again.ScheduleID == nil || *again.ScheduleID != daily.ID {
		t.Errorf("new run = %+v", again)
	}

	// A weekly schedule only runs on its day.
	day := weekdays[now.AddDate(0, 0, 3).Weekday()]
	weekly, _, _ := s.AddSchedule(ctx, a, ScheduleInput{Title: "Dependency updates", Cadence: CadenceWeekly, Weekday: day})
	if _, err := s.RemoveSchedule(ctx, a, daily.ID); err != nil {
		t.Fatal(err)
	}
	if weekly.NextDueAt <= now.UnixMilli() {
		t.Fatalf("weekly next due %d is not in the future", weekly.NextDueAt)
	}
	advanceTo(weekly.NextDueAt - 1000)
	if got := sweep(); len(got) != 0 {
		t.Errorf("before its day: events %v (the removed daily must not run either)", got)
	}
	advanceTo(weekly.NextDueAt + 1000)
	if got := sweep(); !equal(got, []string{"backlog.added"}) {
		t.Errorf("on its day: events %v", got)
	}

	// The removed daily's items stay.
	all, _, _ := s.ListBacklog(ctx, a.WorkspaceID, "all")
	fromDaily := 0
	for _, it := range all {
		if it.ScheduleID != nil && *it.ScheduleID == daily.ID {
			fromDaily++
		}
	}
	if fromDaily != 2 {
		t.Errorf("%d items from the removed schedule, want 2 (kept)", fromDaily)
	}
}
