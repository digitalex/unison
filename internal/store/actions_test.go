package store

import (
	"context"
	"errors"
	"slices"
	"testing"
)

type actionFixture struct {
	s                       *Store
	ws                      *Workspace
	alex, laptop, bob, wren *Participant
}

// newActionFixture: Alex (two devices, one account) and Bob are members;
// Wren is Alex's agent.
func newActionFixture(t *testing.T) *actionFixture {
	t.Helper()
	s, _ := newStore(t)
	ctx := context.Background()
	alex, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "1", Name: "Alex Example"})
	bob, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "2", Name: "Bob"})
	ws, _, err := s.CreateWorkspaceFor(ctx, alex.ID, "Unison", "", "")
	if err != nil {
		t.Fatal(err)
	}
	f := &actionFixture{s: s, ws: ws}
	f.alex, _, _, _, _ = s.JoinAsUser(ctx, ws.ID, alex.ID, "Alex", "")
	f.laptop, _, _, _ = s.Join(ctx, ws.ID, "AlexLaptop", "")
	if _, err := s.db.ExecContext(ctx, `UPDATE participants SET user_id = ? WHERE id = ?`, alex.ID, f.laptop.ID); err != nil {
		t.Fatal(err)
	}
	f.laptop.UserID = alex.ID
	f.bob, _, _, _, _ = s.JoinAsUser(ctx, ws.ID, bob.ID, "Bob", "")
	pass, _, _ := s.MintPass(ctx, ws.ID, alex.ID)
	if f.wren, _, _, err = s.JoinWithPass(ctx, ws.ID, pass, "Wren", ""); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *actionFixture) open(t *testing.T, p *Participant) []int64 {
	t.Helper()
	msgs, err := f.s.OpenActions(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	n, err := f.s.CountOpenActions(context.Background(), p)
	if err != nil || n != len(msgs) {
		t.Errorf("CountOpenActions = %d, %v; OpenActions has %d", n, err, len(msgs))
	}
	var ids []int64
	for _, m := range msgs {
		ids = append(ids, m.ID)
	}
	return ids
}

// TestActionRequired: a message that needs action asks the participants it
// mentions (not its author); one that mentions nobody is refused; the
// action shows on the message everywhere it is listed.
func TestActionRequired(t *testing.T) {
	f := newActionFixture(t)
	ctx := context.Background()
	m, evs, err := f.s.Post(ctx, f.wren, NewMessage{Body: "@Alex PR #7 is ready for your review (cc @wren)", ActionRequired: true})
	if err != nil {
		t.Fatal(err)
	}
	if !m.ActionRequired || !slices.Equal(m.ActionFor, []int64{f.alex.ID}) || len(m.DoneBy) != 0 {
		t.Errorf("message = %+v, want an action for Alex only", m)
	}
	if len(evs) != 1 || evs[0].Type != "message.created" {
		t.Errorf("events = %+v", evs)
	}
	if _, _, err := f.s.Post(ctx, f.wren, NewMessage{Body: "Someone please review", ActionRequired: true}); !errors.Is(err, ErrInvalid) {
		t.Errorf("no mention: err = %v, want ErrInvalid", err)
	}
	if _, _, err := f.s.Post(ctx, f.wren, NewMessage{Body: "@Wren note to self", ActionRequired: true}); !errors.Is(err, ErrInvalid) {
		t.Errorf("only the author: err = %v, want ErrInvalid", err)
	}
	plain, _, _ := f.s.PostMessage(ctx, f.bob, "@Alex FYI", nil, nil)
	if plain.ActionRequired {
		t.Errorf("a plain mention asks for action: %+v", plain)
	}
	list, _ := f.s.ListMessages(ctx, f.ws.ID, 10)
	if len(list) != 2 || !list[0].ActionRequired || list[1].ActionRequired {
		t.Errorf("listed = %+v", list)
	}
	got, _ := f.s.GetMessage(ctx, f.ws.ID, m.ID)
	if !slices.Equal(got.ActionFor, []int64{f.alex.ID}) {
		t.Errorf("GetMessage = %+v", got)
	}
}

// TestActionDone: open actions are per account (any of Alex's devices sees
// and clears them); done can be undone; only those asked can mark it.
func TestActionDone(t *testing.T) {
	f := newActionFixture(t)
	ctx := context.Background()
	m, _, _ := f.s.Post(ctx, f.wren, NewMessage{Body: "@Alex please set the APNS secrets", ActionRequired: true})
	both, _, _ := f.s.Post(ctx, f.wren, NewMessage{Body: "@Alex @Bob which name do you prefer?", ActionRequired: true})
	if got := f.open(t, f.laptop); !slices.Equal(got, []int64{both.ID, m.ID}) {
		t.Errorf("Alex's laptop: open = %v, want both, newest first", got)
	}
	if got := f.open(t, f.bob); !slices.Equal(got, []int64{both.ID}) {
		t.Errorf("Bob: open = %v", got)
	}
	if got := f.open(t, f.wren); len(got) != 0 {
		t.Errorf("Wren (the asker): open = %v", got)
	}

	done, evs, err := f.s.SetDone(ctx, f.laptop, m.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(done.DoneBy, []int64{f.alex.ID}) || len(evs) != 1 || evs[0].Type != "message.done" {
		t.Errorf("done = %+v, events %+v", done, evs)
	}
	if got := f.open(t, f.alex); !slices.Equal(got, []int64{both.ID}) {
		t.Errorf("after done on the laptop, Alex: open = %v", got)
	}
	if _, evs, _ := f.s.SetDone(ctx, f.alex, m.ID, true); len(evs) != 0 {
		t.Errorf("done twice: events = %+v, want none", evs)
	}
	if _, _, err := f.s.SetDone(ctx, f.bob, m.ID, true); !errors.Is(err, ErrInvalid) {
		t.Errorf("Bob marking Alex's action: err = %v, want ErrInvalid", err)
	}
	undone, evs, err := f.s.SetDone(ctx, f.alex, m.ID, false)
	if err != nil || len(undone.DoneBy) != 0 || len(evs) != 1 || evs[0].Type != "message.undone" {
		t.Errorf("undo = %+v, %v, events %+v", undone, err, evs)
	}
	if got := f.open(t, f.alex); len(got) != 2 {
		t.Errorf("after undo: open = %v", got)
	}

	// Bob's done doesn't clear Alex's part of a message that asks both.
	f.s.SetDone(ctx, f.bob, both.ID, true)
	if got := f.open(t, f.alex); !slices.Contains(got, both.ID) {
		t.Errorf("Bob's done cleared Alex's action: %v", got)
	}
}

// TestActionDoneByReplyOrReaction: replying in the thread or reacting
// marks a message done for whoever it asked; others replying or reacting
// change nothing.
func TestActionDoneByReplyOrReaction(t *testing.T) {
	f := newActionFixture(t)
	ctx := context.Background()
	m, _, _ := f.s.Post(ctx, f.wren, NewMessage{Body: "@Alex @Bob ship it?", ActionRequired: true})
	f.s.PostMessage(ctx, f.wren, "(bump)", &m.ID, nil)
	if got := f.open(t, f.alex); len(got) != 1 {
		t.Errorf("the asker's reply cleared Alex's action: %v", got)
	}
	reply, _, _ := f.s.PostMessage(ctx, f.laptop, "Yes, go", &m.ID, nil)
	if got := f.open(t, f.alex); len(got) != 0 {
		t.Errorf("after Alex replied: open = %v", got)
	}
	// A reply to a reply joins the root's thread, and clears the root.
	f.s.PostMessage(ctx, f.bob, "+1", &reply.ID, nil)
	if got := f.open(t, f.bob); len(got) != 0 {
		t.Errorf("after Bob replied to a reply: open = %v", got)
	}

	q, _, _ := f.s.Post(ctx, f.wren, NewMessage{Body: "@Bob ok to merge?", ActionRequired: true})
	f.s.React(ctx, f.alex, q.ID, "👍", true)
	if got := f.open(t, f.bob); len(got) != 1 {
		t.Errorf("Alex's reaction cleared Bob's action: %v", got)
	}
	f.s.React(ctx, f.bob, q.ID, "👍", true)
	if got := f.open(t, f.bob); len(got) != 0 {
		t.Errorf("after Bob reacted: open = %v", got)
	}
	// Taking the reaction back doesn't reopen it.
	f.s.React(ctx, f.bob, q.ID, "👍", false)
	if got := f.open(t, f.bob); len(got) != 0 {
		t.Errorf("after Bob took the reaction back: open = %v", got)
	}
}

// TestInboxTableDropped: the inbox's table is gone after opening.
func TestInboxTableDropped(t *testing.T) {
	s, _ := newStore(t)
	var n int
	if err := s.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM inbox`).Scan(&n); err == nil {
		t.Error("the inbox table still exists")
	}
}
