package server

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/digitalex/unison/internal/store"
)

// TestPeek: GET …/events/peek counts what `unison wait` would wake the
// caller for (unread chat, plus the other events after the cursor) and
// changes nothing but the agent's logged-in clocks.
func TestPeek(t *testing.T) {
	ts, st := newServer(t)
	ctx := t.Context()
	// An hour behind the wall clock, so touch's few-seconds throttle (which
	// reads the wall clock) never hides a write to last_seen_at.
	now := time.Now().Add(-time.Hour)
	st.SetClock(func() time.Time { return now })
	ws, _, _ := st.CreateWorkspace(ctx, "W", "", "")
	alex, alexTok, _, _ := joinMember(t, st, ws.ID, "Alex", "")
	pass, _, _ := st.MintPass(ctx, ws.ID, alex.UserID)
	ada, adaTok, _, err := st.JoinWithPass(ctx, ws.ID, pass, "Ada", "")
	if err != nil {
		t.Fatal(err)
	}
	joined := now.UnixMilli()
	api := ts.URL + "/api/workspaces/" + ws.ID
	peek := func(tok, query string, pending bool, count int) {
		t.Helper()
		latest, _ := st.LatestSeq(ctx, ws.ID)
		status, out := call(t, "GET", api+"/events/peek"+query, tok, "")
		if status != http.StatusOK || out["pending"] != pending || out["count"] != float64(count) || out["latest_seq"] != float64(latest) {
			t.Errorf("peek%s = %d %v, want pending %v, count %d, latest_seq %d", query, status, out, pending, count, latest)
		}
	}
	after := func() string {
		seq, _ := st.LatestSeq(ctx, ws.ID)
		return fmt.Sprintf("?after=%d", seq)
	}

	// Nothing yet: joining isn't news to the one who joined last.
	peek(adaTok, "", false, 0)
	cursor := after()
	peek(adaTok, cursor, false, 0)

	// Chat: unread messages count; --mentions only those that concern Ada.
	// Her own never do.
	first, _, _ := st.PostMessage(ctx, alex, "Morning", nil, nil)
	st.PostMessage(ctx, ada, "Morning!", nil, nil)
	peek(adaTok, cursor, true, 1)
	peek(adaTok, cursor+"&mentions=1", false, 0)
	asked, _, err := st.Post(ctx, alex, store.NewMessage{Body: "@Ada can you review?", ActionRequired: true})
	if err != nil {
		t.Fatal(err)
	}
	peek(adaTok, cursor, true, 2)
	peek(adaTok, cursor+"&mentions=1", true, 1)
	// Unread chat counts whichever side of the cursor it is on, as it wakes
	// `wait`; without a cursor, it is all that counts.
	peek(adaTok, after(), true, 2)
	peek(adaTok, "", true, 2)

	// Other events count after the cursor only.
	later := after()
	st.AddItem(ctx, alex, "Open item", "", 0)
	st.AddItemAssigned(ctx, alex, "Alex's item", "", 0, alex.ID)
	st.AddItem(ctx, ada, "Ada's own item", "", 0)
	peek(adaTok, later, true, 4)
	peek(adaTok, later+"&mentions=1", true, 2)
	peek(adaTok, cursor+"&mentions=1", true, 2)
	peek(adaTok, "", true, 2)
	// Alex sees Ada's message and item, not his own.
	peek(alexTok, cursor, true, 2)

	// None of that changed anything: Ada has read nothing, the request is
	// still open, and she was last seen when she joined.
	now = now.Add(30 * time.Minute)
	peek(adaTok, cursor, true, 4)
	peek(alexTok, cursor, true, 2)
	if upTo, _ := st.ReadUpTo(ctx, ada); upTo != 0 {
		t.Errorf("read marker after peeks = %d, want 0", upTo)
	}
	if open, _ := st.OpenActions(ctx, ada); len(open) != 1 || open[0].ID != asked.ID {
		t.Errorf("open actions after peeks = %+v", open)
	}
	for _, p := range []*store.Participant{ada, alex} {
		if got, _ := st.GetParticipant(ctx, ws.ID, p.ID); got.LastSeenAt != joined {
			t.Errorf("%s's last_seen_at after peeks = %d, want it unchanged (%d)", p.Name, got.LastSeenAt, joined)
		}
	}
	// Any other request does show her as here.
	if status, _ := getAs(t, api+"/backlog", adaTok); status != http.StatusOK {
		t.Fatalf("backlog = %d", status)
	}
	if got, _ := st.GetParticipant(ctx, ws.ID, ada.ID); got.LastSeenAt != now.UnixMilli() {
		t.Errorf("last_seen_at after a backlog request = %d, want %d", got.LastSeenAt, now.UnixMilli())
	}
	seen := now.UnixMilli()

	// Reading catches her up: only the events after the cursor are left.
	if _, _, err := st.MarkRead(ctx, ada, asked.ID); err != nil {
		t.Fatal(err)
	}
	peek(adaTok, later, true, 2)
	peek(adaTok, "", false, 0)
	if _, _, err := st.MarkRead(ctx, alex, first.ID); err != nil {
		t.Fatal(err)
	}
	peek(alexTok, "", true, 1)

	// Peeking alone keeps an agent logged in, and its credential alive,
	// without ever showing it as here.
	for range 30 {
		now = now.Add(time.Hour)
		if status, body := getAs(t, api+"/events/peek", adaTok); status != http.StatusOK {
			t.Fatalf("peek after %s = %d %s", now, status, body)
		}
	}
	got, _ := st.GetParticipant(ctx, ws.ID, ada.ID)
	if got.LastSeenAt != seen || got.RevokedAt != 0 || got.TokenExpiresAt != now.Add(store.AgentCredentialTTL).UnixMilli() {
		t.Errorf("agent after 30 hours of peeks = %+v (last seen %d)", got, seen)
	}

	// The count stops at 100.
	for i := range 105 {
		st.PostMessage(ctx, alex, fmt.Sprintf("message %d", i), nil, nil)
	}
	peek(adaTok, "", true, 100)

	if status, out := call(t, "GET", api+"/events/peek?after=soon", adaTok, ""); status != http.StatusBadRequest {
		t.Errorf("peek?after=soon = %d %v", status, out)
	}
	if status, out := call(t, "GET", api+"/events/peek", "", ""); status != http.StatusUnauthorized {
		t.Errorf("anonymous peek = %d %v", status, out)
	}
}

// TestPeekReplies: with mentions=1, a reply counts only in the caller's own
// threads, even when the thread started before the messages peek reads.
func TestPeekReplies(t *testing.T) {
	ts, st := newServer(t)
	ctx := t.Context()
	ws, _, _ := st.CreateWorkspace(ctx, "W", "", "")
	alex, _, _, _ := joinMember(t, st, ws.ID, "Alex", "")
	ada, adaTok, _, _ := joinMember(t, st, ws.ID, "Ada", "")
	mine, _, _ := st.PostMessage(ctx, ada, "Shipping today", nil, nil)
	theirs, _, _ := st.PostMessage(ctx, alex, "Lunch?", nil, nil)
	if _, _, err := st.MarkRead(ctx, ada, theirs.ID); err != nil {
		t.Fatal(err)
	}
	st.PostMessage(ctx, alex, "Yes please", &theirs.ID, nil)
	url := ts.URL + "/api/workspaces/" + ws.ID + "/events/peek?mentions=1"
	if status, out := call(t, "GET", url, adaTok, ""); status != http.StatusOK || out["count"] != float64(0) {
		t.Errorf("peek after a reply in Alex's thread = %d %v, want count 0", status, out)
	}
	st.PostMessage(ctx, alex, "Nice", &mine.ID, nil)
	if status, out := call(t, "GET", url, adaTok, ""); status != http.StatusOK || out["count"] != float64(1) {
		t.Errorf("peek after a reply in Ada's thread = %d %v, want count 1", status, out)
	}
}
