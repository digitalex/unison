package store

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

func TestSortBacklogItems(t *testing.T) {
	base := []Item{
		{ID: 1, Number: 1, Title: "charlie", Status: StatusOpen, Position: 2, CreatedAt: 100, UpdatedAt: 200},
		{ID: 2, Number: 2, Title: "Alpha", Status: StatusInProgress, Position: 1, CreatedAt: 300, UpdatedAt: 150},
		{ID: 3, Number: 3, Title: "alpha", Status: StatusOpen, Position: 3, CreatedAt: 300, UpdatedAt: 400},
		{ID: 4, Number: 4, Title: "Bravo", Status: StatusOpen, Position: 4, CreatedAt: 200, UpdatedAt: 400},
	}

	idsOf := func(items []Item) []int64 {
		out := make([]int64, len(items))
		for i, it := range items {
			out[i] = it.ID
		}
		return out
	}

	t.Run("default_and_priority_keep_canonical_order", func(t *testing.T) {
		for _, sortBy := range []string{"", "priority", " PRIORITY "} {
			got := slices.Clone(base)
			if err := SortBacklogItems(got, sortBy); err != nil {
				t.Fatalf("SortBacklogItems(%q): %v", sortBy, err)
			}
			if !slices.Equal(idsOf(got), []int64{1, 2, 3, 4}) {
				t.Errorf("SortBacklogItems(%q) = %v, want [1 2 3 4]", sortBy, idsOf(got))
			}
		}
	})

	t.Run("newest_orders_created_desc_then_id_desc", func(t *testing.T) {
		got := slices.Clone(base)
		if err := SortBacklogItems(got, "newest"); err != nil {
			t.Fatalf("SortBacklogItems(newest): %v", err)
		}
		if !slices.Equal(idsOf(got), []int64{3, 2, 4, 1}) {
			t.Errorf("newest = %v, want [3 2 4 1]", idsOf(got))
		}
	})

	t.Run("oldest_orders_created_asc_then_id_asc", func(t *testing.T) {
		got := slices.Clone(base)
		if err := SortBacklogItems(got, "oldest"); err != nil {
			t.Fatalf("SortBacklogItems(oldest): %v", err)
		}
		if !slices.Equal(idsOf(got), []int64{1, 4, 2, 3}) {
			t.Errorf("oldest = %v, want [1 4 2 3]", idsOf(got))
		}
	})

	t.Run("updated_orders_updated_desc_then_id_desc", func(t *testing.T) {
		got := slices.Clone(base)
		if err := SortBacklogItems(got, "updated"); err != nil {
			t.Fatalf("SortBacklogItems(updated): %v", err)
		}
		if !slices.Equal(idsOf(got), []int64{4, 3, 1, 2}) {
			t.Errorf("updated = %v, want [4 3 1 2]", idsOf(got))
		}
	})

	t.Run("title_orders_case_insensitive_then_position_asc_then_id_asc", func(t *testing.T) {
		got := slices.Clone(base)
		if err := SortBacklogItems(got, "title"); err != nil {
			t.Fatalf("SortBacklogItems(title): %v", err)
		}
		// "Alpha" (pos 1, id 2), "alpha" (pos 3, id 3), "Bravo" (id 4), "charlie" (id 1).
		if !slices.Equal(idsOf(got), []int64{2, 3, 4, 1}) {
			t.Errorf("title = %v, want [2 3 4 1]", idsOf(got))
		}
		// Positions inside items are untouched.
		if got[0].Position != 1 || got[1].Position != 3 || got[2].Position != 4 || got[3].Position != 2 {
			t.Errorf("SortBacklogItems mutated positions: %+v", got)
		}
	})

	t.Run("unknown_sort_returns_ErrInvalid", func(t *testing.T) {
		got := slices.Clone(base)
		if err := SortBacklogItems(got, "random"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("SortBacklogItems(random) = %v, want ErrInvalid", err)
		}
	})

	t.Run("db_positions_unchanged_after_sorted_read", func(t *testing.T) {
		f := newActionFixture(t)
		ctx := context.Background()
		now := time.UnixMilli(1_700_000_000_000)
		f.s.SetClock(func() time.Time { return now })

		first, _, err := f.s.AddItem(ctx, f.alex, "Zeta", "", 0)
		if err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Minute)
		second, _, err := f.s.AddItem(ctx, f.alex, "Alpha", "", 0)
		if err != nil {
			t.Fatal(err)
		}

		items, _, err := f.s.ListBacklog(ctx, f.ws.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := SortBacklogItems(items, "newest"); err != nil {
			t.Fatal(err)
		}
		if items[0].ID != second.ID || items[0].Position != 2 || items[1].ID != first.ID || items[1].Position != 1 {
			t.Fatalf("sorted items = %+v, want [#2(pos=2), #1(pos=1)]", items)
		}

		reloaded, _, err := f.s.ListBacklog(ctx, f.ws.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		if reloaded[0].ID != first.ID || reloaded[0].Position != 1 || reloaded[1].ID != second.ID || reloaded[1].Position != 2 {
			t.Fatalf("canonical DB order changed: %+v", reloaded)
		}
	})
}
