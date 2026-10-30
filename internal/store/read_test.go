package store

import (
	"context"
	"errors"
	"testing"
)

func TestMarkRead(t *testing.T) {
	s, _, a, b := setup(t)
	ctx := context.Background()
	post := func(body string, action bool) int64 {
		t.Helper()
		m, _, err := s.Post(ctx, b, NewMessage{Body: body, ActionRequired: action})
		if err != nil {
			t.Fatal(err)
		}
		return m.ID
	}
	plain := post("morning", false)
	asked := post("@Alex please review", true)
	last := post("thanks", false)

	if got, _ := s.ReadUpTo(ctx, a); got != 0 {
		t.Errorf("new participant read up to %d, want 0", got)
	}
	if got, notified, err := s.MarkRead(ctx, a, plain); err != nil || got != plain || notified {
		t.Errorf("MarkRead(plain) = %d, %v, %v; want %d, not notified", got, notified, err, plain)
	}
	if got, notified, err := s.MarkRead(ctx, a, last); err != nil || got != last || !notified {
		t.Errorf("MarkRead past an action for Alex = %d, %v, %v; want %d, notified", got, notified, err, last)
	}
	if got, notified, _ := s.MarkRead(ctx, a, asked); got != last || notified {
		t.Errorf("MarkRead backwards = %d, %v; want the marker to stay at %d", got, notified, last)
	}
	if got, _ := s.ReadUpTo(ctx, a); got != last {
		t.Errorf("ReadUpTo = %d, want %d", got, last)
	}
	// Your own posts never notified you.
	if _, notified, _ := s.MarkRead(ctx, b, last); notified {
		t.Error("reading your own posts counted as notified")
	}
	if _, _, err := s.MarkRead(ctx, a, last+100); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown message: %v, want ErrNotFound", err)
	}
	other, _, _ := s.CreateWorkspace(ctx, "Other", "", "")
	c, _, _, _ := s.Join(ctx, other.ID, "Cy", "")
	if _, _, err := s.MarkRead(ctx, c, last); !errors.Is(err, ErrNotFound) {
		t.Errorf("another workspace's message: %v, want ErrNotFound", err)
	}
}
