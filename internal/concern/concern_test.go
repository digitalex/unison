package concern

import (
	"encoding/json"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

func TestMentions(t *testing.T) {
	tests := []struct {
		body string
		name string
		want bool
	}{
		{"Hello @Alex", "Alex", true},
		{"Hello @alex", "Alex", true},
		{"Hello @AlexSmith", "Alex", false},
		{"Hello @all", "Alex", true},
		{"Hello @everyone", "Alex", true},
		{"Hello @[user]v1", "[user]v1", true},
		{"Hello @user.name", "user.name", true},
		{"Hello world", "Alex", false},
	}
	for _, tt := range tests {
		if got := Mentions(tt.body, tt.name); got != tt.want {
			t.Errorf("Mentions(%q, %q) = %v, want %v", tt.body, tt.name, got, tt.want)
		}
	}
}

func event(typ string, actor int64, data any) store.Event {
	raw, _ := json.Marshal(data)
	return store.Event{Type: typ, ActorID: actor, Data: raw}
}

// TestFilter: what concerns Ada (participant 2) of what Alex (1) and Ada
// herself do, with and without MentionsOnly.
func TestFilter(t *testing.T) {
	const alex, ada = int64(1), int64(2)
	id := func(n int64) *int64 { return &n }
	authors := map[int64]int64{10: ada, 11: alex}
	tests := []struct {
		name          string
		e             store.Event
		all, mentions bool
	}{
		{"her own message", event("message.created", ada, store.Message{ID: 20, AuthorID: ada, Body: "@Ada note to self"}), false, false},
		{"a message", event("message.created", alex, store.Message{ID: 20, AuthorID: alex, Body: "Morning"}), true, false},
		{"a message she has read", event("message.created", alex, store.Message{ID: 15, AuthorID: alex, Body: "@Ada hi"}), false, false},
		{"a mention", event("message.created", alex, store.Message{ID: 20, AuthorID: alex, Body: "@ada ping"}), true, true},
		{"a message for everyone", event("message.created", alex, store.Message{ID: 20, AuthorID: alex, Body: "@all standup"}), true, true},
		{"a reply in her thread", event("message.created", alex, store.Message{ID: 20, AuthorID: alex, ParentID: id(10), Body: "Sure"}), true, true},
		{"a reply in Alex's thread", event("message.created", alex, store.Message{ID: 20, AuthorID: alex, ParentID: id(11), Body: "Also"}), true, false},
		{"an open request to act", event("message.created", alex, store.Message{ID: 20, AuthorID: alex, Body: "Please review", ActionFor: []int64{ada}}), true, true},
		{"a request she has done", event("message.created", alex, store.Message{ID: 20, AuthorID: alex, Body: "Please review", ActionFor: []int64{ada}, DoneBy: []int64{ada}}), true, false},
		{"a comment", event("comment.added", alex, store.CommentEvent{Comment: &store.Comment{Body: "Hm"}, Item: &store.Item{}}), true, false},
		{"a comment that mentions her", event("comment.added", alex, store.CommentEvent{Comment: &store.Comment{Body: "@Ada?"}, Item: &store.Item{}}), true, true},
		{"a comment on her item", event("comment.added", alex, store.CommentEvent{Comment: &store.Comment{Body: "Hm"}, Item: &store.Item{AssigneeID: id(ada)}}), true, true},
		{"a deleted comment", event("comment.deleted", alex, nil), true, false},
		{"a linked PR", event("pr.linked", alex, nil), true, false},
		{"a new open item", event("backlog.added", alex, store.Item{}), true, true},
		{"a new item for Alex", event("backlog.added", alex, store.Item{AssigneeID: id(alex)}), true, false},
		{"a new item for her", event("backlog.added", alex, store.Item{AssigneeID: id(ada)}), true, true},
		{"an item assigned to her", event("backlog.assigned", alex, store.Item{AssigneeID: id(ada)}), true, true},
		{"an item closed", event("backlog.closed", alex, store.Item{AssigneeID: id(ada)}), true, false},
		{"her own new item", event("backlog.added", ada, store.Item{}), false, false},
		{"a recurring task's run", event("backlog.added", 0, store.Item{}), true, true},
		{"a webhook delivery", event("webhook.received", 0, store.DeliveryEvent{Event: "pull_request"}), true, true},
		{"GitHub's setup ping", event("webhook.received", 0, store.DeliveryEvent{Event: "ping"}), false, false},
		{"someone joining", event("participant.joined", alex, nil), true, false},
		{"a reaction", event("message.reacted", alex, nil), false, false},
		{"a message marked done", event("message.done", alex, nil), false, false},
	}
	for _, tt := range tests {
		for mentionsOnly, want := range map[bool]bool{false: tt.all, true: tt.mentions} {
			f := &Filter{Me: &store.Participant{ID: ada, Name: "Ada"}, MentionsOnly: mentionsOnly, ReadUpTo: 15,
				AuthorOf: func(id int64) int64 { return authors[id] }}
			if got := f.Concerns(tt.e); got != want {
				t.Errorf("%s (mentions only: %v): Concerns = %v, want %v", tt.name, mentionsOnly, got, want)
			}
		}
	}
}
