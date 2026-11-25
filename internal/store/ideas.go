package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Ideas (#917): early-stage proposals with a Markdown body and a
// discussion, kept apart from the prioritized backlog until somebody moves
// one there ("promotes" it). Ideas are numbered on their own (idea 3 is not
// backlog item #3), so text calls them "idea N" and never "#N", which links
// to a backlog item.

// Idea statuses.
const (
	IdeaOpen     = "open"
	IdeaPromoted = "promoted"
)

// Idea is an idea on the Ideas board.
type Idea struct {
	ID          int64  `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	Status      string `json:"status"`
	CreatedBy   int64  `json:"created_by"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
	// PromotedItemID is the backlog item the idea was moved to, with its
	// per-workspace number, who moved it and when; nil while the idea is open.
	PromotedItemID     *int64 `json:"promoted_item_id,omitempty"`
	PromotedItemNumber *int64 `json:"promoted_item_number,omitempty"`
	PromotedBy         *int64 `json:"promoted_by,omitempty"`
	PromotedAt         *int64 `json:"promoted_at,omitempty"`
	CommentCount       int    `json:"comment_count"`
}

// IdeaComment is a comment in an idea's discussion.
type IdeaComment struct {
	ID          int64  `json:"id"`
	IdeaID      int64  `json:"idea_id"`
	WorkspaceID string `json:"workspace_id"`
	AuthorID    int64  `json:"author_id"`
	Body        string `json:"body"`
	CreatedAt   int64  `json:"created_at"`
}

// IdeaCommentEvent is the data of idea.commented and idea.comment_deleted
// events: the comment and the idea it belongs to.
type IdeaCommentEvent struct {
	Comment *IdeaComment `json:"comment"`
	Idea    *Idea        `json:"idea"`
}

// IdeaPromotion is what PromoteIdea returns: the idea, now promoted, and
// the backlog item made from it.
type IdeaPromotion struct {
	Idea *Idea `json:"idea"`
	Item *Item `json:"item"`
}

const ideaCols = `id, workspace_id, title, body, status, created_by, created_at, updated_at, promoted_item_id,
	CASE WHEN promoted_item_id IS NOT NULL THEN COALESCE((SELECT i.number FROM items i WHERE i.id = ideas.promoted_item_id), promoted_item_id) END,
	promoted_by, promoted_at,
	(SELECT COUNT(*) FROM idea_comments c WHERE c.idea_id = ideas.id)`

func scanIdea(row interface{ Scan(...any) error }) (*Idea, error) {
	var d Idea
	var item, itemNum, by, at sql.NullInt64
	if err := row.Scan(&d.ID, &d.WorkspaceID, &d.Title, &d.Body, &d.Status, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt,
		&item, &itemNum, &by, &at, &d.CommentCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if item.Valid {
		d.PromotedItemID = &item.Int64
	}
	if itemNum.Valid {
		d.PromotedItemNumber = &itemNum.Int64
	}
	if by.Valid {
		d.PromotedBy = &by.Int64
	}
	if at.Valid {
		d.PromotedAt = &at.Int64
	}
	return &d, nil
}

func getIdea(t *tx, wsID string, id int64) (*Idea, error) {
	d, err := scanIdea(t.QueryRow(`SELECT `+ideaCols+` FROM ideas WHERE workspace_id = ? AND id = ?`, wsID, id))
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("idea %d: %w", id, ErrNotFound)
	}
	return d, err
}

// readIdeas runs read in a read-only transaction.
func (s *Store) readIdeas(ctx context.Context, read func(*tx) error) error {
	t, err := s.db.beginRead(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback()
	return read(t)
}

// ListIdeas returns the ideas with the given status ("" = open, "promoted",
// or "all"): open ones newest first, then promoted ones most recently
// promoted first.
func (s *Store) ListIdeas(ctx context.Context, wsID, status string) ([]Idea, error) {
	query := `SELECT ` + ideaCols + ` FROM ideas WHERE workspace_id = ?`
	args := []any{wsID}
	switch status {
	case "", IdeaOpen:
		query += ` AND status = ?`
		args = append(args, IdeaOpen)
	case IdeaPromoted:
		query += ` AND status = ?`
		args = append(args, IdeaPromoted)
	case "all":
	default:
		return nil, fmt.Errorf("%w: unknown idea status %q (want open, promoted or all)", ErrInvalid, status)
	}
	query += ` ORDER BY status = 'promoted', promoted_at DESC, id DESC`
	out := []Idea{}
	err := s.readIdeas(ctx, func(t *tx) error {
		rows, err := t.Query(query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			d, err := scanIdea(rows)
			if err != nil {
				return err
			}
			out = append(out, *d)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetIdea returns one idea.
func (s *Store) GetIdea(ctx context.Context, wsID string, id int64) (*Idea, error) {
	var d *Idea
	err := s.readIdeas(ctx, func(t *tx) error {
		var err error
		d, err = getIdea(t, wsID, id)
		return err
	})
	return d, err
}

// AddIdea adds an open idea and records idea.added.
func (s *Store) AddIdea(ctx context.Context, actor *Participant, title, body string) (*Idea, []Event, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, nil, fmt.Errorf("%w: idea title is required", ErrInvalid)
	}
	var d *Idea
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		id, err := t.insert(`INSERT INTO ideas (workspace_id, title, body, status, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "id",
			actor.WorkspaceID, title, strings.TrimSpace(body), IdeaOpen, actor.ID, log.now, log.now)
		if err != nil {
			return err
		}
		if d, err = getIdea(t, actor.WorkspaceID, id); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "idea.added", actor.ID, d)
	})
	if err != nil {
		return nil, nil, err
	}
	return d, evs, nil
}

// IdeaPatch holds optional idea field updates.
type IdeaPatch struct {
	Title *string `json:"title"`
	Body  *string `json:"body"`
}

// UpdateIdea edits an idea's title and/or body and records idea.updated.
// Promoted ideas can still be edited: the backlog item has its own copy.
func (s *Store) UpdateIdea(ctx context.Context, actor *Participant, id int64, p IdeaPatch) (*Idea, []Event, error) {
	var d *Idea
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var err error
		if d, err = getIdea(t, actor.WorkspaceID, id); err != nil {
			return err
		}
		if p.Title != nil {
			if strings.TrimSpace(*p.Title) == "" {
				return fmt.Errorf("%w: idea title cannot be empty", ErrInvalid)
			}
			d.Title = strings.TrimSpace(*p.Title)
		}
		if p.Body != nil {
			d.Body = strings.TrimSpace(*p.Body)
		}
		if _, err := t.Exec(`UPDATE ideas SET title = ?, body = ?, updated_at = ? WHERE id = ?`, d.Title, d.Body, log.now, d.ID); err != nil {
			return err
		}
		d.UpdatedAt = log.now
		return log.add(actor.WorkspaceID, "idea.updated", actor.ID, d)
	})
	if err != nil {
		return nil, nil, err
	}
	return d, evs, nil
}

// DeleteIdea removes an idea and its discussion and records idea.deleted.
// A backlog item it was promoted to stays.
func (s *Store) DeleteIdea(ctx context.Context, actor *Participant, id int64) ([]Event, error) {
	return s.tx(ctx, func(t *tx, log *eventLog) error {
		d, err := getIdea(t, actor.WorkspaceID, id)
		if err != nil {
			return err
		}
		if _, err := t.Exec(`DELETE FROM idea_comments WHERE idea_id = ?`, d.ID); err != nil {
			return err
		}
		if _, err := t.Exec(`DELETE FROM ideas WHERE id = ?`, d.ID); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "idea.deleted", actor.ID, d)
	})
}

// PromoteIdea moves an open idea to the backlog ("Move to backlog"): one
// transaction adds a backlog item at position (<= 0: bottom) with the
// idea's title, and a body made of the idea's body, a reference back to
// the idea, and its discussion so far; then marks the idea promoted to that
// item. It records backlog.added and idea.promoted.
func (s *Store) PromoteIdea(ctx context.Context, actor *Participant, id int64, position int) (*IdeaPromotion, []Event, error) {
	var out IdeaPromotion
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		d, err := getIdea(t, actor.WorkspaceID, id)
		if err != nil {
			return err
		}
		if d.Status == IdeaPromoted {
			num := *d.PromotedItemID
			if d.PromotedItemNumber != nil && *d.PromotedItemNumber > 0 {
				num = *d.PromotedItemNumber
			}
			return fmt.Errorf("%w: idea %d was already moved to the backlog as #%d", ErrConflict, d.ID, num)
		}
		comments, err := listIdeaComments(t, actor.WorkspaceID, d.ID)
		if err != nil {
			return err
		}
		body, err := promotedBody(t, actor.WorkspaceID, d, comments)
		if err != nil {
			return err
		}
		if out.Item, err = addItem(t, log, actor, d.Title, body, position, nil, nil); err != nil {
			return err
		}
		if _, err := t.Exec(`UPDATE ideas SET status = ?, promoted_item_id = ?, promoted_by = ?, promoted_at = ?, updated_at = ? WHERE id = ?`,
			IdeaPromoted, out.Item.ID, actor.ID, log.now, log.now, d.ID); err != nil {
			return err
		}
		if out.Idea, err = getIdea(t, actor.WorkspaceID, d.ID); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "idea.promoted", actor.ID, out.Idea)
	})
	if err != nil {
		return nil, nil, err
	}
	return &out, evs, nil
}

// promotedBody is the body of the backlog item an idea is promoted to.
func promotedBody(t *tx, wsID string, d *Idea, comments []IdeaComment) (string, error) {
	var b strings.Builder
	if d.Body != "" {
		b.WriteString(d.Body)
		b.WriteString("\n\n---\n\n")
	}
	fmt.Fprintf(&b, "_Moved here from idea %d on the Ideas board (`unison idea show %d`)._", d.ID, d.ID)
	if len(comments) == 0 {
		return b.String(), nil
	}
	b.WriteString("\n\n**Discussion on the idea:**\n")
	names := map[int64]string{}
	for _, c := range comments {
		name, ok := names[c.AuthorID]
		if !ok {
			if err := t.QueryRow(`SELECT name FROM participants WHERE id = ?`, c.AuthorID).Scan(&name); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return "", err
			}
			if name == "" {
				name = fmt.Sprintf("participant %d", c.AuthorID)
			}
			names[c.AuthorID] = name
		}
		quoted := strings.ReplaceAll(c.Body, "\n", "\n> ")
		fmt.Fprintf(&b, "\n> **%s:** %s\n", name, quoted)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// ---- Discussion ----

const ideaCommentCols = `id, idea_id, workspace_id, author_id, body, created_at`

func scanIdeaComment(row interface{ Scan(...any) error }) (*IdeaComment, error) {
	var c IdeaComment
	if err := row.Scan(&c.ID, &c.IdeaID, &c.WorkspaceID, &c.AuthorID, &c.Body, &c.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

func listIdeaComments(t *tx, wsID string, ideaID int64) ([]IdeaComment, error) {
	rows, err := t.Query(`SELECT `+ideaCommentCols+` FROM idea_comments WHERE workspace_id = ? AND idea_id = ? ORDER BY id`, wsID, ideaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IdeaComment{}
	for rows.Next() {
		c, err := scanIdeaComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// ListIdeaComments returns an idea's discussion, oldest first.
func (s *Store) ListIdeaComments(ctx context.Context, wsID string, ideaID int64) ([]IdeaComment, error) {
	var out []IdeaComment
	err := s.readIdeas(ctx, func(t *tx) error {
		if _, err := getIdea(t, wsID, ideaID); err != nil {
			return err
		}
		var err error
		out, err = listIdeaComments(t, wsID, ideaID)
		return err
	})
	return out, err
}

// AddIdeaComment adds a comment to an idea's discussion and records
// idea.commented. Promoted ideas can still be discussed.
func (s *Store) AddIdeaComment(ctx context.Context, actor *Participant, ideaID int64, body string) (*IdeaComment, []Event, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, nil, fmt.Errorf("%w: comment body is required", ErrInvalid)
	}
	var c *IdeaComment
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		if _, err := getIdea(t, actor.WorkspaceID, ideaID); err != nil {
			return err
		}
		c = &IdeaComment{IdeaID: ideaID, WorkspaceID: actor.WorkspaceID, AuthorID: actor.ID, Body: body, CreatedAt: log.now}
		id, err := t.insert(`INSERT INTO idea_comments (idea_id, workspace_id, author_id, body, created_at) VALUES (?, ?, ?, ?, ?)`, "id",
			c.IdeaID, c.WorkspaceID, c.AuthorID, c.Body, c.CreatedAt)
		if err != nil {
			return err
		}
		c.ID = id
		d, err := getIdea(t, actor.WorkspaceID, ideaID)
		if err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "idea.commented", actor.ID, IdeaCommentEvent{Comment: c, Idea: d})
	})
	if err != nil {
		return nil, nil, err
	}
	return c, evs, nil
}

// DeleteIdeaComment deletes a comment from an idea's discussion and
// records idea.comment_deleted. Only its author may delete it.
func (s *Store) DeleteIdeaComment(ctx context.Context, actor *Participant, ideaID, commentID int64) ([]Event, error) {
	return s.tx(ctx, func(t *tx, log *eventLog) error {
		c, err := scanIdeaComment(t.QueryRow(`SELECT `+ideaCommentCols+` FROM idea_comments WHERE workspace_id = ? AND idea_id = ? AND id = ?`,
			actor.WorkspaceID, ideaID, commentID))
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("comment %d on idea %d: %w", commentID, ideaID, ErrNotFound)
		}
		if err != nil {
			return err
		}
		if c.AuthorID != actor.ID {
			return fmt.Errorf("%w: only the author can delete a comment", ErrForbidden)
		}
		if _, err := t.Exec(`DELETE FROM idea_comments WHERE id = ?`, c.ID); err != nil {
			return err
		}
		d, err := getIdea(t, actor.WorkspaceID, ideaID)
		if err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "idea.comment_deleted", actor.ID, IdeaCommentEvent{Comment: c, Idea: d})
	})
}
