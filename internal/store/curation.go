package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Comment is a comment on a backlog item.
type Comment struct {
	ID          int64  `json:"id"`
	ItemID      int64  `json:"item_id"`
	WorkspaceID string `json:"workspace_id"`
	AuthorID    int64  `json:"author_id"`
	Body        string `json:"body"`
	CreatedAt   int64  `json:"created_at"`
}

// ItemPR is a pull request linked to a backlog item.
type ItemPR struct {
	ID        int64  `json:"id"`
	ItemID    int64  `json:"item_id"`
	URL       string `json:"url"`
	AddedBy   int64  `json:"added_by"`
	CreatedAt int64  `json:"created_at"`
}

// CommentEvent is the data of comment.added and comment.deleted events: the
// comment and the item it belongs to (so `unison wait --mentions` can tell
// whether the item is assigned to you).
type CommentEvent struct {
	Comment *Comment `json:"comment"`
	Item    *Item    `json:"item"`
}

// attachExtras fills in the PRs, comment counts and message counts of items,
// all from wsID.
func attachExtras(t *tx, wsID string, items []*Item) error {
	if len(items) == 0 {
		return nil
	}
	byID := make(map[int64]*Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	filter, args := `workspace_id = ?`, []any{wsID}
	if len(items) == 1 {
		filter, args = `workspace_id = ? AND item_id = ?`, []any{wsID, items[0].ID}
	} else if len(items) > 1 {
		marks := strings.TrimSuffix(strings.Repeat("?, ", len(items)), ", ")
		filter = `workspace_id = ? AND item_id IN (` + marks + `)`
		args = make([]any, 0, 1+len(items))
		args = append(args, wsID)
		for _, it := range items {
			args = append(args, it.ID)
		}
	}
	rows, err := t.Query(`SELECT id, item_id, url, added_by, created_at FROM item_prs WHERE `+filter+` ORDER BY id`, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var pr ItemPR
		if err := rows.Scan(&pr.ID, &pr.ItemID, &pr.URL, &pr.AddedBy, &pr.CreatedAt); err != nil {
			rows.Close()
			return err
		}
		if it := byID[pr.ItemID]; it != nil {
			it.PRs = append(it.PRs, pr)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if err := countPerItem(t, `item_comments WHERE `+filter, args, func(it *Item, n int) { it.CommentCount = n }, byID); err != nil {
		return err
	}
	return countPerItem(t, `messages WHERE item_id IS NOT NULL AND `+filter, args, func(it *Item, n int) { it.MessageCount = n }, byID)
}

// countPerItem counts the rows of `from` (a table and WHERE clause) per
// item_id and hands each item its count.
func countPerItem(t *tx, from string, args []any, set func(*Item, int), byID map[int64]*Item) error {
	rows, err := t.Query(`SELECT item_id, COUNT(*) FROM `+from+` GROUP BY item_id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return err
		}
		if it := byID[id]; it != nil {
			set(it, n)
		}
	}
	return rows.Err()
}

// ---- Comments ----

const commentCols = `id, item_id, workspace_id, author_id, body, created_at`

func scanComment(row interface{ Scan(...any) error }) (*Comment, error) {
	var c Comment
	if err := row.Scan(&c.ID, &c.ItemID, &c.WorkspaceID, &c.AuthorID, &c.Body, &c.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

// ListComments returns an item's comments, oldest first.
func (s *Store) ListComments(ctx context.Context, wsID string, itemID int64) ([]Comment, error) {
	var out []Comment
	_, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		if _, err := getItem(t, wsID, itemID); err != nil {
			return err
		}
		rows, err := t.Query(`SELECT `+commentCols+` FROM item_comments WHERE workspace_id = ? AND item_id = ? ORDER BY id`, wsID, itemID)
		if err != nil {
			return err
		}
		defer rows.Close()
		out = []Comment{}
		for rows.Next() {
			c, err := scanComment(rows)
			if err != nil {
				return err
			}
			out = append(out, *c)
		}
		return rows.Err()
	})
	return out, err
}

// AddComment adds a comment to a backlog item and records comment.added.
func (s *Store) AddComment(ctx context.Context, actor *Participant, itemID int64, body string) (*Comment, []Event, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, nil, fmt.Errorf("%w: comment body is required", ErrInvalid)
	}
	var c *Comment
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		if _, err := getItem(t, actor.WorkspaceID, itemID); err != nil {
			return err
		}
		c = &Comment{ItemID: itemID, WorkspaceID: actor.WorkspaceID, AuthorID: actor.ID, Body: body, CreatedAt: log.now}
		id, err := t.insert(`INSERT INTO item_comments (item_id, workspace_id, author_id, body, created_at) VALUES (?, ?, ?, ?, ?)`, "id",
			c.ItemID, c.WorkspaceID, c.AuthorID, c.Body, c.CreatedAt)
		if err != nil {
			return err
		}
		c.ID = id
		it, err := getItem(t, actor.WorkspaceID, itemID)
		if err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "comment.added", actor.ID, CommentEvent{Comment: c, Item: it})
	})
	if err != nil {
		return nil, nil, err
	}
	return c, evs, nil
}

// DeleteComment deletes a comment. Only its author may delete it.
func (s *Store) DeleteComment(ctx context.Context, actor *Participant, itemID, commentID int64) ([]Event, error) {
	return s.tx(ctx, func(t *tx, log *eventLog) error {
		c, err := scanComment(t.QueryRow(`SELECT `+commentCols+` FROM item_comments WHERE workspace_id = ? AND item_id = ? AND id = ?`,
			actor.WorkspaceID, itemID, commentID))
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("comment %d on #%d: %w", commentID, itemID, ErrNotFound)
		}
		if err != nil {
			return err
		}
		if c.AuthorID != actor.ID {
			return fmt.Errorf("%w: only the author can delete a comment", ErrForbidden)
		}
		if _, err := t.Exec(`DELETE FROM item_comments WHERE id = ?`, c.ID); err != nil {
			return err
		}
		it, err := getItem(t, actor.WorkspaceID, itemID)
		if err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "comment.deleted", actor.ID, CommentEvent{Comment: c, Item: it})
	})
}

// ---- PR links ----

// githubPR matches GitHub pull request URLs, e.g. in a closing note.
var githubPR = regexp.MustCompile(`https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/pull/[0-9]+`)

func validPRURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("%w: a PR link must be an http(s) URL, got %q", ErrInvalid, raw)
	}
	return raw, nil
}

// LinkPR links a pull request URL to a backlog item and records pr.linked.
// An item can have several PRs; linking the same URL twice is a conflict.
func (s *Store) LinkPR(ctx context.Context, actor *Participant, itemID int64, rawURL string) (*Item, []Event, error) {
	u, err := validPRURL(rawURL)
	if err != nil {
		return nil, nil, err
	}
	return s.mutateItem(ctx, actor, itemID, "pr.linked", func(t *tx, log *eventLog, it *Item) error {
		_, err := t.Exec(`INSERT INTO item_prs (item_id, workspace_id, url, added_by, created_at) VALUES (?, ?, ?, ?, ?)`,
			it.ID, it.WorkspaceID, u, actor.ID, log.now)
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: %s is already linked to #%d", ErrConflict, u, it.ID)
		}
		return err
	})
}

// UnlinkPR removes a pull request link from an item and records pr.unlinked.
func (s *Store) UnlinkPR(ctx context.Context, actor *Participant, itemID int64, rawURL string) (*Item, []Event, error) {
	u := strings.TrimSpace(rawURL)
	return s.mutateItem(ctx, actor, itemID, "pr.unlinked", func(t *tx, log *eventLog, it *Item) error {
		res, err := t.Exec(`DELETE FROM item_prs WHERE item_id = ? AND url = ?`, it.ID, u)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("%s is not linked to #%d: %w", u, it.ID, ErrNotFound)
		}
		return nil
	})
}

// linkPRsInNote links every GitHub PR URL found in note to it (skipping
// ones already linked), recording pr.linked for each new link.
func linkPRsInNote(t *tx, log *eventLog, actor *Participant, it *Item, note string) error {
	for _, u := range githubPR.FindAllString(note, -1) {
		var id int64
		err := t.QueryRow(`INSERT INTO item_prs (item_id, workspace_id, url, added_by, created_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (item_id, url) DO NOTHING RETURNING id`, it.ID, it.WorkspaceID, u, actor.ID, log.now).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			continue // Already linked.
		}
		if err != nil {
			return err
		}
		linked, err := getItem(t, it.WorkspaceID, it.ID)
		if err != nil {
			return err
		}
		if err := log.add(it.WorkspaceID, "pr.linked", actor.ID, linked); err != nil {
			return err
		}
	}
	return nil
}
