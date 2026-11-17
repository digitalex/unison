// Package concern decides which workspace events concern a participant:
// what `unison wait` wakes for and what `unison peek` counts. The CLI and
// the server share it, so the two always agree.
package concern

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/digitalex/unison/internal/store"
)

// Filter is one participant's view of what concerns them.
type Filter struct {
	Me *store.Participant
	// MentionsOnly narrows it to what `wait --mentions` wakes for: @mentions,
	// replies to Me's threads, open requests to act, Me's items, new open
	// items and webhook deliveries.
	MentionsOnly bool
	// ReadUpTo is Me's chat read marker: messages up to it are already read.
	ReadUpTo int64
	// AuthorOf returns the author of a message (0 if unknown). It is asked
	// only about the parents of replies, and only when MentionsOnly.
	AuthorOf func(messageID int64) int64
}

// Concerns reports whether event e concerns Me.
func (f *Filter) Concerns(e store.Event) bool {
	if e.ActorID == f.Me.ID {
		return false
	}
	switch {
	case e.Type == "message.created":
		var m store.Message
		if json.Unmarshal(e.Data, &m) != nil {
			return false
		}
		return f.MessageConcerns(m)
	case e.Type == "comment.added":
		var ce store.CommentEvent
		if json.Unmarshal(e.Data, &ce) != nil || ce.Comment == nil {
			return false
		}
		mine := ce.Item != nil && ce.Item.AssigneeID != nil && *ce.Item.AssigneeID == f.Me.ID
		return !f.MentionsOnly || Mentions(ce.Comment.Body, f.Me.Name) || mine
	case e.Type == "comment.deleted", strings.HasPrefix(e.Type, "pr."):
		return !f.MentionsOnly
	case strings.HasPrefix(e.Type, "backlog."):
		if !f.MentionsOnly {
			return true
		}
		var it store.Item
		if json.Unmarshal(e.Data, &it) != nil {
			return false
		}
		if e.Type == "backlog.added" {
			// An unassigned item is work to pick up (#90); an item added for
			// someone else wakes only that assignee (#146).
			return it.AssigneeID == nil || *it.AssigneeID == f.Me.ID
		}
		return (e.Type == "backlog.assigned" || e.Type == "backlog.started") && it.AssigneeID != nil && *it.AssigneeID == f.Me.ID
	case e.Type == "webhook.received":
		// Webhooks are instructions for agents: they wake even --mentions,
		// except GitHub's setup ping.
		var d store.DeliveryEvent
		return json.Unmarshal(e.Data, &d) == nil && d.Event != "ping"
	case e.Type == "participant.joined", e.Type == "workspace.updated", strings.HasPrefix(e.Type, "repo."),
		strings.HasPrefix(e.Type, "link."), strings.HasPrefix(e.Type, "webhook."), strings.HasPrefix(e.Type, "schedule."):
		return !f.MentionsOnly
	}
	return false
}

// MessageConcerns reports whether chat message m is unread and concerns Me.
func (f *Filter) MessageConcerns(m store.Message) bool {
	if m.ID <= f.ReadUpTo || m.AuthorID == f.Me.ID {
		return false
	}
	if !f.MentionsOnly {
		return true
	}
	if Mentions(m.Body, f.Me.Name) {
		return true
	}
	if m.ParentID != nil && f.AuthorOf != nil && f.AuthorOf(*m.ParentID) == f.Me.ID {
		return true
	}
	return slices.Contains(m.ActionFor, f.Me.ID) && !slices.Contains(m.DoneBy, f.Me.ID)
}

var mentionRegexCache sync.Map

// Mentions reports whether body @mentions name, or everyone (@all,
// @everyone).
func Mentions(body, name string) bool {
	v, ok := mentionRegexCache.Load(name)
	if !ok {
		v, _ = mentionRegexCache.LoadOrStore(name, regexp.MustCompile(`(?i)@(`+regexp.QuoteMeta(name)+`|all|everyone)\b`))
	}
	return v.(*regexp.Regexp).MatchString(body)
}
