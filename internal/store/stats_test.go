package store

import (
	"context"
	"testing"
	"time"
)

// Workspace stats count rolling windows that end now (#301).
func TestWorkspaceStats(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	stats := func() WorkspaceStats {
		t.Helper()
		st, err := s.WorkspaceStats(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	if got := stats(); got != (WorkspaceStats{}) {
		t.Fatalf("empty server: %+v, want zeros", got)
	}

	end := *now
	// One workspace per age, oldest first.
	for _, age := range []time.Duration{
		30 * 24 * time.Hour,        // long ago
		7*24*time.Hour + time.Hour, // just outside the week
		7*24*time.Hour - time.Hour, // just inside it
		25 * time.Hour,             // yesterday, by the clock on the wall
		23 * time.Hour,             // inside the past 24 hours
		0,                          // right now
	} {
		*now = end.Add(-age)
		if _, _, err := s.CreateWorkspace(ctx, "Acme", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	*now = end
	if got, want := stats(), (WorkspaceStats{Total: 6, CreatedLast7Days: 4, CreatedToday: 2}); got != want {
		t.Fatalf("stats: %+v, want %+v", got, want)
	}

	// The windows move with the clock; the total does not.
	*now = end.Add(2 * time.Hour)
	if got, want := stats(), (WorkspaceStats{Total: 6, CreatedLast7Days: 3, CreatedToday: 1}); got != want {
		t.Fatalf("two hours on: %+v, want %+v", got, want)
	}
}
