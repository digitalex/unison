package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestIdeas (#917): add, list, edit and discuss ideas, with events;
// validation; only the author deletes a comment; ideas go with their
// workspace.
func TestIdeas(t *testing.T) {
	s, _, a, b := setup(t)
	ctx := context.Background()
	if _, _, err := s.AddIdea(ctx, a, "  ", "body"); !errors.Is(err, ErrInvalid) {
		t.Errorf("AddIdea without a title err = %v, want ErrInvalid", err)
	}
	d, evs, err := s.AddIdea(ctx, a, "  Dark mode  ", " Maybe **later** ")
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Dark mode" || d.Body != "Maybe **later**" || d.Status != IdeaOpen || d.CreatedBy != a.ID || d.PromotedItemID != nil {
		t.Errorf("added = %+v", d)
	}
	if len(evs) != 1 || evs[0].Type != "idea.added" || evs[0].ActorID != a.ID {
		t.Errorf("add events = %+v", evs)
	}
	second, _, err := s.AddIdea(ctx, b, "Offline sync", "")
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.ListIdeas(ctx, a.WorkspaceID, "")
	if err != nil || len(list) != 2 || list[0].ID != second.ID || list[1].ID != d.ID {
		t.Fatalf("list = %+v, %v (want newest first)", list, err)
	}
	if _, err := s.ListIdeas(ctx, a.WorkspaceID, "bogus"); !errors.Is(err, ErrInvalid) {
		t.Errorf("list bogus status err = %v, want ErrInvalid", err)
	}
	// Ideas don't show up in the backlog.
	if items, _, _ := s.ListBacklog(ctx, a.WorkspaceID, "all"); len(items) != 0 {
		t.Errorf("backlog = %+v, want empty", items)
	}

	title := "Dark theme"
	got, evs, err := s.UpdateIdea(ctx, b, d.ID, IdeaPatch{Title: &title})
	if err != nil || got.Title != title || got.Body != d.Body || len(evs) != 1 || evs[0].Type != "idea.updated" {
		t.Fatalf("edit = %+v, %+v, %v", got, evs, err)
	}
	empty := " "
	if _, _, err := s.UpdateIdea(ctx, b, d.ID, IdeaPatch{Title: &empty}); !errors.Is(err, ErrInvalid) {
		t.Errorf("edit to empty title err = %v, want ErrInvalid", err)
	}

	if _, _, err := s.AddIdeaComment(ctx, b, d.ID, " "); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty comment err = %v, want ErrInvalid", err)
	}
	c, evs, err := s.AddIdeaComment(ctx, b, d.ID, "Yes please, @Alex")
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Type != "idea.commented" {
		t.Fatalf("comment events = %+v", evs)
	}
	var ce IdeaCommentEvent
	if err := json.Unmarshal(evs[0].Data, &ce); err != nil || ce.Comment.ID != c.ID || ce.Idea.ID != d.ID || ce.Idea.CommentCount != 1 {
		t.Errorf("comment event data = %+v, %v", ce, err)
	}
	if _, err := s.DeleteIdeaComment(ctx, a, d.ID, c.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("deleting another's comment err = %v, want ErrForbidden", err)
	}
	if evs, err := s.DeleteIdeaComment(ctx, b, d.ID, c.ID); err != nil || len(evs) != 1 || evs[0].Type != "idea.comment_deleted" {
		t.Errorf("delete comment = %+v, %v", evs, err)
	}
	if comments, err := s.ListIdeaComments(ctx, a.WorkspaceID, d.ID); err != nil || len(comments) != 0 {
		t.Errorf("comments after delete = %+v, %v", comments, err)
	}
	s.AddIdeaComment(ctx, a, second.ID, "Needs a design first")

	// Another workspace's participant can't see or touch them.
	other, _, _ := s.CreateWorkspace(ctx, "Other", "", "")
	stranger, _, _, err := s.Join(ctx, other.ID, "Eve", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddIdeaComment(ctx, stranger, d.ID, "hi"); !errors.Is(err, ErrNotFound) {
		t.Errorf("comment from another workspace err = %v, want ErrNotFound", err)
	}
	if _, _, err := s.PromoteIdea(ctx, stranger, d.ID, 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("promote from another workspace err = %v, want ErrNotFound", err)
	}

	evs, err = s.DeleteIdea(ctx, a, d.ID)
	if err != nil || len(evs) != 1 || evs[0].Type != "idea.deleted" {
		t.Fatalf("delete = %+v, %v", evs, err)
	}
	if _, err := s.GetIdea(ctx, a.WorkspaceID, d.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("get deleted err = %v, want ErrNotFound", err)
	}

	if err := s.DeleteWorkspace(ctx, a.WorkspaceID, 0); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM ideas) + (SELECT COUNT(*) FROM idea_comments)`).Scan(&n)
	if n != 0 {
		t.Errorf("%d ideas and comments left after deleting the workspace", n)
	}
}

// TestPromoteIdea (#917): "Move to backlog" adds an item carrying the
// idea's title, body and discussion, marks the idea promoted to it, and
// records both events in one transaction; promoting twice is a conflict.
func TestPromoteIdea(t *testing.T) {
	s, _, a, b := setup(t)
	ctx := context.Background()
	s.AddItem(ctx, a, "Existing", "", 0)
	d, _, err := s.AddIdea(ctx, a, "Dark mode", "Users keep asking for it.")
	if err != nil {
		t.Fatal(err)
	}
	s.AddIdeaComment(ctx, b, d.ID, "Start with the sidebar")
	s.AddIdeaComment(ctx, a, d.ID, "Agreed.\nThen the chat.")

	out, evs, err := s.PromoteIdea(ctx, b, d.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	it := out.Item
	if it.Title != "Dark mode" || it.Position != 1 || it.Status != StatusOpen || it.CreatedBy != b.ID {
		t.Errorf("item = %+v", it)
	}
	for _, want := range []string{"Users keep asking for it.", "idea 1", "unison idea show 1", "Start with the sidebar", "**Alex:** Agreed.\n> Then the chat."} {
		if !strings.Contains(it.Body, want) {
			t.Errorf("item body lacks %q:\n%s", want, it.Body)
		}
	}
	if out.Idea.Status != IdeaPromoted || out.Idea.PromotedItemID == nil || *out.Idea.PromotedItemID != it.ID || *out.Idea.PromotedBy != b.ID || out.Idea.PromotedAt == nil {
		t.Errorf("idea = %+v", out.Idea)
	}
	var types []string
	for _, e := range evs {
		types = append(types, e.Type)
	}
	if strings.Join(types, ",") != "backlog.added,idea.promoted" {
		t.Errorf("events = %v", types)
	}
	if titles := titles(t, s, a.WorkspaceID); strings.Join(titles, ",") != "Dark mode,Existing" {
		t.Errorf("backlog = %v, want the promoted idea on top", titles)
	}
	if _, _, err := s.PromoteIdea(ctx, a, d.ID, 0); !errors.Is(err, ErrConflict) {
		t.Errorf("second promote err = %v, want ErrConflict", err)
	}
	if open, _ := s.ListIdeas(ctx, a.WorkspaceID, ""); len(open) != 0 {
		t.Errorf("open ideas = %+v, want none", open)
	}
	if promoted, _ := s.ListIdeas(ctx, a.WorkspaceID, IdeaPromoted); len(promoted) != 1 || promoted[0].CommentCount != 2 {
		t.Errorf("promoted ideas = %+v", promoted)
	}
	// The item outlives the idea, and the idea its item.
	if _, err := s.DeleteItem(ctx, a, it.ID); err != nil {
		t.Fatalf("deleting the promoted item: %v", err)
	}
	if got, err := s.GetIdea(ctx, a.WorkspaceID, d.ID); err != nil || got.Status != IdeaPromoted {
		t.Errorf("idea after its item was deleted = %+v, %v", got, err)
	}

	// An idea without body or discussion still says where it came from.
	bare, _, _ := s.AddIdea(ctx, a, "Bare", "")
	out, _, err = s.PromoteIdea(ctx, a, bare.ID, 0)
	if err != nil || !strings.HasPrefix(out.Item.Body, "_Moved here from idea") || strings.Contains(out.Item.Body, "Discussion") {
		t.Errorf("bare promote = %+v, %v", out, err)
	}
}
