package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
)

// Action-required messages (#49) replaced the inbox. A chat message that
// @mentions people can ask them to act: review a PR, answer a question, do
// some manual setup. Each participant it mentions has an open action until
// they mark it done (or undo that), reply in the message's thread, or react
// to it. Done is per account: any of a member's participants (devices)
// marks it done for all of them.

// accountParticipants selects the IDs of actor's participants in its
// workspace: actor itself and, when it is linked to an account, the
// account's other participants. Arguments: workspace ID, actor ID, user ID.
const accountParticipants = `SELECT id FROM participants WHERE workspace_id = ? AND (id = ? OR (user_id IS NOT NULL AND user_id = ?))`

// mentionedIn returns the participants of wsID (other than the author)
// that body @mentions by name, matched as `unison wait --mentions` does.
func mentionedIn(t *tx, wsID string, authorID int64, body string) ([]int64, error) {
	rows, err := t.Query(`SELECT id, name FROM participants WHERE workspace_id = ? AND id <> ? ORDER BY id`, wsID, authorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		if regexp.MustCompile(`(?i)@` + regexp.QuoteMeta(name) + `\b`).MatchString(body) {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

// addActions asks the participants a new message mentions to act on it, or
// refuses a message that mentions nobody.
func addActions(t *tx, m *Message) error {
	ids, err := mentionedIn(t, m.WorkspaceID, m.AuthorID, m.Body)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return fmt.Errorf("%w: a message that needs action must @mention who should act", ErrInvalid)
	}
	for _, id := range ids {
		if _, err := t.Exec(`INSERT INTO message_actions (workspace_id, message_id, participant_id) VALUES (?, ?, ?)`, m.WorkspaceID, m.ID, id); err != nil {
			return err
		}
	}
	m.ActionRequired, m.ActionFor = true, ids
	return nil
}

// markDone marks message messageID done (or not done) for actor's account,
// and reports whether anything changed. It is ErrInvalid when the message
// asks none of actor's participants to act.
func markDone(t *tx, actor *Participant, messageID int64, done bool, now int64) (bool, error) {
	var mine int
	if err := t.QueryRow(`SELECT COUNT(*) FROM message_actions WHERE message_id = ? AND participant_id IN (`+accountParticipants+`)`,
		messageID, actor.WorkspaceID, actor.ID, actor.UserID).Scan(&mine); err != nil {
		return false, err
	}
	if mine == 0 {
		return false, fmt.Errorf("%w: message %d doesn't ask you to act", ErrInvalid, messageID)
	}
	q := `UPDATE message_actions SET done_at = ? WHERE message_id = ? AND done_at IS NULL AND participant_id IN (` + accountParticipants + `)`
	args := []any{now, messageID, actor.WorkspaceID, actor.ID, actor.UserID}
	if !done {
		q = `UPDATE message_actions SET done_at = NULL WHERE message_id = ? AND done_at IS NOT NULL AND participant_id IN (` + accountParticipants + `)`
		args = args[1:]
	}
	res, err := t.Exec(q, args...)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// markDoneIfAsked marks messageID done for actor's account when it asks
// actor to act (a reply in its thread, a reaction); otherwise it does
// nothing.
func markDoneIfAsked(t *tx, actor *Participant, messageID int64, now int64) error {
	if _, err := markDone(t, actor, messageID, true, now); err != nil && !errors.Is(err, ErrInvalid) {
		return err
	}
	return nil
}

// SetDone marks a message that asks actor to act done (or, with done
// false, not done) for actor's account, and returns the message. A change
// records message.done (or message.undone) with the message; repeating
// either is a no-op without an event.
func (s *Store) SetDone(ctx context.Context, actor *Participant, messageID int64, done bool) (*Message, []Event, error) {
	var m *Message
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var err error
		if m, err = scanMessage(t.QueryRow(selectMessages+` WHERE m.id = ? AND m.workspace_id = ?`, messageID, actor.WorkspaceID)); err != nil {
			return fmt.Errorf("message %d: %w", messageID, err)
		}
		changed, err := markDone(t, actor, messageID, done, log.now)
		if err != nil {
			return err
		}
		msgs := []Message{*m}
		if err := attachDetails(t.Query, actor.WorkspaceID, msgs); err != nil {
			return err
		}
		m = &msgs[0]
		if !changed {
			return nil
		}
		return log.add(actor.WorkspaceID, map[bool]string{true: "message.done", false: "message.undone"}[done], actor.ID, m)
	})
	if err != nil {
		return nil, nil, err
	}
	return m, evs, nil
}

// OpenActions returns the messages that ask actor's account to act and
// that it hasn't marked done, newest first.
func (s *Store) OpenActions(ctx context.Context, actor *Participant) ([]Message, error) {
	return s.withDetails(ctx, actor.WorkspaceID)(scanMessages(s.db.QueryContext(ctx, (selectMessages + ` WHERE m.workspace_id = ? AND m.id IN (
		SELECT message_id FROM message_actions WHERE done_at IS NULL AND participant_id IN (` + accountParticipants + `)) ORDER BY m.id DESC`),
		actor.WorkspaceID, actor.WorkspaceID, actor.ID, actor.UserID)))
}

// CountOpenActions is how many messages OpenActions would return.
func (s *Store) CountOpenActions(ctx context.Context, actor *Participant) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, (`SELECT COUNT(DISTINCT message_id) FROM message_actions WHERE workspace_id = ? AND done_at IS NULL AND participant_id IN (` + accountParticipants + `)`),
		actor.WorkspaceID, actor.WorkspaceID, actor.ID, actor.UserID).Scan(&n)
	return n, err
}

// attachActions fills in who msgs (all from wsID) ask to act, and who of
// them has.
func attachActions(query queryFunc, wsID string, msgs []Message) error {
	if len(msgs) == 0 {
		return nil
	}
	byID := make(map[int64]*Message, len(msgs))
	lo, hi := msgs[0].ID, msgs[0].ID
	for i := range msgs {
		byID[msgs[i].ID] = &msgs[i]
		lo, hi = min(lo, msgs[i].ID), max(hi, msgs[i].ID)
	}
	rows, err := query(`SELECT message_id, participant_id, done_at FROM message_actions
		WHERE workspace_id = ? AND message_id BETWEEN ? AND ? ORDER BY id`, wsID, lo, hi)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, who int64
		var doneAt sql.NullInt64
		if err := rows.Scan(&id, &who, &doneAt); err != nil {
			return err
		}
		if m := byID[id]; m != nil {
			m.ActionRequired = true
			m.ActionFor = append(m.ActionFor, who)
			if doneAt.Valid {
				m.DoneBy = append(m.DoneBy, who)
			}
		}
	}
	return rows.Err()
}

// attachDetails fills in the reactions and actions of msgs, all from wsID.
func attachDetails(query queryFunc, wsID string, msgs []Message) error {
	if err := attachReactions(query, wsID, msgs); err != nil {
		return err
	}
	return attachActions(query, wsID, msgs)
}
