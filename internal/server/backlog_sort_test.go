package server

import (
	"net/http"
	"slices"
	"testing"
	"time"
)

func TestBacklogSortAPI(t *testing.T) {
	ts, st := newServer(t)
	ctx := t.Context()

	now := time.UnixMilli(1_700_000_000_000)
	st.SetClock(func() time.Time { return now })

	ws, _, err := st.CreateWorkspace(ctx, "SortWS", "", "")
	if err != nil {
		t.Fatal(err)
	}
	alex, tok, _, _ := joinMember(t, st, ws.ID, "Alex", "🧑")
	api := ts.URL + "/api/workspaces/" + ws.ID

	it1, _, err := st.AddItem(ctx, alex, "charlie task", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Second)
	it2, _, err := st.AddItem(ctx, alex, "Alpha task", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Second)
	it3, _, err := st.AddItem(ctx, alex, "bravo task", "", 0)
	if err != nil {
		t.Fatal(err)
	}

	// Update it1 so it becomes the most recently updated item while remaining oldest and position 1.
	now = now.Add(10 * time.Second)
	call(t, "PATCH", api+"/backlog/1", tok, `{"body":"touched"}`)

	nums := func(list []map[string]any) []int64 {
		out := make([]int64, len(list))
		for i, m := range list {
			out[i] = int64(m["id"].(float64))
		}
		return out
	}

	for _, tc := range []struct {
		query string
		want  []int64
	}{
		{"", []int64{it1.ID, it2.ID, it3.ID}},
		{"?sort=priority", []int64{it1.ID, it2.ID, it3.ID}},
		{"?sort=newest", []int64{it3.ID, it2.ID, it1.ID}},
		{"?sort=oldest", []int64{it1.ID, it2.ID, it3.ID}},
		{"?sort=updated", []int64{it1.ID, it3.ID, it2.ID}},
		{"?sort=title", []int64{it2.ID, it3.ID, it1.ID}},
	} {
		status, list := callList(t, api+"/backlog"+tc.query, tok)
		if status != http.StatusOK {
			t.Fatalf("GET /backlog%s = %d", tc.query, status)
		}
		if got := nums(list); !slices.Equal(got, tc.want) {
			t.Errorf("GET /backlog%s ids = %v, want %v", tc.query, got, tc.want)
		}
	}

	// Invalid sort returns 400 Bad Request.
	status, errBody := call(t, "GET", api+"/backlog?sort=bogus", tok, "")
	if status != http.StatusBadRequest || errBody["error"] == nil {
		t.Fatalf("GET /backlog?sort=bogus = %d %v, want 400", status, errBody)
	}

	// Canonical positions remain untouched after all view-only sorted queries.
	_, canonical := callList(t, api+"/backlog", tok)
	if canonical[0]["position"] != 1.0 || canonical[1]["position"] != 2.0 || canonical[2]["position"] != 3.0 {
		t.Fatalf("canonical positions mutated: %v", canonical)
	}
}
