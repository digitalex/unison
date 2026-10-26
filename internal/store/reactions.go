package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Reactions are emoji that participants put on chat messages, from a fixed
// set. Each participant puts each emoji on a message at most once.

// ReactionEmoji are the allowed reactions, in display order.
var ReactionEmoji = []string{"👍", "👎", "❤️", "💪", "💯"}

// reactionNames maps names (for CLIs and URLs) and variants to an emoji.
var reactionNames = map[string]string{
	"+1": "👍", "thumbsup": "👍", "thumbs_up": "👍", "up": "👍",
	"-1": "👎", "thumbsdown": "👎", "thumbs_down": "👎", "down": "👎",
	"heart": "❤️", "❤": "❤️", "love": "❤️",
	"muscle": "💪", "strong": "💪",
	"100": "💯", "hundred": "💯",
}

// Reaction is one emoji on a message: how many put it there, and who
// (participant IDs, in the order they reacted).
type Reaction struct {
	Emoji string  `json:"emoji"`
	Count int     `json:"count"`
	By    []int64 `json:"by"`
}

// NormalizeReaction returns the allowed emoji for s (an emoji or a name
// such as "+1", "heart" or "100"), or ErrInvalid.
func NormalizeReaction(s string) (string, error) {
	s = strings.TrimSpace(s)
	for _, e := range ReactionEmoji {
		if s == e {
			return e, nil
		}
	}
	if e, ok := reactionNames[strings.ToLower(s)]; ok {
		return e, nil
	}
	return "", fmt.Errorf("%w: reactions are %s (or +1, -1, heart, muscle, 100)", ErrInvalid, strings.Join(ReactionEmoji, " "))
}

// React puts (on) or takes back (off) the actor's emoji reaction on a
// message of its workspace, and returns the message with its reactions.
// Repeating either is a no-op without an event; a change records
// message.reacted with the message.
func (s *Store) React(ctx context.Context, actor *Participant, messageID int64, emoji string, on bool) (*Message, []Event, error) {
	emoji, err := NormalizeReaction(emoji)
	if err != nil {
		return nil, nil, err
	}
	var m *Message
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var err error
		if m, err = scanMessage(t.QueryRow(selectMessages+` WHERE m.id = ? AND m.workspace_id = ?`, messageID, actor.WorkspaceID)); err != nil {
			return fmt.Errorf("message %d: %w", messageID, err)
		}
		var res sql.Result
		if on {
			res, err = t.Exec(`INSERT INTO reactions (workspace_id, message_id, participant_id, emoji, created_at) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT (message_id, participant_id, emoji) DO NOTHING`, actor.WorkspaceID, messageID, actor.ID, emoji, log.now)
		} else {
			res, err = t.Exec(`DELETE FROM reactions WHERE message_id = ? AND participant_id = ? AND emoji = ?`, messageID, actor.ID, emoji)
		}
		if err != nil {
			return err
		}
		msgs := []Message{*m}
		if err := attachReactions(t.Query, actor.WorkspaceID, msgs); err != nil {
			return err
		}
		m = &msgs[0]
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		return log.add(actor.WorkspaceID, "message.reacted", actor.ID, m)
	})
	if err != nil {
		return nil, nil, err
	}
	return m, evs, nil
}

// GetMessage returns one message of a workspace, with its reactions.
func (s *Store) GetMessage(ctx context.Context, wsID string, id int64) (*Message, error) {
	msgs, err := s.withReactions(ctx, wsID)(scanMessages(s.db.QueryContext(ctx, selectMessages+` WHERE m.id = ? AND m.workspace_id = ?`, id, wsID)))
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		return nil, ErrNotFound
	}
	return &msgs[0], nil
}

type queryFunc func(q string, args ...any) (*sql.Rows, error)

// attachReactions fills in the reactions of msgs, all from wsID.
func attachReactions(query queryFunc, wsID string, msgs []Message) error {
	if len(msgs) == 0 {
		return nil
	}
	byID := make(map[int64]*Message, len(msgs))
	lo, hi := msgs[0].ID, msgs[0].ID
	for i := range msgs {
		byID[msgs[i].ID] = &msgs[i]
		lo, hi = min(lo, msgs[i].ID), max(hi, msgs[i].ID)
	}
	rows, err := query(`SELECT message_id, emoji, participant_id FROM reactions
		WHERE workspace_id = ? AND message_id BETWEEN ? AND ? ORDER BY created_at, id`, wsID, lo, hi)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, who int64
		var emoji string
		if err := rows.Scan(&id, &emoji, &who); err != nil {
			return err
		}
		if m := byID[id]; m != nil {
			m.addReaction(emoji, who)
		}
	}
	return rows.Err()
}

// addReaction counts who's emoji, keeping Reactions in ReactionEmoji order.
func (m *Message) addReaction(emoji string, who int64) {
	for i := range m.Reactions {
		if m.Reactions[i].Emoji == emoji {
			m.Reactions[i].Count++
			m.Reactions[i].By = append(m.Reactions[i].By, who)
			return
		}
	}
	m.Reactions = append(m.Reactions, Reaction{Emoji: emoji, Count: 1, By: []int64{who}})
	rank := func(e string) int {
		for i, x := range ReactionEmoji {
			if x == e {
				return i
			}
		}
		return len(ReactionEmoji)
	}
	for i := len(m.Reactions) - 1; i > 0 && rank(m.Reactions[i].Emoji) < rank(m.Reactions[i-1].Emoji); i-- {
		m.Reactions[i], m.Reactions[i-1] = m.Reactions[i-1], m.Reactions[i]
	}
}
