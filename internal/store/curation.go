package store

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// Comment is a comment on a backlog item (legacy item_comments row; new
// comments are stored as chat messages linked to the item, #916).
type Comment struct {
	ID          int64  `json:"id"`
	ItemID      int64  `json:"item_id"`
	ItemNumber  int64  `json:"item_number,omitempty"`
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

// attachExtras fills in the Needs, ReadyAt, PRs, unified thread/comment counts
// and message counts of items, all from wsID. CommentCount includes both chat
// messages linked to the item and any legacy item_comments rows (#916).
func attachExtras(t *tx, wsID string, items []*Item) error {
	if len(items) == 0 {
		return nil
	}
	byID := make(map[int64]*Item, len(items))
	for _, it := range items {
		it.Needs = []ItemNeed{}
		if it.PRs == nil {
			it.PRs = []ItemPR{}
		}
		byID[it.ID] = it
	}
	filter, nFilter, args := `workspace_id = ?`, `n.workspace_id = ?`, []any{wsID}
	if len(items) == 1 {
		filter = `workspace_id = ? AND item_id = ?`
		nFilter = `n.workspace_id = ? AND n.item_id = ?`
		args = []any{wsID, items[0].ID}
	} else if len(items) > 1 {
		marks := strings.TrimSuffix(strings.Repeat("?, ", len(items)), ", ")
		filter = `workspace_id = ? AND item_id IN (` + marks + `)`
		nFilter = `n.workspace_id = ? AND n.item_id IN (` + marks + `)`
		args = make([]any, 0, 1+len(items))
		args = append(args, wsID)
		for _, it := range items {
			args = append(args, it.ID)
		}
	}
	nrows, err := t.Query(`SELECT n.item_id, dep.id, COALESCE(dep.number, 0), dep.title, dep.status, dep.created_at, dep.closed_at
		FROM item_needs n
		JOIN items dep ON dep.id = n.needs_item_id AND dep.workspace_id = n.workspace_id
		WHERE `+nFilter+` ORDER BY n.item_id, COALESCE(dep.number, dep.id), dep.id`, args...)
	if err != nil {
		return err
	}
	readyMax := make(map[int64]int64, len(items))
	hasDoneDep := make(map[int64]bool, len(items))
	for nrows.Next() {
		var itemID, depCreated int64
		var need ItemNeed
		var depClosed sql.NullInt64
		if err := nrows.Scan(&itemID, &need.ID, &need.Number, &need.Title, &need.Status, &depCreated, &depClosed); err != nil {
			nrows.Close()
			return err
		}
		if depClosed.Valid {
			need.ClosedAt = &depClosed.Int64
		}
		if it := byID[itemID]; it != nil {
			it.Needs = append(it.Needs, need)
			if need.Status == StatusDone {
				doneAt := depCreated
				if need.ClosedAt != nil {
					doneAt = *need.ClosedAt
				}
				if !hasDoneDep[itemID] || doneAt > readyMax[itemID] {
					readyMax[itemID] = doneAt
					hasDoneDep[itemID] = true
				}
			}
		}
	}
	nrows.Close()
	if err := nrows.Err(); err != nil {
		return err
	}
	for _, it := range items {
		if it.IsBlocked() {
			it.ReadyAt = nil
		} else {
			ready := it.CreatedAt
			if hasDoneDep[it.ID] && readyMax[it.ID] > ready {
				ready = readyMax[it.ID]
			}
			it.ReadyAt = &ready
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
	return countPerItem(t, `messages WHERE item_id IS NOT NULL AND `+filter, args, func(it *Item, n int) {
		it.MessageCount = n
		it.CommentCount += n
	}, byID)
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

// ---- Comments (unified with item chat threads, #916) ----

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

// ListComments returns an item's unified thread, oldest first: any historical
// legacy rows from item_comments (marked Legacy: true) merged chronologically
// with the chat messages tied to the item (with reactions and actions).
func (s *Store) ListComments(ctx context.Context, wsID string, itemID int64) ([]Message, error) {
	t, err := s.db.beginRead(ctx)
	if err != nil {
		return nil, err
	}
	defer t.Rollback()
	it, err := getItem(t, wsID, itemID)
	if err != nil {
		return nil, err
	}
	about := &MessageItem{ID: it.ID, Number: it.DisplayNumber(), Title: it.Title, Status: it.Status, AssigneeID: it.AssigneeID}
	legacyRows, err := t.Query(`SELECT `+commentCols+` FROM item_comments WHERE workspace_id = ? AND item_id = ? ORDER BY created_at, id`, wsID, it.ID)
	if err != nil {
		return nil, err
	}
	var legacy []Message
	for legacyRows.Next() {
		c, err := scanComment(legacyRows)
		if err != nil {
			legacyRows.Close()
			return nil, err
		}
		idCopy := it.ID
		legacy = append(legacy, Message{
			ID:          c.ID,
			WorkspaceID: c.WorkspaceID,
			AuthorID:    c.AuthorID,
			Body:        c.Body,
			CreatedAt:   c.CreatedAt,
			ItemID:      &idCopy,
			ItemNumber:  it.DisplayNumber(),
			Item:        about,
			Legacy:      true,
		})
	}
	legacyRows.Close()
	if err := legacyRows.Err(); err != nil {
		return nil, err
	}

	msgRows, err := t.Query(selectMessages+` WHERE m.workspace_id = ? AND m.item_id = ? ORDER BY m.created_at, m.id`, wsID, it.ID)
	msgs, err := scanMessages(msgRows, err)
	if err != nil {
		return nil, err
	}
	if err := attachDetails(t.Query, wsID, msgs); err != nil {
		return nil, err
	}

	out := make([]Message, 0, len(legacy)+len(msgs))
	out = append(out, legacy...)
	out = append(out, msgs...)
	slices.SortStableFunc(out, func(a, b Message) int {
		if c := cmp.Compare(a.CreatedAt, b.CreatedAt); c != 0 {
			return c
		}
		if a.Legacy != b.Legacy {
			if a.Legacy {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return out, nil
}

// AddComment posts a comment on a backlog item into its unified chat thread
// (creating a root message if none exists yet, or replying in the existing
// thread) and records message.created (#916).
func (s *Store) AddComment(ctx context.Context, actor *Participant, itemID int64, body string) (*Message, []Event, error) {
	return s.PostItemComment(ctx, actor, itemID, body, false)
}

// PostItemComment posts a comment on a backlog item into its unified chat
// thread, optionally asking the @mentioned participants to act (#916).
func (s *Store) PostItemComment(ctx context.Context, actor *Participant, itemID int64, body string, actionRequired bool) (*Message, []Event, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, nil, fmt.Errorf("%w: comment body is required", ErrInvalid)
	}
	var m *Message
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		it, err := getItem(t, actor.WorkspaceID, itemID)
		if err != nil {
			return err
		}
		var rootID int64
		var parentID *int64
		err = t.QueryRow(`SELECT id FROM messages WHERE workspace_id = ? AND item_id = ? AND parent_id IS NULL ORDER BY id ASC LIMIT 1`,
			actor.WorkspaceID, it.ID).Scan(&rootID)
		if err == nil {
			parentID = &rootID
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		idCopy := it.ID
		itemNum := it.DisplayNumber()
		about := &MessageItem{ID: it.ID, Number: itemNum, Title: it.Title, Status: it.Status, AssigneeID: it.AssigneeID}
		id, err := t.insert(`INSERT INTO messages (workspace_id, author_id, parent_id, body, created_at, item_id) VALUES (?, ?, ?, ?, ?, ?)`, "id",
			actor.WorkspaceID, actor.ID, parentID, body, log.now, idCopy)
		if err != nil {
			return err
		}
		m = &Message{
			ID:          id,
			WorkspaceID: actor.WorkspaceID,
			AuthorID:    actor.ID,
			ParentID:    parentID,
			Body:        body,
			CreatedAt:   log.now,
			ItemID:      &idCopy,
			ItemNumber:  itemNum,
			Item:        about,
		}
		if actionRequired {
			if err := addActions(t, m); err != nil {
				return err
			}
		}
		if parentID != nil {
			if err := markDoneIfAsked(t, actor, *parentID, log.now); err != nil {
				return err
			}
		}
		return log.add(actor.WorkspaceID, "message.created", actor.ID, m)
	})
	if err != nil {
		return nil, nil, err
	}
	return m, evs, nil
}

// DeleteComment deletes an item's comment (either a chat message tied to the
// item or a legacy item_comments row). Only its author may delete it.
func (s *Store) DeleteComment(ctx context.Context, actor *Participant, itemID, commentID int64) ([]Event, error) {
	return s.deleteComment(ctx, actor, itemID, commentID, false)
}

// DeleteLegacyComment deletes a historical row from item_comments directly,
// disambiguating when an item has both a legacy comment and a chat message
// sharing the same numeric ID.
func (s *Store) DeleteLegacyComment(ctx context.Context, actor *Participant, itemID, commentID int64) ([]Event, error) {
	return s.deleteComment(ctx, actor, itemID, commentID, true)
}

func (s *Store) deleteComment(ctx context.Context, actor *Participant, itemID, commentID int64, legacyOnly bool) ([]Event, error) {
	return s.tx(ctx, func(t *tx, log *eventLog) error {
		realItemID, err := ResolveItemID(t, actor.WorkspaceID, itemID)
		if err != nil {
			return fmt.Errorf("comment %d on #%d: %w", commentID, itemID, ErrNotFound)
		}
		if !legacyOnly {
			m, err := scanMessage(t.QueryRow(selectMessages+` WHERE m.workspace_id = ? AND m.item_id = ? AND m.id = ?`,
				actor.WorkspaceID, realItemID, commentID))
			if err == nil {
				if m.AuthorID != actor.ID {
					return fmt.Errorf("%w: only the author can delete a comment", ErrForbidden)
				}
				if err := deleteItemMessage(t, log, actor, m); err != nil {
					return err
				}
				return log.add(actor.WorkspaceID, "message.deleted", actor.ID, m)
			}
			if !errors.Is(err, ErrNotFound) {
				return err
			}
		}
		c, err := scanComment(t.QueryRow(`SELECT `+commentCols+` FROM item_comments WHERE workspace_id = ? AND item_id = ? AND id = ?`,
			actor.WorkspaceID, realItemID, commentID))
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
		it, err := getItemByID(t, actor.WorkspaceID, realItemID)
		if err != nil {
			return err
		}
		c.ItemNumber = it.DisplayNumber()
		return log.add(actor.WorkspaceID, "comment.deleted", actor.ID, CommentEvent{Comment: c, Item: it})
	})
}

// deleteItemMessage removes a chat message tied to an item, promoting its
// earliest reply to thread root (and reparenting any other replies) if it was
// a thread root with replies.
func deleteItemMessage(t *tx, log *eventLog, actor *Participant, m *Message) error {
	if m.ParentID == nil {
		rows, err := t.Query(`SELECT id FROM messages WHERE workspace_id = ? AND parent_id = ? ORDER BY id ASC`, actor.WorkspaceID, m.ID)
		if err != nil {
			return err
		}
		var replyIDs []int64
		for rows.Next() {
			var rid int64
			if err := rows.Scan(&rid); err != nil {
				rows.Close()
				return err
			}
			replyIDs = append(replyIDs, rid)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(replyIDs) > 0 {
			newRoot := replyIDs[0]
			if _, err := t.Exec(`UPDATE messages SET parent_id = NULL WHERE id = ?`, newRoot); err != nil {
				return err
			}
			if len(replyIDs) > 1 {
				if _, err := t.Exec(`UPDATE messages SET parent_id = ? WHERE workspace_id = ? AND parent_id = ?`, newRoot, actor.WorkspaceID, m.ID); err != nil {
					return err
				}
			}
			updatedRows, err := t.Query(selectMessages+` WHERE m.workspace_id = ? AND (m.id = ? OR m.parent_id = ?) ORDER BY m.id ASC`, actor.WorkspaceID, newRoot, newRoot)
			updated, err := scanMessages(updatedRows, err)
			if err != nil {
				return err
			}
			if err := attachDetails(t.Query, actor.WorkspaceID, updated); err != nil {
				return err
			}
			for _, u := range updated {
				if err := log.add(actor.WorkspaceID, "message.updated", actor.ID, u); err != nil {
					return err
				}
			}
		}
	}
	if _, err := t.Exec(`DELETE FROM reactions WHERE message_id = ?`, m.ID); err != nil {
		return err
	}
	if _, err := t.Exec(`DELETE FROM message_actions WHERE message_id = ?`, m.ID); err != nil {
		return err
	}
	_, err := t.Exec(`DELETE FROM messages WHERE id = ?`, m.ID)
	return err
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
			return fmt.Errorf("%w: %s is already linked to #%d", ErrConflict, u, it.DisplayNumber())
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
			return fmt.Errorf("%s is not linked to #%d: %w", u, it.DisplayNumber(), ErrNotFound)
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
		linked, err := getItemByID(t, it.WorkspaceID, it.ID)
		if err != nil {
			return err
		}
		if err := log.add(it.WorkspaceID, "pr.linked", actor.ID, linked); err != nil {
			return err
		}
	}
	return nil
}

// ---- Needs (item dependencies, #921) ----

// ListItemNeeds returns an item's prerequisite items in deterministic order.
func (s *Store) ListItemNeeds(ctx context.Context, wsID string, itemRef int64) ([]ItemNeed, []Event, error) {
	it, evs, err := s.GetItem(ctx, wsID, itemRef)
	if err != nil {
		return nil, nil, err
	}
	return it.Needs, evs, nil
}

// AddItemNeeds links one or more prerequisite items (by per-workspace number
// or legacy ID) to itemRef and records backlog.needs_updated when new
// prerequisites are added. Linking an already-linked prerequisite is
// idempotent; self-dependencies are ErrInvalid and dependency cycles are
// ErrConflict.
func (s *Store) AddItemNeeds(ctx context.Context, actor *Participant, itemRef int64, needRefs ...int64) (*Item, []Event, error) {
	if len(needRefs) == 0 {
		return nil, nil, fmt.Errorf("%w: prerequisite item id is required", ErrInvalid)
	}
	for _, nref := range needRefs {
		if nref <= 0 {
			return nil, nil, fmt.Errorf("%w: prerequisite item id must be positive", ErrInvalid)
		}
	}

	var it *Item
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		if err := expireLeases(t, log, actor.WorkspaceID); err != nil {
			return err
		}
		var err error
		if it, err = getItem(t, actor.WorkspaceID, itemRef); err != nil {
			return err
		}
		seen := make(map[int64]bool, len(needRefs))
		unique := make([]int64, 0, len(needRefs))
		for _, nref := range needRefs {
			nid, err := ResolveItemID(t, actor.WorkspaceID, nref)
			if err != nil {
				return err
			}
			if nid == it.ID {
				return fmt.Errorf("%w: item #%d cannot depend on itself", ErrInvalid, it.DisplayNumber())
			}
			if !seen[nid] {
				seen[nid] = true
				unique = append(unique, nid)
			}
		}

		rows, err := t.Query(`SELECT item_id, needs_item_id FROM item_needs WHERE workspace_id = ?`, actor.WorkspaceID)
		if err != nil {
			return err
		}
		adj := make(map[int64][]int64)
		for rows.Next() {
			var from, to int64
			if err := rows.Scan(&from, &to); err != nil {
				rows.Close()
				return err
			}
			adj[from] = append(adj[from], to)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		for _, nid := range unique {
			if reachable(adj, nid, it.ID) {
				dep, err := getItemByID(t, actor.WorkspaceID, nid)
				if err != nil {
					return err
				}
				return fmt.Errorf("%w: dependency cycle: #%d already depends on #%d", ErrConflict, dep.DisplayNumber(), it.DisplayNumber())
			}
			adj[it.ID] = append(adj[it.ID], nid)
		}

		var inserted int64
		for _, nid := range unique {
			res, err := t.Exec(`INSERT INTO item_needs (workspace_id, item_id, needs_item_id, created_at) VALUES (?, ?, ?, ?)
				ON CONFLICT (item_id, needs_item_id) DO NOTHING`, actor.WorkspaceID, it.ID, nid, log.now)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				inserted += n
			}
		}
		if inserted > 0 {
			if _, err := t.Exec(`UPDATE items SET updated_at = ? WHERE id = ?`, log.now, it.ID); err != nil {
				return err
			}
		}
		if it, err = getItemByID(t, actor.WorkspaceID, it.ID); err != nil {
			return err
		}
		if inserted > 0 {
			return log.add(actor.WorkspaceID, "backlog.needs_updated", actor.ID, it)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return it, evs, nil
}

// RemoveItemNeed unlinks a prerequisite item from itemRef and records
// backlog.needs_updated.
func (s *Store) RemoveItemNeed(ctx context.Context, actor *Participant, itemRef, needRef int64) (*Item, []Event, error) {
	if needRef <= 0 {
		return nil, nil, fmt.Errorf("%w: prerequisite item id must be positive", ErrInvalid)
	}
	return s.mutateItem(ctx, actor, itemRef, "backlog.needs_updated", func(t *tx, log *eventLog, it *Item) error {
		needID, err := ResolveItemID(t, it.WorkspaceID, needRef)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return fmt.Errorf("#%d is not a prerequisite of #%d: %w", needRef, it.DisplayNumber(), ErrNotFound)
			}
			return err
		}
		res, err := t.Exec(`DELETE FROM item_needs WHERE workspace_id = ? AND item_id = ? AND needs_item_id = ?`,
			it.WorkspaceID, it.ID, needID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			dep, derr := getItemByID(t, it.WorkspaceID, needID)
			shown := needRef
			if derr == nil {
				shown = dep.DisplayNumber()
			}
			return fmt.Errorf("#%d is not a prerequisite of #%d: %w", shown, it.DisplayNumber(), ErrNotFound)
		}
		_, err = t.Exec(`UPDATE items SET updated_at = ? WHERE id = ?`, log.now, it.ID)
		return err
	})
}

func reachable(adj map[int64][]int64, start, target int64) bool {
	if start == target {
		return true
	}
	seen := map[int64]bool{start: true}
	queue := []int64{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range adj[cur] {
			if next == target {
				return true
			}
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	return false
}
