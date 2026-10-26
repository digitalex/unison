package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// TestReactions: reactions toggle per participant and emoji, aggregate in
// a fixed order on listed messages, and record message.reacted only when
// something changes.
func TestReactions(t *testing.T) {
	s, _, a, b := setup(t)
	ctx := context.Background()
	m, _, _ := s.PostMessage(ctx, a, "Shipped!", nil, nil)
	other, _, _ := s.PostMessage(ctx, b, "Nice", nil, nil)

	got, evs, err := s.React(ctx, b, m.ID, "💯", true)
	if err != nil {
		t.Fatal(err)
	}
	var data Message
	if len(evs) != 1 || evs[0].Type != "message.reacted" || json.Unmarshal(evs[0].Data, &data) != nil || len(data.Reactions) != 1 {
		t.Errorf("events = %+v", evs)
	}
	if len(got.Reactions) != 1 || got.Reactions[0].Emoji != "💯" || got.Reactions[0].By[0] != b.ID {
		t.Errorf("reactions = %+v", got.Reactions)
	}
	if _, evs, _ := s.React(ctx, b, m.ID, "100", true); len(evs) != 0 {
		t.Errorf("reacting twice recorded %+v", evs)
	}
	s.React(ctx, a, m.ID, "+1", true)
	s.React(ctx, b, m.ID, "👍", true)
	s.React(ctx, a, m.ID, "❤", true) // without the variation selector
	s.React(ctx, a, other.ID, "muscle", true)

	msgs, _ := s.ListMessages(ctx, a.WorkspaceID, 0)
	want := []Reaction{{"👍", 2, []int64{a.ID, b.ID}}, {"❤️", 1, []int64{a.ID}}, {"💯", 1, []int64{b.ID}}}
	if !equalReactions(msgs[0].Reactions, want) {
		t.Errorf("message reactions = %+v, want %+v", msgs[0].Reactions, want)
	}
	if !equalReactions(msgs[1].Reactions, []Reaction{{"💪", 1, []int64{a.ID}}}) {
		t.Errorf("other reactions = %+v", msgs[1].Reactions)
	}

	got, evs, _ = s.React(ctx, b, m.ID, "👍", false)
	if len(evs) != 1 || got.Reactions[0].Count != 1 {
		t.Errorf("taking back: %+v %+v", got.Reactions, evs)
	}
	if _, evs, _ := s.React(ctx, b, m.ID, "👍", false); len(evs) != 0 {
		t.Errorf("taking back twice recorded %+v", evs)
	}
}

func equalReactions(got, want []Reaction) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i].Emoji != want[i].Emoji || got[i].Count != want[i].Count || len(got[i].By) != len(want[i].By) {
			return false
		}
		for j := range got[i].By {
			if got[i].By[j] != want[i].By[j] {
				return false
			}
		}
	}
	return true
}

// TestReactionValidation: only the fixed emoji, only on messages of the
// actor's workspace.
func TestReactionValidation(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()
	m, _, _ := s.PostMessage(ctx, a, "hi", nil, nil)
	for _, e := range []string{"🎉", "", "thumbs", "👍👍"} {
		if _, _, err := s.React(ctx, a, m.ID, e, true); !errors.Is(err, ErrInvalid) {
			t.Errorf("emoji %q: err = %v, want ErrInvalid", e, err)
		}
	}
	ws2, _, _ := s.CreateWorkspace(ctx, "Other", "", "")
	c, _, _, _ := s.Join(ctx, ws2.ID, "Carol", "")
	if _, _, err := s.React(ctx, c, m.ID, "👍", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("reacting across workspaces: err = %v, want ErrNotFound", err)
	}
	if _, _, err := s.React(ctx, a, 999, "👍", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing message: err = %v", err)
	}
}
