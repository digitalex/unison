package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const itemCols = `id, workspace_id, title, body, status, position, assignee_id, lease_expires_at, created_by, created_at, updated_at, closed_at, close_note`

func scanItem(row interface{ Scan(...any) error }) (*Item, error) {
	var it Item
	var assignee, lease, closed sql.NullInt64
	if err := row.Scan(&it.ID, &it.WorkspaceID, &it.Title, &it.Body, &it.Status, &it.Position, &assignee, &lease,
		&it.CreatedBy, &it.CreatedAt, &it.UpdatedAt, &closed, &it.CloseNote); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if assignee.Valid {
		it.AssigneeID = &assignee.Int64
	}
	if lease.Valid {
		it.LeaseExpiresAt = &lease.Int64
	}
	if closed.Valid {
		it.ClosedAt = &closed.Int64
	}
	return &it, nil
}

func getItem(t *tx, wsID string, id int64) (*Item, error) {
	it, err := scanItem(t.QueryRow(`SELECT `+itemCols+` FROM items WHERE workspace_id = ? AND id = ?`, wsID, id))
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("backlog item #%d: %w", id, ErrNotFound)
	}
	return it, err
}

// queuedIDs returns the IDs of open and assigned items in priority order.
func queuedIDs(t *tx, wsID string) ([]int64, error) {
	rows, err := t.Query(`SELECT id FROM items WHERE workspace_id = ? AND status != ? ORDER BY position, id`, wsID, StatusDone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// renumber assigns positions 1..n to ids in order.
func renumber(t *tx, ids []int64) error {
	for i, id := range ids {
		if _, err := t.Exec(`UPDATE items SET position = ? WHERE id = ?`, i+1, id); err != nil {
			return err
		}
	}
	return nil
}

// insertAt places id at the 1-based position (clamped) within the queue.
// position <= 0 means the bottom of the queue.
func insertAt(t *tx, wsID string, id int64, position int) error {
	ids, err := queuedIDs(t, wsID)
	if err != nil {
		return err
	}
	rest := make([]int64, 0, len(ids))
	for _, other := range ids {
		if other != id {
			rest = append(rest, other)
		}
	}
	if position <= 0 || position > len(rest)+1 {
		position = len(rest) + 1
	}
	ordered := append(append(append([]int64{}, rest[:position-1]...), id), rest[position-1:]...)
	return renumber(t, ordered)
}

// expireLeases releases assigned items whose lease has run out, recording
// a backlog.lease_expired event for each.
func expireLeases(t *tx, log *eventLog, wsID string) error {
	query := `SELECT ` + itemCols + ` FROM items WHERE status = ? AND lease_expires_at <= ?`
	args := []any{StatusAssigned, log.now}
	if wsID != "" {
		query += ` AND workspace_id = ?`
		args = append(args, wsID)
	}
	rows, err := t.Query(query, args...)
	if err != nil {
		return err
	}
	var expired []*Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			rows.Close()
			return err
		}
		expired = append(expired, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, it := range expired {
		if _, err := t.Exec(`UPDATE items SET status = ?, assignee_id = NULL, lease_expires_at = NULL, updated_at = ? WHERE id = ?`,
			StatusOpen, log.now, it.ID); err != nil {
			return err
		}
		it.Status, it.AssigneeID, it.LeaseExpiresAt, it.UpdatedAt = StatusOpen, nil, nil, log.now
		if err := log.add(it.WorkspaceID, "backlog.lease_expired", 0, it); err != nil {
			return err
		}
	}
	return nil
}

// ExpireLeases releases every expired lease across all workspaces.
func (s *Store) ExpireLeases(ctx context.Context) ([]Event, error) {
	return s.tx(ctx, func(t *tx, log *eventLog) error { return expireLeases(t, log, "") })
}

// ListBacklog returns items with the given status ("" = open and assigned,
// "all" = everything). Queued items come in priority order; done items come
// most recently closed first.
func (s *Store) ListBacklog(ctx context.Context, wsID, status string) ([]Item, []Event, error) {
	var out []Item
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		if err := expireLeases(t, log, wsID); err != nil {
			return err
		}
		query := `SELECT ` + itemCols + ` FROM items WHERE workspace_id = ?`
		args := []any{wsID}
		switch status {
		case "":
			query += ` AND status != ?`
			args = append(args, StatusDone)
		case "all":
		case StatusOpen, StatusAssigned, StatusDone:
			query += ` AND status = ?`
			args = append(args, status)
		default:
			return fmt.Errorf("%w: unknown status %q (want open, assigned, done or all)", ErrInvalid, status)
		}
		query += ` ORDER BY status = 'done', position, closed_at DESC, id`
		rows, err := t.Query(query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		out = []Item{}
		for rows.Next() {
			it, err := scanItem(rows)
			if err != nil {
				return err
			}
			out = append(out, *it)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, nil, err
	}
	return out, evs, nil
}

// GetItem returns one backlog item.
func (s *Store) GetItem(ctx context.Context, wsID string, id int64) (*Item, []Event, error) {
	var it *Item
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		if err := expireLeases(t, log, wsID); err != nil {
			return err
		}
		var err error
		it, err = getItem(t, wsID, id)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return it, evs, nil
}

// AddItem adds a backlog item at the 1-based position (<= 0: bottom).
func (s *Store) AddItem(ctx context.Context, actor *Participant, title, body string, position int) (*Item, []Event, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, nil, fmt.Errorf("%w: item title is required", ErrInvalid)
	}
	var it *Item
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		id, err := t.insert(`INSERT INTO items (workspace_id, title, body, status, position, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, 0, ?, ?, ?)`, "id",
			actor.WorkspaceID, title, strings.TrimSpace(body), StatusOpen, actor.ID, log.now, log.now)
		if err != nil {
			return err
		}
		if err := insertAt(t, actor.WorkspaceID, id, position); err != nil {
			return err
		}
		if it, err = getItem(t, actor.WorkspaceID, id); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "backlog.added", actor.ID, it)
	})
	if err != nil {
		return nil, nil, err
	}
	return it, evs, nil
}

// ItemPatch holds optional item field updates.
type ItemPatch struct {
	Title *string `json:"title"`
	Body  *string `json:"body"`
}

// UpdateItem edits an item's title and/or body.
func (s *Store) UpdateItem(ctx context.Context, actor *Participant, id int64, p ItemPatch) (*Item, []Event, error) {
	return s.mutateItem(ctx, actor, id, "backlog.updated", func(t *tx, it *Item, now int64) error {
		if p.Title != nil {
			if strings.TrimSpace(*p.Title) == "" {
				return fmt.Errorf("%w: item title cannot be empty", ErrInvalid)
			}
			it.Title = strings.TrimSpace(*p.Title)
		}
		if p.Body != nil {
			it.Body = strings.TrimSpace(*p.Body)
		}
		_, err := t.Exec(`UPDATE items SET title = ?, body = ?, updated_at = ? WHERE id = ?`, it.Title, it.Body, now, it.ID)
		return err
	})
}

// MoveItem moves a queued item to the 1-based position (clamped).
func (s *Store) MoveItem(ctx context.Context, actor *Participant, id int64, position int) (*Item, []Event, error) {
	if position < 1 {
		return nil, nil, fmt.Errorf("%w: position must be >= 1", ErrInvalid)
	}
	return s.mutateItem(ctx, actor, id, "backlog.moved", func(t *tx, it *Item, now int64) error {
		if it.Status == StatusDone {
			return fmt.Errorf("%w: item #%d is done; reopen it first", ErrConflict, it.ID)
		}
		if _, err := t.Exec(`UPDATE items SET updated_at = ? WHERE id = ?`, now, it.ID); err != nil {
			return err
		}
		return insertAt(t, it.WorkspaceID, it.ID, position)
	})
}

// AssignItem atomically assigns an item to assigneeID with a fresh lease.
// It succeeds only if the item is open, its lease has expired, or it is
// already assigned to the same participant (which renews the lease).
func (s *Store) AssignItem(ctx context.Context, actor *Participant, id, assigneeID int64) (*Item, []Event, error) {
	return s.mutateItem(ctx, actor, id, "backlog.assigned", func(t *tx, it *Item, now int64) error {
		return assign(t, it, assigneeID, now)
	})
}

func assign(t *tx, it *Item, assigneeID, now int64) error {
	var exists int
	if err := t.QueryRow(`SELECT COUNT(*) FROM participants WHERE id = ? AND workspace_id = ?`, assigneeID, it.WorkspaceID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return fmt.Errorf("participant %d: %w", assigneeID, ErrNotFound)
	}
	switch {
	case it.Status == StatusDone:
		return fmt.Errorf("%w: item #%d is done", ErrConflict, it.ID)
	case it.Status == StatusAssigned && *it.AssigneeID != assigneeID:
		return fmt.Errorf("%w: item #%d is already assigned to participant %d", ErrConflict, it.ID, *it.AssigneeID)
	}
	lease := now + LeaseDuration.Milliseconds()
	res, err := t.Exec(`UPDATE items SET status = ?, assignee_id = ?, lease_expires_at = ?, updated_at = ?
		WHERE id = ? AND (status = ? OR (status = ? AND assignee_id = ?))`,
		StatusAssigned, assigneeID, lease, now, it.ID, StatusOpen, StatusAssigned, assigneeID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("%w: item #%d was claimed concurrently", ErrConflict, it.ID)
	}
	it.Status, it.AssigneeID, it.LeaseExpiresAt = StatusAssigned, &assigneeID, &lease
	return nil
}

// ClaimNext atomically assigns the highest-priority open item to the actor.
// It returns ErrNotFound if no item is open.
func (s *Store) ClaimNext(ctx context.Context, actor *Participant) (*Item, []Event, error) {
	var it *Item
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		if err := expireLeases(t, log, actor.WorkspaceID); err != nil {
			return err
		}
		var err error
		it, err = scanItem(t.QueryRow(`SELECT `+itemCols+` FROM items WHERE workspace_id = ? AND status = ? ORDER BY position, id LIMIT 1`,
			actor.WorkspaceID, StatusOpen))
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("%w: no open backlog items", ErrNotFound)
		}
		if err != nil {
			return err
		}
		if err := assign(t, it, actor.ID, log.now); err != nil {
			return err
		}
		it.UpdatedAt = log.now
		return log.add(actor.WorkspaceID, "backlog.assigned", actor.ID, it)
	})
	if err != nil {
		return nil, nil, err
	}
	return it, evs, nil
}

// ReleaseItem returns an assigned item to the open queue.
func (s *Store) ReleaseItem(ctx context.Context, actor *Participant, id int64) (*Item, []Event, error) {
	return s.mutateItem(ctx, actor, id, "backlog.released", func(t *tx, it *Item, now int64) error {
		if it.Status != StatusAssigned {
			return fmt.Errorf("%w: item #%d is %s, not assigned", ErrConflict, it.ID, it.Status)
		}
		it.Status, it.AssigneeID, it.LeaseExpiresAt = StatusOpen, nil, nil
		_, err := t.Exec(`UPDATE items SET status = ?, assignee_id = NULL, lease_expires_at = NULL, updated_at = ? WHERE id = ?`, StatusOpen, now, it.ID)
		return err
	})
}

// CloseItem marks an item done with an optional note (e.g. a PR link).
func (s *Store) CloseItem(ctx context.Context, actor *Participant, id int64, note string) (*Item, []Event, error) {
	return s.mutateItem(ctx, actor, id, "backlog.closed", func(t *tx, it *Item, now int64) error {
		if it.Status == StatusDone {
			return fmt.Errorf("%w: item #%d is already done", ErrConflict, it.ID)
		}
		it.Status, it.LeaseExpiresAt, it.ClosedAt, it.CloseNote = StatusDone, nil, &now, strings.TrimSpace(note)
		if _, err := t.Exec(`UPDATE items SET status = ?, position = 0, lease_expires_at = NULL, closed_at = ?, close_note = ?, updated_at = ? WHERE id = ?`,
			StatusDone, now, it.CloseNote, now, it.ID); err != nil {
			return err
		}
		ids, err := queuedIDs(t, it.WorkspaceID)
		if err != nil {
			return err
		}
		return renumber(t, ids)
	})
}

// ReopenItem returns a done item to the bottom of the open queue.
func (s *Store) ReopenItem(ctx context.Context, actor *Participant, id int64) (*Item, []Event, error) {
	return s.mutateItem(ctx, actor, id, "backlog.reopened", func(t *tx, it *Item, now int64) error {
		if it.Status != StatusDone {
			return fmt.Errorf("%w: item #%d is not done", ErrConflict, it.ID)
		}
		if _, err := t.Exec(`UPDATE items SET status = ?, assignee_id = NULL, closed_at = NULL, close_note = '', updated_at = ? WHERE id = ?`,
			StatusOpen, now, it.ID); err != nil {
			return err
		}
		return insertAt(t, it.WorkspaceID, it.ID, 0)
	})
}

// DeleteItem removes an item entirely.
func (s *Store) DeleteItem(ctx context.Context, actor *Participant, id int64) ([]Event, error) {
	return s.tx(ctx, func(t *tx, log *eventLog) error {
		it, err := getItem(t, actor.WorkspaceID, id)
		if err != nil {
			return err
		}
		if _, err := t.Exec(`DELETE FROM items WHERE id = ?`, id); err != nil {
			return err
		}
		ids, err := queuedIDs(t, actor.WorkspaceID)
		if err != nil {
			return err
		}
		if err := renumber(t, ids); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "backlog.deleted", actor.ID, it)
	})
}

// mutateItem loads an item (after expiring stale leases), applies fn, then
// reloads it and records an event of the given type.
func (s *Store) mutateItem(ctx context.Context, actor *Participant, id int64, eventType string, fn func(*tx, *Item, int64) error) (*Item, []Event, error) {
	var it *Item
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		if err := expireLeases(t, log, actor.WorkspaceID); err != nil {
			return err
		}
		var err error
		if it, err = getItem(t, actor.WorkspaceID, id); err != nil {
			return err
		}
		if err := fn(t, it, log.now); err != nil {
			return err
		}
		if it, err = getItem(t, actor.WorkspaceID, id); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, eventType, actor.ID, it)
	})
	if err != nil {
		return nil, nil, err
	}
	return it, evs, nil
}
