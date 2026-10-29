package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitalex/unison/internal/store/storetest"
)

func newStore(t testing.TB) (*Store, *time.Time) {
	t.Helper()
	s, err := Open(storetest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return now })
	return s, &now
}

func setup(t *testing.T) (*Store, *time.Time, *Participant, *Participant) {
	t.Helper()
	s, now := newStore(t)
	ctx := context.Background()
	ws, _, err := s.CreateWorkspace(ctx, "Unison", "https://github.com/digitalex/unison", "")
	if err != nil {
		t.Fatal(err)
	}
	a, _, _, err := s.Join(ctx, ws.ID, "Alex", "🧑")
	if err != nil {
		t.Fatal(err)
	}
	b, _, _, err := s.Join(ctx, ws.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return s, now, a, b
}

func titles(t *testing.T, s *Store, ws string) []string {
	t.Helper()
	items, _, err := s.ListBacklog(context.Background(), ws, "")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for i, it := range items {
		if it.Position != int64(i+1) {
			t.Errorf("item %q has position %d, want %d", it.Title, it.Position, i+1)
		}
		out = append(out, it.Title)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestJoinAutoAssignsAndRejectsDuplicateNames(t *testing.T) {
	s, _, a, b := setup(t)
	if b.Name == "" || b.Emoji == "" {
		t.Errorf("auto-assigned participant = %+v, want name and emoji", b)
	}
	if _, _, _, err := s.Join(context.Background(), a.WorkspaceID, "alex", ""); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate join err = %v, want ErrConflict", err)
	}
	if _, _, _, err := s.Join(context.Background(), a.WorkspaceID, "has space", ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("join with space err = %v, want ErrInvalid", err)
	}
}

func TestAuthenticate(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	ws, _, _ := s.CreateWorkspace(ctx, "W", "", "")
	p, token, _, err := s.Join(ctx, ws.ID, "Ada", "🦊")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Authenticate(ctx, token)
	if err != nil || got.ID != p.ID {
		t.Errorf("Authenticate = %v, %v; want participant %d", got, err, p.ID)
	}
	if _, err := s.Authenticate(ctx, "bogus"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Authenticate(bogus) err = %v, want ErrNotFound", err)
	}
}

func TestBacklogOrdering(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()
	one, _, _ := s.AddItem(ctx, a, "one", "", 0)
	s.AddItem(ctx, a, "two", "", 0)
	s.AddItem(ctx, a, "zero", "", 1)
	if got, want := titles(t, s, a.WorkspaceID), []string{"zero", "one", "two"}; !equal(got, want) {
		t.Fatalf("after add: %v, want %v", got, want)
	}
	if _, _, err := s.MoveItem(ctx, a, one.ID, 99); err != nil {
		t.Fatal(err)
	}
	if got, want := titles(t, s, a.WorkspaceID), []string{"zero", "two", "one"}; !equal(got, want) {
		t.Fatalf("after move: %v, want %v", got, want)
	}
	if _, _, err := s.CloseItem(ctx, a, one.ID, "done"); err != nil {
		t.Fatal(err)
	}
	if got, want := titles(t, s, a.WorkspaceID), []string{"zero", "two"}; !equal(got, want) {
		t.Fatalf("after close: %v, want %v", got, want)
	}
	if _, _, err := s.ReopenItem(ctx, a, one.ID); err != nil {
		t.Fatal(err)
	}
	if got, want := titles(t, s, a.WorkspaceID), []string{"zero", "two", "one"}; !equal(got, want) {
		t.Fatalf("after reopen: %v, want %v", got, want)
	}
}

func TestAssignIsExclusiveAndRenewable(t *testing.T) {
	s, now, a, b := setup(t)
	ctx := context.Background()
	it, _, _ := s.AddItem(ctx, a, "task", "", 0)
	got, _, err := s.AssignItem(ctx, b, it.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusAssigned || *got.AssigneeID != b.ID {
		t.Fatalf("assigned item = %+v", got)
	}
	if _, _, err := s.AssignItem(ctx, a, it.ID, a.ID); !errors.Is(err, ErrConflict) {
		t.Errorf("second assign err = %v, want ErrConflict", err)
	}
	*now = now.Add(time.Hour)
	renewed, evs, err := s.AssignItem(ctx, b, it.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Type != "backlog.renewed" {
		t.Errorf("renewal events = %+v, want one backlog.renewed", evs)
	}
	if *renewed.LeaseExpiresAt <= *got.LeaseExpiresAt {
		t.Errorf("renewed lease %d not after original %d", *renewed.LeaseExpiresAt, *got.LeaseExpiresAt)
	}
}

func TestLeaseExpires(t *testing.T) {
	s, now, a, b := setup(t)
	ctx := context.Background()
	it, _, _ := s.AddItem(ctx, a, "task", "", 0)
	if _, _, err := s.AssignItem(ctx, b, it.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(LeaseDuration + time.Second)
	evs, err := s.ExpireLeases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Type != "backlog.lease_expired" {
		t.Fatalf("ExpireLeases events = %+v", evs)
	}
	got, _, _ := s.GetItem(ctx, a.WorkspaceID, it.ID)
	if got.Status != StatusOpen || got.AssigneeID != nil {
		t.Errorf("after expiry item = %+v, want open and unassigned", got)
	}
	if _, _, err := s.AssignItem(ctx, a, it.ID, a.ID); err != nil {
		t.Errorf("assign after expiry: %v", err)
	}
}

func TestLeaseExpiryIsLazy(t *testing.T) {
	s, now, a, b := setup(t)
	ctx := context.Background()
	it, _, _ := s.AddItem(ctx, a, "task", "", 0)
	s.AssignItem(ctx, b, it.ID, b.ID)
	*now = now.Add(LeaseDuration)
	// No sweeper ran, but the claim must still see the lease as expired.
	got, _, err := s.ClaimNext(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != it.ID || *got.AssigneeID != a.ID {
		t.Errorf("ClaimNext = %+v, want item %d assigned to %d", got, it.ID, a.ID)
	}
}

func TestClaimNextConcurrent(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()
	const n = 5
	var agents []*Participant
	for i := 0; i < 2*n; i++ {
		p, _, _, err := s.Join(ctx, a.WorkspaceID, "", "")
		if err != nil {
			t.Fatal(err)
		}
		agents = append(agents, p)
	}
	for i := 0; i < n; i++ {
		s.AddItem(ctx, a, "task", "", 0)
	}
	var mu sync.Mutex
	claimed := map[int64]int64{}
	var misses int
	var wg sync.WaitGroup
	for _, p := range agents {
		wg.Add(1)
		go func(p *Participant) {
			defer wg.Done()
			it, _, err := s.ClaimNext(ctx, p)
			mu.Lock()
			defer mu.Unlock()
			if errors.Is(err, ErrNotFound) {
				misses++
				return
			}
			if err != nil {
				t.Error(err)
				return
			}
			if prev, ok := claimed[it.ID]; ok {
				t.Errorf("item %d claimed by both %d and %d", it.ID, prev, p.ID)
			}
			claimed[it.ID] = p.ID
		}(p)
	}
	wg.Wait()
	if len(claimed) != n || misses != n {
		t.Errorf("claimed %d items with %d misses, want %d and %d", len(claimed), misses, n, n)
	}
}

func TestMessagesThreadOneLevel(t *testing.T) {
	s, _, a, b := setup(t)
	ctx := context.Background()
	root, _, _ := s.PostMessage(ctx, a, "hello @"+b.Name, nil, nil)
	reply, _, _ := s.PostMessage(ctx, b, "hi", &root.ID, nil)
	nested, _, err := s.PostMessage(ctx, a, "nested", &reply.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if *nested.ParentID != root.ID {
		t.Errorf("nested reply parent = %d, want root %d", *nested.ParentID, root.ID)
	}
	msgs, _ := s.ListMessages(ctx, a.WorkspaceID, 0)
	if len(msgs) != 3 || msgs[0].ID != root.ID {
		t.Errorf("ListMessages = %+v", msgs)
	}
	evs, _ := s.EventsAfter(ctx, a.WorkspaceID, 0, 0)
	if len(evs) == 0 || evs[len(evs)-1].Type != "message.created" {
		t.Errorf("last event = %+v, want message.created", evs[len(evs)-1])
	}
}

// TestConcurrentWriters exercises the writer serialization that Postgres
// relies on (SQLite serializes anyway): auto-named joins get distinct names,
// concurrent adds and moves keep positions a dense 1..n, and event seqs are
// unique and increasing in commit order.
func TestConcurrentWriters(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()
	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, 3*n)
	for i := 0; i < n; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			_, _, _, err := s.Join(ctx, a.WorkspaceID, "", "")
			errs <- err
		}()
		go func() {
			defer wg.Done()
			_, _, err := s.AddItem(ctx, a, "task", "", 1)
			errs <- err
		}()
		go func() {
			defer wg.Done()
			items, _, err := s.ListBacklog(ctx, a.WorkspaceID, "")
			if err == nil && len(items) > 0 {
				_, _, err = s.MoveItem(ctx, a, items[len(items)-1].ID, 1)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	people, _ := s.ListParticipants(ctx, a.WorkspaceID)
	names := map[string]bool{}
	for _, p := range people {
		if names[strings.ToLower(p.Name)] {
			t.Errorf("duplicate name %q", p.Name)
		}
		names[strings.ToLower(p.Name)] = true
	}
	items, _, _ := s.ListBacklog(ctx, a.WorkspaceID, "")
	if len(items) != n {
		t.Fatalf("got %d items, want %d", len(items), n)
	}
	for i, it := range items {
		if it.Position != int64(i+1) {
			t.Errorf("item %d at position %d, want %d", it.ID, it.Position, i+1)
		}
	}
	evs, _ := s.EventsAfter(ctx, a.WorkspaceID, 0, 1000)
	for i := 1; i < len(evs); i++ {
		if evs[i].Seq <= evs[i-1].Seq {
			t.Errorf("event seq %d after %d", evs[i].Seq, evs[i-1].Seq)
		}
	}
}

func TestDeleteWorkspace(t *testing.T) {
	s, _, a, b := setup(t)
	ctx := t.Context()
	wsID := a.WorkspaceID

	// Create user accounts and link membership
	u1, err := s.LoginWithProvider(ctx, "github", Profile{Subject: "del-u1", Name: "User 1"})
	if err != nil {
		t.Fatal(err)
	}
	u2, err := s.LoginWithProvider(ctx, "google", Profile{Subject: "del-u2", Name: "User 2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := s.JoinAsUser(ctx, wsID, u1.ID, "User1", "👤"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := s.JoinAsUser(ctx, wsID, u2.ID, "User2", "👥"); err != nil {
		t.Fatal(err)
	}

	// Backlog item, comments, and PR
	it, _, err := s.AddItem(ctx, a, "Fix something", "Details", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddComment(ctx, a, it.ID, "A comment"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.LinkPR(ctx, a, it.ID, "https://github.com/digitalex/unison/pull/1"); err != nil {
		t.Fatal(err)
	}

	// Messages: parent message and threaded reply, plus reaction and action
	m1, _, err := s.Post(ctx, a, NewMessage{Body: "@" + b.Name + " root message", ItemID: ptr(it.ID), ActionRequired: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Post(ctx, b, NewMessage{Body: "Reply message", ParentID: ptr(m1.ID)}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.React(ctx, b, m1.ID, "👍", false); err != nil {
		t.Fatal(err)
	}

	// Webhook and delivery
	wh, _, err := s.AddWebhook(ctx, a, NewWebhook{Name: "CI", Instructions: "deploy instructions"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReceiveDelivery(ctx, wh, IncomingDelivery{Source: "github", Event: "push", Summary: "commit", Body: []byte("{}"), Headers: map[string]string{}}); err != nil {
		t.Fatal(err)
	}

	// Passes and invites
	if _, _, err := s.MintPass(ctx, wsID, u1.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateEmailInvite(ctx, wsID, u1.ID, "invitee@example.com"); err != nil {
		t.Fatal(err)
	}

	// Delete workspace
	if err := s.DeleteWorkspace(ctx, wsID); err != nil {
		t.Fatalf("DeleteWorkspace failed: %v", err)
	}

	// GetWorkspace should return ErrNotFound
	if _, err := s.GetWorkspace(ctx, wsID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetWorkspace after delete = %v, want ErrNotFound", err)
	}

	// Deleting again should return ErrNotFound
	if err := s.DeleteWorkspace(ctx, wsID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second DeleteWorkspace = %v, want ErrNotFound", err)
	}

	// Verify all workspace tables are cleaned up for wsID
	for _, table := range []string{
		"webhook_deliveries", "webhooks", "workspace_repos", "item_comments",
		"item_prs", "reactions", "message_actions", "messages", "items",
		"agent_passes", "workspace_invites", "memberships", "participants",
		"events", "workspaces",
	} {
		col := "workspace_id"
		if table == "workspaces" {
			col = "id"
		}
		var count int
		q := "SELECT count(*) FROM " + table + " WHERE " + col + " = ?"
		if err := s.db.QueryRowContext(ctx, q, wsID).Scan(&count); err != nil {
			t.Errorf("checking %s: %v", table, err)
		} else if count != 0 {
			t.Errorf("table %s has %d rows for deleted workspace, want 0", table, count)
		}
	}

	// Verify global user accounts are still intact
	if _, err := s.GetUser(ctx, u1.ID); err != nil {
		t.Errorf("GetUser(u1) after workspace deletion: %v", err)
	}
	if _, err := s.GetUser(ctx, u2.ID); err != nil {
		t.Errorf("GetUser(u2) after workspace deletion: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }

func TestRenumberBatched(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()

	// 1. Empty ids slice
	_, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		return renumber(t, nil)
	})
	if err != nil {
		t.Fatalf("renumber(nil) = %v", err)
	}

	// 2. Single item
	it1, _, err := s.AddItem(ctx, a, "item 1", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := titles(t, s, a.WorkspaceID), []string{"item 1"}; !equal(got, want) {
		t.Fatalf("single item backlog = %v, want %v", got, want)
	}

	// 3. Large backlog spanning multiple batches (> 500 items, e.g. 600 items)
	const totalItems = 600
	var itemIDs []int64
	itemIDs = append(itemIDs, it1.ID)

	for i := 2; i <= totalItems; i++ {
		it, _, err := s.AddItem(ctx, a, fmt.Sprintf("item %d", i), "", 0)
		if err != nil {
			t.Fatalf("AddItem %d: %v", i, err)
		}
		itemIDs = append(itemIDs, it.ID)
	}

	// Reverse itemIDs array and renumber directly
	for i, j := 0, len(itemIDs)-1; i < j; i, j = i+1, j-1 {
		itemIDs[i], itemIDs[j] = itemIDs[j], itemIDs[i]
	}

	_, err = s.tx(ctx, func(t *tx, log *eventLog) error {
		return renumber(t, itemIDs)
	})
	if err != nil {
		t.Fatalf("renumber %d items: %v", totalItems, err)
	}

	// Verify all 600 items now have positions 1..600 matching reversed order
	items, _, err := s.ListBacklog(ctx, a.WorkspaceID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != totalItems {
		t.Fatalf("ListBacklog returned %d items, want %d", len(items), totalItems)
	}
	for i, item := range items {
		expectedID := itemIDs[i]
		if item.ID != expectedID {
			t.Errorf("item at position %d has ID %d, want %d", i+1, item.ID, expectedID)
		}
		if item.Position != int64(i+1) {
			t.Errorf("item %d has position %d, want %d", item.ID, item.Position, i+1)
		}
	}

	// Test MoveItem, CloseItem, DeleteItem on large backlog
	// Move first item to position 300
	movedID := items[0].ID
	if _, _, err := s.MoveItem(ctx, a, movedID, 300); err != nil {
		t.Fatalf("MoveItem: %v", err)
	}
	itemsAfterMove, _, err := s.ListBacklog(ctx, a.WorkspaceID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(itemsAfterMove) != totalItems {
		t.Fatalf("ListBacklog after move returned %d items, want %d", len(itemsAfterMove), totalItems)
	}
	for i, item := range itemsAfterMove {
		if item.Position != int64(i+1) {
			t.Errorf("after move: item %d has position %d, want %d", item.ID, item.Position, i+1)
		}
	}
	if itemsAfterMove[299].ID != movedID {
		t.Errorf("item not moved to position 300, got ID %d at pos 300", itemsAfterMove[299].ID)
	}

	// Close an item
	closeID := itemsAfterMove[100].ID
	if _, _, err := s.CloseItem(ctx, a, closeID, "done"); err != nil {
		t.Fatalf("CloseItem: %v", err)
	}
	itemsAfterClose, _, err := s.ListBacklog(ctx, a.WorkspaceID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(itemsAfterClose) != totalItems-1 {
		t.Fatalf("ListBacklog after close returned %d items, want %d", len(itemsAfterClose), totalItems-1)
	}
	for i, item := range itemsAfterClose {
		if item.Position != int64(i+1) {
			t.Errorf("after close: item %d has position %d, want %d", item.ID, item.Position, i+1)
		}
	}

	// Delete an item
	deleteID := itemsAfterClose[200].ID
	if _, err := s.DeleteItem(ctx, a, deleteID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}
	itemsAfterDelete, _, err := s.ListBacklog(ctx, a.WorkspaceID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(itemsAfterDelete) != totalItems-2 {
		t.Fatalf("ListBacklog after delete returned %d items, want %d", len(itemsAfterDelete), totalItems-2)
	}
	for i, item := range itemsAfterDelete {
		if item.Position != int64(i+1) {
			t.Errorf("after delete: item %d has position %d, want %d", item.ID, item.Position, i+1)
		}
	}
}

func BenchmarkRenumber(b *testing.B) {
	for _, size := range []int{10, 100, 500, 1000} {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			s, _ := newStore(b)
			ctx := context.Background()
			ws, _, _ := s.CreateWorkspace(ctx, "W", "", "")
			p, _, _, _ := s.Join(ctx, ws.ID, "BenchmarkUser", "🏎️")

			ids := make([]int64, size)
			for i := 0; i < size; i++ {
				it, _, err := s.AddItem(ctx, p, fmt.Sprintf("item %d", i), "", 0)
				if err != nil {
					b.Fatal(err)
				}
				ids[i] = it.ID
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := s.tx(ctx, func(t *tx, log *eventLog) error {
					return renumber(t, ids)
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestRecentActivity(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()
	it, _, err := s.AddItem(ctx, a, "Ship it", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PostMessage(ctx, a, "not activity", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AssignItem(ctx, a, it.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AssignItem(ctx, a, it.ID, a.ID); err != nil { // a renewal isn't activity
		t.Fatal(err)
	}
	if _, _, err := s.CloseItem(ctx, a, it.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReopenItem(ctx, a, it.ID); err != nil {
		t.Fatal(err)
	}
	evs, err := s.RecentActivity(ctx, a.WorkspaceID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, e := range evs {
		types = append(types, e.Type)
	}
	// The two joins from setup, then the item's life; no message or renewal.
	want := []string{"participant.joined", "participant.joined", "backlog.added", "backlog.assigned", "backlog.closed", "backlog.reopened"}
	if !equal(types, want) {
		t.Errorf("activity = %v, want %v", types, want)
	}
	// The limit keeps the newest, still oldest first.
	evs, err = s.RecentActivity(ctx, a.WorkspaceID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[0].Type != "backlog.closed" || evs[1].Type != "backlog.reopened" {
		t.Errorf("limited activity = %+v, want closed then reopened", evs)
	}
}
