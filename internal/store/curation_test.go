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
	s.StartItem(ctx, a, ids[2]) // "three" (position 3) is in progress.
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

func TestAttachExtrasMultiItem(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()

	it1, _, _ := s.AddItem(ctx, a, "Item One", "", 0)
	it2, _, _ := s.AddItem(ctx, a, "Item Two", "", 0)
	it3, _, _ := s.AddItem(ctx, a, "Item Three", "", 0)

	// Add PRs
	s.LinkPR(ctx, a, it1.ID, "https://github.com/digitalex/unison/pull/1")
	s.LinkPR(ctx, a, it2.ID, "https://github.com/digitalex/unison/pull/2")
	s.LinkPR(ctx, a, it3.ID, "https://github.com/digitalex/unison/pull/3")

	// Add comments
	s.AddComment(ctx, a, it1.ID, "Comment on item 1 - A")
	s.AddComment(ctx, a, it1.ID, "Comment on item 1 - B")
	s.AddComment(ctx, a, it2.ID, "Comment on item 2 - A")

	// Add messages attached to items
	s.PostMessage(ctx, a, "Msg on item 1", nil, &it1.ID)
	s.PostMessage(ctx, a, "Msg on item 2", nil, &it2.ID)
	s.PostMessage(ctx, a, "Msg on item 3", nil, &it3.ID)

	// Test ListBacklog (fetches all 3 items together in multi-item path)
	items, _, err := s.ListBacklog(ctx, a.WorkspaceID, "")
	if err != nil {
		t.Fatalf("ListBacklog failed: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("ListBacklog returned %d items, want 3", len(items))
	}

	byID := make(map[int64]Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}

	if len(byID[it1.ID].PRs) != 1 || byID[it1.ID].PRs[0].URL != "https://github.com/digitalex/unison/pull/1" {
		t.Errorf("it1 PRs = %+v", byID[it1.ID].PRs)
	}
	if byID[it1.ID].CommentCount != 2 {
		t.Errorf("it1 CommentCount = %d, want 2", byID[it1.ID].CommentCount)
	}
	if byID[it1.ID].MessageCount != 1 {
		t.Errorf("it1 MessageCount = %d, want 1", byID[it1.ID].MessageCount)
	}

	if len(byID[it2.ID].PRs) != 1 || byID[it2.ID].PRs[0].URL != "https://github.com/digitalex/unison/pull/2" {
		t.Errorf("it2 PRs = %+v", byID[it2.ID].PRs)
	}
	if byID[it2.ID].CommentCount != 1 {
		t.Errorf("it2 CommentCount = %d, want 1", byID[it2.ID].CommentCount)
	}
	if byID[it2.ID].MessageCount != 1 {
		t.Errorf("it2 MessageCount = %d, want 1", byID[it2.ID].MessageCount)
	}

	if len(byID[it3.ID].PRs) != 1 || byID[it3.ID].PRs[0].URL != "https://github.com/digitalex/unison/pull/3" {
		t.Errorf("it3 PRs = %+v", byID[it3.ID].PRs)
	}
	if byID[it3.ID].CommentCount != 0 {
		t.Errorf("it3 CommentCount = %d, want 0", byID[it3.ID].CommentCount)
	}
	if byID[it3.ID].MessageCount != 1 {
		t.Errorf("it3 MessageCount = %d, want 1", byID[it3.ID].MessageCount)
	}

	// Test attachExtras directly with a slice containing only [it1, it2]
	slice := []*Item{
		{ID: it1.ID, WorkspaceID: a.WorkspaceID},
		{ID: it2.ID, WorkspaceID: a.WorkspaceID},
	}
	_, err = s.tx(ctx, func(t *tx, log *eventLog) error {
		return attachExtras(t, a.WorkspaceID, slice)
	})
	if err != nil {
		t.Fatalf("attachExtras failed: %v", err)
	}

	if len(slice[0].PRs) != 1 || slice[0].CommentCount != 2 || slice[0].MessageCount != 1 {
		t.Errorf("slice[0] extras = PRs:%d Comments:%d Msgs:%d", len(slice[0].PRs), slice[0].CommentCount, slice[0].MessageCount)
	}
	if len(slice[1].PRs) != 1 || slice[1].CommentCount != 1 || slice[1].MessageCount != 1 {
		t.Errorf("slice[1] extras = PRs:%d Comments:%d Msgs:%d", len(slice[1].PRs), slice[1].CommentCount, slice[1].MessageCount)
	}
}
