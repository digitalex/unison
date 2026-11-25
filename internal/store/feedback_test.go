package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFeedbackAndWorkspaceToggle(t *testing.T) {
	s, _, a, b := setup(t)
	wsID := a.WorkspaceID
	ctx := context.Background()

	// Workspace feedback_enabled defaults to false and toggles cleanly via UpdateWorkspace.
	ws, err := s.GetWorkspace(ctx, wsID)
	if err != nil {
		t.Fatal(err)
	}
	if ws.FeedbackEnabled {
		t.Fatalf("new workspace FeedbackEnabled = true, want false")
	}
	enable := true
	ws, evs, err := s.UpdateWorkspace(ctx, wsID, a.ID, WorkspacePatch{FeedbackEnabled: &enable})
	if err != nil || !ws.FeedbackEnabled || len(evs) != 1 || evs[0].Type != "workspace.updated" {
		t.Fatalf("enable feedback = %+v, %+v, %v", ws, evs, err)
	}
	disable := false
	ws, _, err = s.UpdateWorkspace(ctx, wsID, a.ID, WorkspacePatch{FeedbackEnabled: &disable})
	if err != nil || ws.FeedbackEnabled {
		t.Fatalf("disable feedback = %+v, %v", ws, err)
	}

	// Validation: empty title, unknown kind/severity, cross-workspace image URL.
	if _, _, err := s.CreateFeedback(ctx, a, FeedbackInput{Title: "   "}); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty title err = %v, want ErrInvalid", err)
	}
	if _, _, err := s.CreateFeedback(ctx, a, FeedbackInput{Title: "X", Kind: "complaint"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("invalid kind err = %v, want ErrInvalid", err)
	}
	if _, _, err := s.CreateFeedback(ctx, a, FeedbackInput{Title: "X", Severity: "urgent"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("invalid severity err = %v, want ErrInvalid", err)
	}
	if _, _, err := s.CreateFeedback(ctx, a, FeedbackInput{Title: "X", ImageURL: "/api/workspaces/ws_other/images/1"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("cross-workspace image err = %v, want ErrInvalid", err)
	}

	// Create bug report with full origin URL normalized to /api/workspaces/{ws}/images/{id}.
	imgPath := "/api/workspaces/" + wsID + "/images/7"
	f1, evs, err := s.CreateFeedback(ctx, a, FeedbackInput{
		Kind:     "BUG",
		Severity: "high",
		Title:    "  Modal close overlaps header  ",
		Body:     " Clicking X misses by 4px ",
		ImageURL: "http://localhost:4600" + imgPath,
		PageURL:  "/w/" + wsID + "/backlog",
	})
	if err != nil {
		t.Fatal(err)
	}
	if f1.Kind != FeedbackBug || f1.Severity != SeverityHigh || f1.Title != "Modal close overlaps header" ||
		f1.ImageURL != imgPath || f1.Status != FeedbackOpen {
		t.Fatalf("f1 = %+v", f1)
	}
	if len(evs) != 1 || evs[0].Type != "feedback.created" {
		t.Fatalf("create events = %+v", evs)
	}

	// Create suggestion (defaults severity to medium).
	f2, _, err := s.CreateFeedback(ctx, b, FeedbackInput{
		Kind:  FeedbackSuggestion,
		Title: "Keyboard shortcut for Feedback",
	})
	if err != nil || f2.Severity != SeverityMedium {
		t.Fatalf("f2 = %+v, %v", f2, err)
	}

	// List open (newest first).
	open, err := s.ListFeedback(ctx, wsID, "")
	if err != nil || len(open) != 2 || open[0].ID != f2.ID || open[1].ID != f1.ID {
		t.Fatalf("list open = %+v, %v", open, err)
	}

	// Dismiss & reopen f2.
	dismissed, evs, err := s.DismissFeedback(ctx, a, f2.ID)
	if err != nil || dismissed.Status != FeedbackDismissed || len(evs) != 1 || evs[0].Type != "feedback.updated" {
		t.Fatalf("dismiss = %+v, %+v, %v", dismissed, evs, err)
	}
	if list, err := s.ListFeedback(ctx, wsID, FeedbackOpen); err != nil || len(list) != 1 || list[0].ID != f1.ID {
		t.Fatalf("open after dismiss = %+v, %v", list, err)
	}
	if list, err := s.ListFeedback(ctx, wsID, FeedbackDismissed); err != nil || len(list) != 1 || list[0].ID != f2.ID {
		t.Fatalf("dismissed list = %+v, %v", list, err)
	}
	reopened, _, err := s.ReopenFeedback(ctx, b, f2.ID)
	if err != nil || reopened.Status != FeedbackOpen {
		t.Fatalf("reopen = %+v, %v", reopened, err)
	}

	// Seed an existing backlog item so promoted item's per-workspace number is #2 while feedback ID is 1.
	if _, _, err := s.AddItem(ctx, a, "Existing item", "", 0); err != nil {
		t.Fatal(err)
	}

	// Promote f1 to backlog -> creates #2 with metadata, body, screenshot, and provenance.
	promo, evs, err := s.PromoteFeedback(ctx, b, f1.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if promo.Item.Number != 2 || promo.Feedback.Status != FeedbackPromoted ||
		promo.Feedback.PromotedItemNumber == nil || *promo.Feedback.PromotedItemNumber != 2 {
		t.Fatalf("promote = %+v, item = %+v", promo.Feedback, promo.Item)
	}
	if !strings.Contains(promo.Item.Body, "**Type:** Bug · **Severity:** High") ||
		!strings.Contains(promo.Item.Body, "Clicking X misses by 4px") ||
		!strings.Contains(promo.Item.Body, "![Screenshot]("+imgPath+")") ||
		!strings.Contains(promo.Item.Body, "unison feedback show") {
		t.Errorf("promoted item body = %q", promo.Item.Body)
	}
	if len(evs) != 2 || evs[0].Type != "backlog.added" || evs[1].Type != "feedback.promoted" {
		t.Errorf("promote events = %+v", evs)
	}
	if _, _, err := s.PromoteFeedback(ctx, b, f1.ID, 0); !errors.Is(err, ErrConflict) {
		t.Errorf("promote twice err = %v, want ErrConflict", err)
	}
	if _, _, err := s.DismissFeedback(ctx, b, f1.ID); !errors.Is(err, ErrConflict) {
		t.Errorf("dismiss promoted err = %v, want ErrConflict", err)
	}

	// List all: open first, then promoted.
	all, err := s.ListFeedback(ctx, wsID, "all")
	if err != nil || len(all) != 2 || all[0].ID != f2.ID || all[1].ID != f1.ID {
		t.Fatalf("list all = %+v, %v", all, err)
	}

	// Delete f2; DeleteWorkspace cleans up remaining feedback_reports cleanly.
	if evs, err := s.DeleteFeedback(ctx, a, f2.ID); err != nil || len(evs) != 1 || evs[0].Type != "feedback.deleted" {
		t.Fatalf("delete f2 = %+v, %v", evs, err)
	}
	if err := s.DeleteWorkspace(ctx, wsID, 0); err != nil {
		t.Fatalf("DeleteWorkspace with promoted feedback = %v", err)
	}
}
