package store

import (
	"context"
	"errors"
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
