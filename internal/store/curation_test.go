package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCommentsOnItems(t *testing.T) {
	s, _, a, b := setup(t)
	ctx := context.Background()
	it, _, _ := s.AddItem(ctx, a, "Write docs", "", 0)

	c1, evs, err := s.AddComment(ctx, a, it.ID, "  Where should these live?  ")
	if err != nil {
		t.Fatal(err)
	}
	if c1.Body != "Where should these live?" || len(evs) != 1 || evs[0].Type != "comment.added" {
		t.Errorf("comment = %+v, events = %+v", c1, evs)
	}
	c2, _, _ := s.AddComment(ctx, b, it.ID, "In the README, @Alex")
	if _, _, err := s.AddComment(ctx, a, it.ID, " "); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty comment err = %v, want ErrInvalid", err)
	}
	if _, _, err := s.AddComment(ctx, a, 999, "hi"); !errors.Is(err, ErrNotFound) {
		t.Errorf("comment on missing item err = %v, want ErrNotFound", err)
	}

	list, err := s.ListComments(ctx, a.WorkspaceID, it.ID)
	if err != nil || len(list) != 2 || list[0].ID != c1.ID || list[1].AuthorID != b.ID {
		t.Fatalf("ListComments = %+v, %v", list, err)
	}
	got, _, _ := s.GetItem(ctx, a.WorkspaceID, it.ID)
	if got.CommentCount != 2 {
		t.Errorf("comment_count = %d, want 2", got.CommentCount)
	}

	// Only the author can delete a comment.
	if _, err := s.DeleteComment(ctx, a, it.ID, c2.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("deleting someone else's comment err = %v, want ErrForbidden", err)
	}
	evs, err = s.DeleteComment(ctx, b, it.ID, c2.ID)
	if err != nil || len(evs) != 1 || evs[0].Type != "comment.deleted" {
		t.Fatalf("DeleteComment = %+v, %v", evs, err)
	}
	if _, err := s.DeleteComment(ctx, b, it.ID, c2.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting twice err = %v, want ErrNotFound", err)
	}
	items, _, _ := s.ListBacklog(ctx, a.WorkspaceID, "")
	if items[0].CommentCount != 1 {
		t.Errorf("listed comment_count = %d, want 1", items[0].CommentCount)
	}
}

func TestPRLinks(t *testing.T) {
	s, _, a, b := setup(t)
	ctx := context.Background()
	it, _, _ := s.AddItem(ctx, a, "Fix login", "", 0)
	other, _, _ := s.AddItem(ctx, a, "Other", "", 0)
	pr1 := "https://github.com/digitalex/unison/pull/4"
	pr2 := "https://github.com/acme/auth/pull/12"

	got, evs, err := s.LinkPR(ctx, a, it.ID, pr1)
	if err != nil || len(got.PRs) != 1 || got.PRs[0].URL != pr1 || evs[len(evs)-1].Type != "pr.linked" {
		t.Fatalf("LinkPR = %+v, %+v, %v", got, evs, err)
	}
	if got, _, err = s.LinkPR(ctx, b, it.ID, pr2); err != nil || len(got.PRs) != 2 {
		t.Fatalf("second PR = %+v, %v", got, err)
	}
	if _, _, err := s.LinkPR(ctx, a, it.ID, pr1); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate link err = %v, want ErrConflict", err)
	}
	if _, _, err := s.LinkPR(ctx, a, other.ID, pr1); err != nil {
		t.Errorf("the same PR on another item: %v", err)
	}
	for _, bad := range []string{"", "github.com/x/y/pull/1", "javascript:alert(1)", "ftp://example.com/x"} {
		if _, _, err := s.LinkPR(ctx, a, it.ID, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("LinkPR(%q) err = %v, want ErrInvalid", bad, err)
		}
	}
	items, _, _ := s.ListBacklog(ctx, a.WorkspaceID, "")
	if len(items[0].PRs) != 2 || len(items[1].PRs) != 1 {
		t.Errorf("listed PRs = %+v / %+v", items[0].PRs, items[1].PRs)
	}

	got, evs, err = s.UnlinkPR(ctx, a, it.ID, pr1)
	if err != nil || len(got.PRs) != 1 || got.PRs[0].URL != pr2 || evs[len(evs)-1].Type != "pr.unlinked" {
		t.Fatalf("UnlinkPR = %+v, %+v, %v", got, evs, err)
	}
	if _, _, err := s.UnlinkPR(ctx, a, it.ID, pr1); !errors.Is(err, ErrNotFound) {
		t.Errorf("unlinking twice err = %v, want ErrNotFound", err)
	}
}

func TestCloseNoteLinksGitHubPRs(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()
	it, _, _ := s.AddItem(ctx, a, "Ship it", "", 0)
	s.LinkPR(ctx, a, it.ID, "https://github.com/digitalex/unison/pull/7")
	note := "Done in https://github.com/digitalex/unison/pull/7 and https://github.com/digitalex/unison/pull/8. See https://example.com/notes"
	closed, evs, err := s.CloseItem(ctx, a, it.ID, note)
	if err != nil {
		t.Fatal(err)
	}
	if len(closed.PRs) != 2 || closed.PRs[1].URL != "https://github.com/digitalex/unison/pull/8" {
		t.Errorf("PRs after close = %+v, want #7 (already linked) and #8", closed.PRs)
	}
	var types []string
	for _, e := range evs {
		types = append(types, e.Type)
	}
	if len(types) != 2 || types[0] != "pr.linked" || types[1] != "backlog.closed" {
		t.Errorf("events = %v, want [pr.linked backlog.closed]", types)
	}
}

func TestDeleteItemRemovesCommentsAndPRs(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()
	it, _, _ := s.AddItem(ctx, a, "Doomed", "", 0)
	s.AddComment(ctx, a, it.ID, "bye")
	s.LinkPR(ctx, a, it.ID, "https://github.com/digitalex/unison/pull/1")
	if _, err := s.DeleteItem(ctx, a, it.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListComments(ctx, a.WorkspaceID, it.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("comments of a deleted item err = %v, want ErrNotFound", err)
	}
}

func TestMoveAcrossAssignedItems(t *testing.T) {
	// Positions form one queue across assigned and open items; the web UI
	// moves an item to the position of its neighbour within a group.
	s, _, a, _ := setup(t)
	ctx := context.Background()
	var ids []int64
	for _, title := range []string{"one", "two", "three", "four"} {
		it, _, _ := s.AddItem(ctx, a, title, "", 0)
		ids = append(ids, it.ID)
	}
	s.AssignItem(ctx, a, ids[2], a.ID) // "three" (position 3) is in progress.
	// Move "four" (4) up past the assigned item, onto "two"'s position (2).
	if _, _, err := s.MoveItem(ctx, a, ids[3], 2); err != nil {
		t.Fatal(err)
	}
	items, _, _ := s.ListBacklog(ctx, a.WorkspaceID, "")
	var order []string
	for _, it := range items {
		order = append(order, it.Title)
	}
	if want := "one four two three"; strings.Join(order, " ") != want {
		t.Errorf("queue = %q, want %q", strings.Join(order, " "), want)
	}
}
