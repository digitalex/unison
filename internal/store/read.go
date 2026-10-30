package store

import (
	"context"
	"database/sql"
	"fmt"
)

// The chat read marker (#124): the newest chat message a participant has
// read, kept on the server so web, iOS and the CLI agree on what is unread.
// It only moves forward. It is private to its participant: no event is
// recorded, so others can't see what you have read.

// ReadUpTo returns the participant's read marker (0 if nothing is read).
func (s *Store) ReadUpTo(ctx context.Context, p *Participant) (int64, error) {
	var upTo sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT read_up_to FROM participants WHERE id = ?`, p.ID).Scan(&upTo)
	return upTo.Int64, err
}

// MarkRead moves the actor's read marker up to message upTo (a message in
// their workspace), and returns the marker after the move. notified says
// whether the newly read messages include one that notified the actor's
// devices (it asked them to act, or @mentioned them), so their phone can
// clear those notifications. Marking older messages read changes nothing.
func (s *Store) MarkRead(ctx context.Context, actor *Participant, upTo int64) (marker int64, notified bool, err error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM messages WHERE workspace_id = ? AND id = ?)`,
		actor.WorkspaceID, upTo).Scan(&exists); err != nil {
		return 0, false, err
	}
	if !exists {
		return 0, false, fmt.Errorf("message %d: %w", upTo, ErrNotFound)
	}
	old, err := s.ReadUpTo(ctx, actor)
	if err != nil {
		return 0, false, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE participants SET read_up_to = ? WHERE id = ? AND COALESCE(read_up_to, 0) < ?`,
		upTo, actor.ID, upTo)
	if err != nil {
		return 0, false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Already read that far (perhaps by another of their devices just now).
		marker, err := s.ReadUpTo(ctx, actor)
		return marker, false, err
	}
	notified, err = s.notifiedBetween(ctx, actor, old, upTo)
	return upTo, notified, err
}

// notifiedBetween reports whether a message by someone else in (after, upTo]
// asked p to act or @mentioned p: the posts that push to p (see
// PushRecipients).
func (s *Store) notifiedBetween(ctx context.Context, p *Participant, after, upTo int64) (bool, error) {
	var asked bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM message_actions
		WHERE workspace_id = ? AND participant_id = ? AND message_id > ? AND message_id <= ?)`,
		p.WorkspaceID, p.ID, after, upTo).Scan(&asked); err != nil || asked {
		return asked, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT body FROM messages WHERE workspace_id = ? AND author_id <> ? AND id > ? AND id <= ?`,
		p.WorkspaceID, p.ID, after, upTo)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return false, err
		}
		if mentions(body, p.Name) {
			return true, nil
		}
	}
	return false, rows.Err()
}
