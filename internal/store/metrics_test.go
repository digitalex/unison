package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestAddMetricCreatesTrackingItem(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()
	s.AddItem(ctx, a, "existing work", "", 0)
	target := 95.0
	m, evs, err := s.AddMetric(ctx, a, NewMetric{Name: "Uptime", Description: "Share of time the server answers", Unit: "%", Target: &target})
	if err != nil {
		t.Fatal(err)
	}
	if m.Direction != DirectionIncrease || m.ItemID == nil || *m.Target != 95 || m.Unit != "%" {
		t.Fatalf("metric = %+v", m)
	}
	it, _, err := s.GetItem(ctx, a.WorkspaceID, *m.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != "Track metric: Uptime" || it.Status != StatusOpen || it.Position != 2 {
		t.Errorf("tracking item = %+v, want open at the bottom (position 2)", it)
	}
	for _, want := range []string{"measured and recorded", "target: 95 %", "Share of time the server answers"} {
		if !strings.Contains(it.Body, want) {
			t.Errorf("tracking item body %q lacks %q", it.Body, want)
		}
	}
	if len(evs) != 2 || evs[0].Type != "backlog.added" || evs[1].Type != "metric.added" {
		t.Fatalf("events = %+v", evs)
	}
	var evMetric Metric
	json.Unmarshal(evs[1].Data, &evMetric)
	if evMetric.ItemID == nil || *evMetric.ItemID != it.ID {
		t.Errorf("metric.added data = %s, want item_id %d", evs[1].Data, it.ID)
	}
	got, err := s.GetMetric(ctx, a.WorkspaceID, m.ID)
	if err != nil || got.ItemID == nil || *got.ItemID != it.ID {
		t.Errorf("GetMetric = %+v, %v", got, err)
	}
}

func TestAddMetricDuplicateNameIsAtomic(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()
	if _, _, err := s.AddMetric(ctx, a, NewMetric{Name: "Latency", Direction: "decrease", Unit: "ms"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddMetric(ctx, a, NewMetric{Name: "LATENCY"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate (different case) err = %v, want ErrConflict", err)
	}
	items, _, _ := s.ListBacklog(ctx, a.WorkspaceID, "all")
	if len(items) != 1 {
		t.Errorf("a failed add left %d backlog items, want 1", len(items))
	}
	metrics, _ := s.ListMetrics(ctx, a.WorkspaceID)
	if len(metrics) != 1 || metrics[0].Direction != DirectionDecrease {
		t.Errorf("metrics = %+v", metrics)
	}
}

func TestAddMetricValidation(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()
	for _, nm := range []NewMetric{{Name: " "}, {Name: "x", Direction: "sideways"}} {
		if _, _, err := s.AddMetric(ctx, a, nm); !errors.Is(err, ErrInvalid) {
			t.Errorf("AddMetric(%+v) err = %v, want ErrInvalid", nm, err)
		}
	}
}

func TestEditAndDeleteMetric(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()
	target := 100.0
	m, _, _ := s.AddMetric(ctx, a, NewMetric{Name: "Latency", Target: &target})
	other, _, _ := s.AddMetric(ctx, a, NewMetric{Name: "Errors"})

	var p MetricPatch
	if err := json.Unmarshal([]byte(`{"name":"p95 latency","direction":"decrease","unit":"ms","target":null}`), &p); err != nil {
		t.Fatal(err)
	}
	got, evs, err := s.EditMetric(ctx, a, m.ID, p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "p95 latency" || got.Direction != DirectionDecrease || got.Unit != "ms" || got.Target != nil {
		t.Errorf("edited = %+v", got)
	}
	if len(evs) != 1 || evs[0].Type != "metric.updated" {
		t.Errorf("events = %+v", evs)
	}
	// An absent field is left alone.
	got, _, _ = s.EditMetric(ctx, a, m.ID, MetricPatch{Description: ptr("p95 of API requests")})
	if got.Unit != "ms" || got.Description != "p95 of API requests" {
		t.Errorf("partial edit = %+v", got)
	}
	if _, _, err := s.EditMetric(ctx, a, m.ID, MetricPatch{Name: ptr("errors")}); !errors.Is(err, ErrConflict) {
		t.Errorf("rename onto %q err = %v, want ErrConflict", other.Name, err)
	}
	if _, _, err := s.EditMetric(ctx, a, m.ID, MetricPatch{Direction: ptr("up")}); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad direction err = %v, want ErrInvalid", err)
	}

	evs, err = s.DeleteMetric(ctx, a, m.ID)
	if err != nil || len(evs) != 1 || evs[0].Type != "metric.deleted" {
		t.Fatalf("DeleteMetric = %+v, %v", evs, err)
	}
	if _, err := s.GetMetric(ctx, a.WorkspaceID, m.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted metric err = %v", err)
	}
	if _, _, err := s.GetItem(ctx, a.WorkspaceID, *m.ItemID); err != nil {
		t.Errorf("deleting a metric removed its tracking item: %v", err)
	}
}

func TestDeletingTrackingItemUnlinksMetric(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()
	m, _, _ := s.AddMetric(ctx, a, NewMetric{Name: "Signups"})
	evs, err := s.DeleteItem(ctx, a, *m.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[0].Type != "metric.updated" || evs[1].Type != "backlog.deleted" {
		t.Errorf("events = %+v", evs)
	}
	got, _ := s.GetMetric(ctx, a.WorkspaceID, m.ID)
	if got.ItemID != nil {
		t.Errorf("item_id = %d, want unlinked", *got.ItemID)
	}
}

func ptr[T any](v T) *T { return &v }
