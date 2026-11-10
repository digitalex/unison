package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestLinks (#299): add, edit and remove a workspace's custom links, in
// order, with events; validation; and links go with their workspace.
func TestLinks(t *testing.T) {
	s, _, a, b := setup(t)
	ctx := context.Background()
	for _, in := range []LinkInput{
		{Name: " ", URL: "https://example.com"},
		{Name: "two\nlines", URL: "https://example.com"},
		{Name: strings.Repeat("x", MaxLinkName+1), URL: "https://example.com"},
		{Name: "Spec", URL: "example.com/spec"},
		{Name: "Spec", URL: "javascript:alert(1)"},
	} {
		if _, _, err := s.AddLink(ctx, a, in); !errors.Is(err, ErrInvalid) {
			t.Errorf("AddLink(%+v) err = %v, want ErrInvalid", in, err)
		}
	}

	spec, evs, err := s.AddLink(ctx, a, LinkInput{Name: "  Spec  ", URL: " https://example.com/spec "})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Name != "Spec" || spec.URL != "https://example.com/spec" || spec.Position != 1 || spec.AddedBy != a.ID {
		t.Errorf("added = %+v", spec)
	}
	if len(evs) != 1 || evs[0].Type != "link.added" || evs[0].ActorID != a.ID {
		t.Errorf("add events = %+v", evs)
	}
	dash, _, err := s.AddLink(ctx, b, LinkInput{Name: "Dashboard", URL: "https://grafana.example.com/d/1"})
	if err != nil || dash.Position != 2 {
		t.Fatalf("second link = %+v, %v", dash, err)
	}

	name := "Design doc"
	got, evs, err := s.EditLink(ctx, b, spec.ID, LinkPatch{Name: &name})
	if err != nil || got.Name != name || got.URL != spec.URL || len(evs) != 1 || evs[0].Type != "link.updated" {
		t.Fatalf("edit = %+v, %+v, %v", got, evs, err)
	}
	bad := "ftp://example.com"
	if _, _, err := s.EditLink(ctx, a, spec.ID, LinkPatch{URL: &bad}); !errors.Is(err, ErrInvalid) {
		t.Errorf("edit to ftp err = %v, want ErrInvalid", err)
	}

	ws, err := s.GetWorkspace(ctx, a.WorkspaceID)
	if err != nil || len(ws.Links) != 2 || ws.Links[0].Name != "Design doc" || ws.Links[1].Name != "Dashboard" {
		t.Fatalf("workspace links = %+v, %v", ws.Links, err)
	}

	evs, err = s.RemoveLink(ctx, a, spec.ID)
	if err != nil || len(evs) != 1 || evs[0].Type != "link.removed" {
		t.Fatalf("remove = %+v, %v", evs, err)
	}
	if _, err := s.RemoveLink(ctx, a, spec.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second remove err = %v, want ErrNotFound", err)
	}
	list, err := s.ListLinks(ctx, a.WorkspaceID)
	if err != nil || len(list) != 1 || list[0].ID != dash.ID || list[0].Position != 1 {
		t.Fatalf("after remove = %+v, %v (want the dashboard, renumbered to 1)", list, err)
	}

	// Another workspace's participant can't touch it.
	other, _, _ := s.CreateWorkspace(ctx, "Other", "", "")
	stranger, _, _, err := s.Join(ctx, other.ID, "Eve", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveLink(ctx, stranger, dash.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("remove from another workspace err = %v, want ErrNotFound", err)
	}

	if err := s.DeleteWorkspace(ctx, a.WorkspaceID, 0); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM workspace_links`).Scan(&n)
	if n != 0 {
		t.Errorf("%d links left after deleting the workspace", n)
	}
}
