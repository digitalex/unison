package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type inboxFixture struct {
	s                 *Store
	ws                *Workspace
	alexID, bobID     int64
	alex, laptop, bob *Participant
	wren, legacy      *Participant
}

// newInboxFixture: Alex (two devices, one account) and Bob are members;
// Wren is Alex's agent; Old is a participant from before accounts.
func newInboxFixture(t *testing.T) *inboxFixture {
	t.Helper()
	s, _ := newStore(t)
	ctx := context.Background()
	alex, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "1", Name: "Alex Example"})
	bob, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "2", Name: "Bob"})
	ws, _, err := s.CreateWorkspaceFor(ctx, alex.ID, "Unison", "", "")
	if err != nil {
		t.Fatal(err)
	}
	f := &inboxFixture{s: s, ws: ws, alexID: alex.ID, bobID: bob.ID}
	f.alex, _, _, _, _ = s.JoinAsUser(ctx, ws.ID, alex.ID, "Alex", "")
	f.laptop, _, _, _ = s.Join(ctx, ws.ID, "AlexLaptop", "")
	s.db.ExecContext(ctx, `UPDATE participants SET user_id = ? WHERE id = ?`, alex.ID, f.laptop.ID)
	f.bob, _, _, _, _ = s.JoinAsUser(ctx, ws.ID, bob.ID, "Bob", "")
	pass, _, _ := s.MintPass(ctx, ws.ID, alex.ID)
	f.wren, _, _, err = s.JoinWithPass(ctx, ws.ID, pass, "Wren", "")
	if err != nil {
		t.Fatal(err)
	}
	f.legacy, _, _, _ = s.Join(ctx, ws.ID, "Old", "")
	return f
}

// TestInboxSend: entries go to accounts; an agent's default is its member;
// names resolve to accounts (any device); agents and unlinked participants
// have no inbox; the event carries no content.
func TestInboxSend(t *testing.T) {
	f := newInboxFixture(t)
	ctx := context.Background()
	it, _, _ := f.s.AddItem(ctx, f.alex, "Landing page", "", 0)
	e, evs, err := f.s.SendInbox(ctx, f.wren, InboxMessage{Body: "PR ready for review", ItemID: &it.ID, URL: "https://github.com/o/r/pull/7"})
	if err != nil {
		t.Fatal(err)
	}
	if e.RecipientUserID != f.alexID || e.SenderName != "Wren" || e.Item == nil || e.Item.Title != "Landing page" || e.URL == "" {
		t.Errorf("entry = %+v", e)
	}
	var data map[string]any
	if len(evs) != 1 || evs[0].Type != "inbox.created" || json.Unmarshal(evs[0].Data, &data) != nil || data["body"] != nil || data["recipient_user_id"] != float64(f.alexID) {
		t.Errorf("event = %+v (it must not carry the text)", evs)
	}
	// By participant name (any of the account's devices, any case, "@").
	for _, to := range []string{"AlexLaptop", "@alex", "Alex Example"} {
		e, _, err := f.s.SendInbox(ctx, f.bob, InboxMessage{To: to, Body: "Can you set up the Cloud SQL instance?"})
		if err != nil || e.RecipientUserID != f.alexID {
			t.Errorf("to %q: %+v %v", to, e, err)
		}
	}
	for to, why := range map[string]string{"": "no default for humans", "Wren": "agent", "Old": "unlinked", "Nobody": "unknown"} {
		if _, _, err := f.s.SendInbox(ctx, f.bob, InboxMessage{To: to, Body: "x"}); !errors.Is(err, ErrInvalid) {
			t.Errorf("to %q (%s): err = %v, want ErrInvalid", to, why, err)
		}
	}
	bad := int64(999)
	for _, m := range []InboxMessage{{To: "Alex", Body: "  "}, {To: "Alex", Body: "x", URL: "javascript:alert(1)"}, {To: "Alex", Body: "x", ItemID: &bad}} {
		if _, _, err := f.s.SendInbox(ctx, f.bob, m); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: err = %v, want ErrInvalid", m, err)
		}
	}
	// A removed member has no inbox here.
	f.s.RemoveMember(ctx, f.ws.ID, f.alexID, f.bobID)
	if _, _, err := f.s.SendInbox(ctx, f.alex, InboxMessage{To: "Bob", Body: "x"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("to a removed member: err = %v", err)
	}
}

// TestInboxListAndDismiss: each member sees only their own entries;
// dismissing and restoring work only on your own.
func TestInboxListAndDismiss(t *testing.T) {
	f := newInboxFixture(t)
	ctx := context.Background()
	first, _, _ := f.s.SendInbox(ctx, f.wren, InboxMessage{Body: "Review PR #7"})
	second, _, _ := f.s.SendInbox(ctx, f.wren, InboxMessage{Body: "Which region?"})
	f.s.SendInbox(ctx, f.alex, InboxMessage{To: "Bob", Body: "For Bob"})

	list, _ := f.s.ListInbox(ctx, f.ws.ID, f.alexID, false)
	if len(list) != 2 || list[0].ID != second.ID || list[1].ID != first.ID {
		t.Fatalf("Alex's inbox = %+v", list)
	}
	if _, _, err := f.s.DismissInbox(ctx, f.ws.ID, f.bobID, first.ID, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("Bob dismissing Alex's entry: err = %v, want ErrNotFound", err)
	}
	e, evs, err := f.s.DismissInbox(ctx, f.ws.ID, f.alexID, first.ID, true)
	if err != nil || e.DismissedAt == 0 || len(evs) != 1 || evs[0].Type != "inbox.dismissed" {
		t.Fatalf("dismiss = %+v %+v %v", e, evs, err)
	}
	if _, evs, _ := f.s.DismissInbox(ctx, f.ws.ID, f.alexID, first.ID, true); len(evs) != 0 {
		t.Errorf("dismissing twice recorded %+v", evs)
	}
	if list, _ := f.s.ListInbox(ctx, f.ws.ID, f.alexID, false); len(list) != 1 || list[0].ID != second.ID {
		t.Errorf("open entries = %+v", list)
	}
	if list, _ := f.s.ListInbox(ctx, f.ws.ID, f.alexID, true); len(list) != 2 {
		t.Errorf("all entries = %+v", list)
	}
	if e, evs, _ := f.s.DismissInbox(ctx, f.ws.ID, f.alexID, first.ID, false); e.DismissedAt != 0 || len(evs) != 1 || evs[0].Type != "inbox.restored" {
		t.Errorf("restore = %+v %+v", e, evs)
	}
	if list, _ := f.s.ListInbox(ctx, f.ws.ID, f.bobID, false); len(list) != 1 || list[0].Body != "For Bob" {
		t.Errorf("Bob's inbox = %+v", list)
	}
	// Other workspaces don't see it.
	if list, _ := f.s.ListInbox(ctx, "ws_other", f.alexID, true); len(list) != 0 {
		t.Errorf("another workspace = %+v", list)
	}
}

// TestInboxItemDeleted: deleting an item keeps entries about it.
func TestInboxItemDeleted(t *testing.T) {
	f := newInboxFixture(t)
	ctx := context.Background()
	it, _, _ := f.s.AddItem(ctx, f.alex, "Doomed", "", 0)
	f.s.SendInbox(ctx, f.wren, InboxMessage{Body: "About it", ItemID: &it.ID})
	if _, err := f.s.DeleteItem(ctx, f.alex, it.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := f.s.ListInbox(ctx, f.ws.ID, f.alexID, false); len(list) != 1 || list[0].ItemID != nil {
		t.Errorf("after delete = %+v", list)
	}
}
