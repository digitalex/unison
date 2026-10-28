package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/digitalex/unison/internal/push"
	"github.com/digitalex/unison/internal/store"
)

// Push notifications (#54): a post that asks a member to act, or
// @mentions them, alerts every device of their account. Each account gets
// at most one push per post, and never for its own posts.

// pushTimeout bounds delivering one post's notifications.
const pushTimeout = 10 * time.Second

// pushBodyRunes is how much of the post a notification shows.
const pushBodyRunes = 180

// SetPusher enables push notifications through p (nil disables them).
// Call it before serving.
func (s *Server) SetPusher(p push.Sender) { s.pusher = p }

// pushEvents sends the notifications evs call for, in the background.
func (s *Server) pushEvents(evs []store.Event) {
	if s.pusher == nil {
		return
	}
	for _, ev := range evs {
		if ev.Type != "message.created" && ev.Type != "comment.added" {
			continue
		}
		s.pushing.Add(1)
		go func() {
			defer s.pushing.Done()
			ctx, cancel := context.WithTimeout(context.Background(), pushTimeout)
			defer cancel()
			if err := s.pushEvent(ctx, ev); err != nil {
				log.Printf("push for event %d: %v", ev.Seq, err)
			}
		}()
	}
}

// pushPost is what pushEvent needs from a message or comment.
type pushPost struct {
	authorID  int64
	body      string
	actionFor []int64
	n         push.Notification
}

func postFromEvent(ev store.Event) (*pushPost, error) {
	if ev.Type == "comment.added" {
		var ce store.CommentEvent
		if err := json.Unmarshal(ev.Data, &ce); err != nil {
			return nil, err
		}
		c := ce.Comment
		return &pushPost{authorID: c.AuthorID, body: c.Body,
			n: push.Notification{WorkspaceID: c.WorkspaceID, ItemID: c.ItemID, CommentID: c.ID}}, nil
	}
	var m store.Message
	if err := json.Unmarshal(ev.Data, &m); err != nil {
		return nil, err
	}
	p := &pushPost{authorID: m.AuthorID, body: m.Body, n: push.Notification{WorkspaceID: m.WorkspaceID, MessageID: m.ID}}
	if m.ActionRequired {
		p.actionFor = m.ActionFor
	}
	if m.ParentID != nil {
		p.n.ParentID = *m.ParentID
	}
	if m.ItemID != nil {
		p.n.ItemID = *m.ItemID
	}
	return p, nil
}

func (s *Server) pushEvent(ctx context.Context, ev store.Event) error {
	post, err := postFromEvent(ev)
	if err != nil {
		return err
	}
	action, mention, err := s.store.PushRecipients(ctx, ev.WorkspaceID, post.authorID, post.body, post.actionFor)
	if err != nil || len(action)+len(mention) == 0 {
		return err
	}
	author, err := s.store.GetParticipant(ctx, ev.WorkspaceID, post.authorID)
	if err != nil {
		return err
	}
	post.n.Body = truncate(post.body, pushBodyRunes)
	post.n.Title, post.n.Kind = author.Name+" needs you", "action"
	for _, user := range action {
		s.pushTo(ctx, user, post.n)
	}
	post.n.Title, post.n.Kind = author.Name+" mentioned you", "mention"
	if ev.Type == "comment.added" {
		post.n.Kind = "comment_mention"
	}
	for _, user := range mention {
		s.pushTo(ctx, user, post.n)
	}
	return nil
}

// pushTo sends n to every device of userID, forgetting the ones APNs no
// longer accepts. Device tokens are never logged.
func (s *Server) pushTo(ctx context.Context, userID int64, n push.Notification) {
	devices, err := s.store.ListDevices(ctx, userID)
	if err != nil {
		log.Printf("push: listing devices of user %d: %v", userID, err)
		return
	}
	var wg sync.WaitGroup
	wg.Add(len(devices))
	for _, d := range devices {
		go func(d store.Device) {
			defer wg.Done()
			err := s.pusher.Send(ctx, d.APNSToken, n)
			switch {
			case err == nil:
				err = s.store.TouchDevice(ctx, d.ID)
			case errors.Is(err, push.ErrUnregistered):
				err = s.store.ForgetDevice(ctx, d.ID)
				if err == nil {
					log.Printf("push: forgot device %d of user %d (unregistered)", d.ID, userID)
				}
			}
			if err != nil {
				log.Printf("push: device %d of user %d: %v", d.ID, userID, err)
			}
		}(d)
	}
	wg.Wait()
}

// truncate shortens s to at most n runes, ending in an ellipsis when cut.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// waitPushes waits for the notifications in flight. Intended for tests.
func (s *Server) waitPushes() { s.pushing.Wait() }
