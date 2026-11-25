package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Todo statuses and list filter values (#915).
const (
	TodoStatusOpen = "open"
	TodoStatusDone = "done"

	TodoKindSectionItem = "section_item"

	TodoFilterActive  = "active"  // open and not currently snoozed
	TodoFilterSnoozed = "snoozed" // open and currently snoozed
	TodoFilterOpen    = "open"    // all open (active + snoozed)
	TodoFilterDone    = "done"
	TodoFilterAll     = "all"
)

// Snooze durations (#915).
const (
	Snooze1Day  = "1d"
	Snooze1Week = "1w"
	SnoozeNone  = "none"
)

// TodoItem summarizes the backlog item a TODO is linked to.
type TodoItem struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// Todo is an actionable task assigned to a human participant in a workspace (#915).
type Todo struct {
	ID            int64     `json:"id"`
	WorkspaceID   string    `json:"workspace_id"`
	Title         string    `json:"title"`
	Body          string    `json:"body"`
	CreatorID     *int64    `json:"creator_id,omitempty"`
	CreatorName   string    `json:"creator_name,omitempty"`
	CreatorEmoji  string    `json:"creator_emoji,omitempty"`
	AssigneeID    int64     `json:"assignee_id"`
	AssigneeName  string    `json:"assignee_name,omitempty"`
	AssigneeEmoji string    `json:"assignee_emoji,omitempty"`
	ItemID        *int64    `json:"item_id,omitempty"`
	Item          *TodoItem `json:"item,omitempty"`
	PRURL         string    `json:"pr_url,omitempty"`
	Status        string    `json:"status"`
	SnoozedUntil  *int64    `json:"snoozed_until,omitempty"`
	DoneNote      string    `json:"done_note,omitempty"`
	DoneByID      *int64    `json:"done_by_id,omitempty"`
	DoneByName    string    `json:"done_by_name,omitempty"`
	DoneByEmoji   string    `json:"done_by_emoji,omitempty"`
	CreatedAt     int64     `json:"created_at"`
	DoneAt        *int64    `json:"done_at,omitempty"`

	// Surfaced notify-section item fields (#919); omitted on regular TODOs.
	Kind           string   `json:"kind,omitempty"`
	SectionID      *int64   `json:"section_id,omitempty"`
	SectionSlug    string   `json:"section_slug,omitempty"`
	SectionName    string   `json:"section_name,omitempty"`
	SectionIcon    string   `json:"section_icon,omitempty"`
	Choices        []string `json:"choices,omitempty"`
	CloseOnAnswer  bool     `json:"close_on_answer,omitempty"`
	AnsweredChoice string   `json:"answered_choice,omitempty"`
	AnsweredByID   *int64   `json:"answered_by_id,omitempty"`
	AnsweredByName string   `json:"answered_by_name,omitempty"`
	AnsweredAt     *int64   `json:"answered_at,omitempty"`
}

// UserTodo is a cross-workspace TODO returned by ListUserTodos (GET /api/me/todos).
type UserTodo struct {
	Todo
	WorkspaceName string `json:"workspace_name"`
}

// TodoInput holds the fields for creating a TODO. Either AssigneeID or
// Assignee (participant name in the workspace) must name a human participant.
type TodoInput struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	AssigneeID int64  `json:"assignee_id"`
	Assignee   string `json:"assignee"`
	ItemID     *int64 `json:"item_id"`
	PRURL      string `json:"pr_url"`
}

// TodoPatch holds optional updates to a TODO.
type TodoPatch struct {
	Title      *string `json:"title"`
	Body       *string `json:"body"`
	AssigneeID *int64  `json:"assignee_id"`
	ItemID     *int64  `json:"item_id"` // 0 clears the item link
	PRURL      *string `json:"pr_url"`  // "" clears the PR link
}

const selectTodos = `SELECT t.id, t.workspace_id, t.title, t.body, t.creator_id, t.assignee_id,
	t.item_id, t.pr_url, t.status, t.snoozed_until, t.done_note, t.done_by_id,
	t.created_at, t.done_at,
	ap.name, ap.emoji,
	cp.name, cp.emoji,
	dp.name, dp.emoji,
	i.title, i.status
	FROM todos t
	JOIN participants ap ON ap.id = t.assignee_id
	LEFT JOIN participants cp ON cp.id = t.creator_id
	LEFT JOIN participants dp ON dp.id = t.done_by_id
	LEFT JOIN items i ON i.id = t.item_id`

func scanTodo(row interface{ Scan(...any) error }, extra ...any) (*Todo, error) {
	var td Todo
	var creatorID, itemID, snoozedUntil, doneByID, doneAt sql.NullInt64
	var creatorName, creatorEmoji, doneByName, doneByEmoji, itemTitle, itemStatus sql.NullString
	dest := append([]any{
		&td.ID, &td.WorkspaceID, &td.Title, &td.Body, &creatorID, &td.AssigneeID,
		&itemID, &td.PRURL, &td.Status, &snoozedUntil, &td.DoneNote, &doneByID,
		&td.CreatedAt, &doneAt,
		&td.AssigneeName, &td.AssigneeEmoji,
		&creatorName, &creatorEmoji,
		&doneByName, &doneByEmoji,
		&itemTitle, &itemStatus,
	}, extra...)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if creatorID.Valid {
		td.CreatorID = &creatorID.Int64
		td.CreatorName = creatorName.String
		td.CreatorEmoji = creatorEmoji.String
	}
	if itemID.Valid {
		td.ItemID = &itemID.Int64
		td.Item = &TodoItem{ID: itemID.Int64, Title: itemTitle.String, Status: itemStatus.String}
	}
	if snoozedUntil.Valid {
		td.SnoozedUntil = &snoozedUntil.Int64
	}
	if doneByID.Valid {
		td.DoneByID = &doneByID.Int64
		td.DoneByName = doneByName.String
		td.DoneByEmoji = doneByEmoji.String
	}
	if doneAt.Valid {
		td.DoneAt = &doneAt.Int64
	}
	return &td, nil
}

func scanTodos(rows *sql.Rows, err error) ([]Todo, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Todo{}
	for rows.Next() {
		td, err := scanTodo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *td)
	}
	return out, rows.Err()
}

func getTodoTx(t *tx, wsID string, id int64) (*Todo, error) {
	td, err := scanTodo(t.QueryRow(selectTodos+` WHERE t.workspace_id = ? AND t.id = ?`, wsID, id))
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("todo #%d: %w", id, ErrNotFound)
	}
	return td, err
}

// NormalizePRURL validates and normalizes an optional pull request URL.
// GitHub pull request URLs are normalized to https://github.com/<owner>/<repo>/pull/<n>.
func NormalizePRURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("%w: a PR link must be an http(s) URL, got %q", ErrInvalid, raw)
	}
	if strings.EqualFold(u.Host, "github.com") {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 4 || parts[0] == "" || parts[1] == "" || parts[2] != "pull" {
			return "", fmt.Errorf("%w: GitHub PR URL must look like https://github.com/<owner>/<repo>/pull/<number>, got %q", ErrInvalid, raw)
		}
		num, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil || num <= 0 {
			return "", fmt.Errorf("%w: GitHub PR URL must have a positive pull request number, got %q", ErrInvalid, raw)
		}
		return fmt.Sprintf("https://github.com/%s/%s/pull/%d", parts[0], strings.TrimSuffix(parts[1], ".git"), num), nil
	}
	return raw, nil
}

// resolveHumanAssignee resolves an assignee ID or name in wsID and checks that
// the assignee is a human participant (not a pass-joined agent or deleted account).
func resolveHumanAssignee(t *tx, wsID string, assigneeID int64, assigneeName string) (*Participant, error) {
	assigneeName = strings.TrimPrefix(strings.TrimSpace(assigneeName), "@")
	if assigneeID <= 0 && assigneeName == "" {
		return nil, fmt.Errorf("%w: assignee is required", ErrInvalid)
	}
	var p *Participant
	var err error
	if assigneeID > 0 {
		p, err = scanParticipant(t.QueryRow(`SELECT `+participantCols+` FROM participants WHERE workspace_id = ? AND id = ?`, wsID, assigneeID))
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: participant %d is not in this workspace", ErrInvalid, assigneeID)
		}
	} else {
		p, err = scanParticipant(t.QueryRow(`SELECT `+participantCols+` FROM participants WHERE workspace_id = ? AND lower(name) = lower(?)`, wsID, assigneeName))
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: no participant named %q in this workspace", ErrInvalid, assigneeName)
		}
	}
	if err != nil {
		return nil, err
	}
	if p.Sponsor != nil {
		return nil, fmt.Errorf("%w: TODOs are for humans (%s is an agent; assign backlog items or @mention agents instead)", ErrInvalid, p.Name)
	}
	if p.RevokedAt != 0 {
		return nil, fmt.Errorf("%w: cannot assign a TODO to %s (inactive account)", ErrInvalid, p.Name)
	}
	return p, nil
}

// ResolveParticipantID looks up a participant by name in wsID.
func (s *Store) ResolveParticipantID(ctx context.Context, wsID, name string) (int64, error) {
	name = strings.TrimPrefix(strings.TrimSpace(name), "@")
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM participants WHERE workspace_id = ? AND lower(name) = lower(?)`, wsID, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("%w: no participant named %q in this workspace", ErrInvalid, name)
	}
	return id, err
}

// CreateTodo creates an open TODO assigned to a human participant in actor's
// workspace, optionally linked to a backlog item and/or pull request URL, and
// records todo.created.
func (s *Store) CreateTodo(ctx context.Context, actor *Participant, in TodoInput) (*Todo, []Event, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, nil, fmt.Errorf("%w: TODO title is required", ErrInvalid)
	}
	prURL, err := NormalizePRURL(in.PRURL)
	if err != nil {
		return nil, nil, err
	}
	var itemID *int64
	if in.ItemID != nil && *in.ItemID != 0 {
		if *in.ItemID < 0 {
			return nil, nil, fmt.Errorf("%w: item_id must be positive", ErrInvalid)
		}
		id := *in.ItemID
		itemID = &id
	}
	var td *Todo
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		assignee, err := resolveHumanAssignee(t, actor.WorkspaceID, in.AssigneeID, in.Assignee)
		if err != nil {
			return err
		}
		if itemID != nil {
			if _, err := getItem(t, actor.WorkspaceID, *itemID); errors.Is(err, ErrNotFound) {
				return fmt.Errorf("%w: there is no backlog item #%d in this workspace", ErrInvalid, *itemID)
			} else if err != nil {
				return err
			}
		}
		id, err := t.insert(`INSERT INTO todos (workspace_id, title, body, creator_id, assignee_id, item_id, pr_url, status, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, "id",
			actor.WorkspaceID, title, strings.TrimSpace(in.Body), actor.ID, assignee.ID, itemID, prURL, TodoStatusOpen, log.now)
		if err != nil {
			return err
		}
		td, err = getTodoTx(t, actor.WorkspaceID, id)
		if err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "todo.created", actor.ID, td)
	})
	if err != nil {
		return nil, nil, err
	}
	return td, evs, nil
}

// UpdateTodo updates fields of a TODO and records todo.updated.
func (s *Store) UpdateTodo(ctx context.Context, actor *Participant, id int64, p TodoPatch) (*Todo, []Event, error) {
	var td *Todo
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		cur, err := getTodoTx(t, actor.WorkspaceID, id)
		if err != nil {
			return err
		}
		title := cur.Title
		if p.Title != nil {
			title = strings.TrimSpace(*p.Title)
			if title == "" {
				return fmt.Errorf("%w: TODO title cannot be empty", ErrInvalid)
			}
		}
		body := cur.Body
		if p.Body != nil {
			body = strings.TrimSpace(*p.Body)
		}
		assigneeID := cur.AssigneeID
		if p.AssigneeID != nil {
			assignee, err := resolveHumanAssignee(t, actor.WorkspaceID, *p.AssigneeID, "")
			if err != nil {
				return err
			}
			assigneeID = assignee.ID
		}
		itemID := cur.ItemID
		if p.ItemID != nil {
			if *p.ItemID == 0 {
				itemID = nil
			} else if *p.ItemID < 0 {
				return fmt.Errorf("%w: item_id must be >= 0", ErrInvalid)
			} else {
				if _, err := getItem(t, actor.WorkspaceID, *p.ItemID); errors.Is(err, ErrNotFound) {
					return fmt.Errorf("%w: there is no backlog item #%d in this workspace", ErrInvalid, *p.ItemID)
				} else if err != nil {
					return err
				}
				v := *p.ItemID
				itemID = &v
			}
		}
		prURL := cur.PRURL
		if p.PRURL != nil {
			if prURL, err = NormalizePRURL(*p.PRURL); err != nil {
				return err
			}
		}
		if _, err := t.Exec(`UPDATE todos SET title = ?, body = ?, assignee_id = ?, item_id = ?, pr_url = ? WHERE id = ?`,
			title, body, assigneeID, itemID, prURL, cur.ID); err != nil {
			return err
		}
		td, err = getTodoTx(t, actor.WorkspaceID, cur.ID)
		if err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "todo.updated", actor.ID, td)
	})
	if err != nil {
		return nil, nil, err
	}
	return td, evs, nil
}

func todoStatusWhere(status string, now int64) (string, []any, error) {
	switch status {
	case "", TodoFilterActive:
		return ` AND t.status = ? AND (t.snoozed_until IS NULL OR t.snoozed_until <= ?)`, []any{TodoStatusOpen, now}, nil
	case TodoFilterSnoozed:
		return ` AND t.status = ? AND t.snoozed_until IS NOT NULL AND t.snoozed_until > ?`, []any{TodoStatusOpen, now}, nil
	case TodoFilterOpen:
		return ` AND t.status = ?`, []any{TodoStatusOpen}, nil
	case TodoFilterDone:
		return ` AND t.status = ?`, []any{TodoStatusDone}, nil
	case TodoFilterAll:
		return ``, nil, nil
	default:
		return "", nil, fmt.Errorf("%w: unknown status %q (want active, snoozed, open, done or all)", ErrInvalid, status)
	}
}

const selectNotifyItems = `SELECT i.id, i.workspace_id, i.title, i.body, i.created_by, i.assignee_id,
	i.status, i.close_note, i.created_at, i.closed_at,
	s.id, s.slug, s.name, s.icon,
	i.choices_json, i.close_on_answer, i.answered_choice, i.answered_by_id, i.answered_at,
	ap.name, ap.emoji,
	cp.name, cp.emoji,
	COALESCE(ab.name, ''),
	COALESCE((SELECT p.url FROM item_prs p WHERE p.item_id = i.id ORDER BY p.id LIMIT 1), '')`

func scanNotifyTodo(row interface{ Scan(...any) error }, extra ...any) (*Todo, error) {
	var td Todo
	var createdBy int64
	var itStatus, choicesJSON string
	var closedAt, answeredByID, answeredAt sql.NullInt64
	var creatorName, creatorEmoji sql.NullString
	var secID int64
	dest := append([]any{
		&td.ID, &td.WorkspaceID, &td.Title, &td.Body, &createdBy, &td.AssigneeID,
		&itStatus, &td.DoneNote, &td.CreatedAt, &closedAt,
		&secID, &td.SectionSlug, &td.SectionName, &td.SectionIcon,
		&choicesJSON, &td.CloseOnAnswer, &td.AnsweredChoice, &answeredByID, &answeredAt,
		&td.AssigneeName, &td.AssigneeEmoji,
		&creatorName, &creatorEmoji,
		&td.AnsweredByName,
		&td.PRURL,
	}, extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	td.Kind = TodoKindSectionItem
	td.CreatorID = &createdBy
	td.CreatorName = creatorName.String
	td.CreatorEmoji = creatorEmoji.String
	itemID := td.ID
	td.ItemID = &itemID
	td.Item = &TodoItem{ID: itemID, Title: td.Title, Status: itStatus}
	if itStatus == StatusDone {
		td.Status = TodoStatusDone
	} else {
		td.Status = TodoStatusOpen
	}
	if closedAt.Valid {
		td.DoneAt = &closedAt.Int64
	}
	td.SectionID = &secID
	if choicesJSON != "" && choicesJSON != "[]" {
		var choices []string
		if err := json.Unmarshal([]byte(choicesJSON), &choices); err == nil && len(choices) > 0 {
			td.Choices = choices
		}
	}
	if answeredByID.Valid {
		td.AnsweredByID = &answeredByID.Int64
	}
	if answeredAt.Valid {
		td.AnsweredAt = &answeredAt.Int64
	}
	return &td, nil
}

func notifyItemStatusWhere(status string) (clause string, args []any, include bool) {
	switch status {
	case "", TodoFilterActive, TodoFilterOpen:
		return ` AND i.status != ?`, []any{StatusDone}, true
	case TodoFilterDone:
		return ` AND i.status = ?`, []any{StatusDone}, true
	case TodoFilterAll:
		return ``, nil, true
	default:
		return ``, nil, false
	}
}

func todoSortLess(a, b Todo) bool {
	aDone := a.Status == TodoStatusDone
	bDone := b.Status == TodoStatusDone
	if aDone != bDone {
		return !aDone
	}
	aTime := a.CreatedAt
	if a.DoneAt != nil {
		aTime = *a.DoneAt
	}
	bTime := b.CreatedAt
	if b.DoneAt != nil {
		bTime = *b.DoneAt
	}
	if aTime != bTime {
		return aTime > bTime
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.ID > b.ID
}

// ListTodos lists TODOs in wsID matching status ("active" [default: open and
// not currently snoozed], "snoozed", "open", "done", or "all"), optionally
// narrowed to assigneeID (> 0), plus human-assigned items in notify=true
// custom sections (#919).
func (s *Store) ListTodos(ctx context.Context, wsID, status string, assigneeID int64) ([]Todo, error) {
	clause, statusArgs, err := todoStatusWhere(status, s.nowMillis())
	if err != nil {
		return nil, err
	}
	query := selectTodos + ` WHERE t.workspace_id = ?` + clause
	args := append([]any{wsID}, statusArgs...)
	if assigneeID > 0 {
		query += ` AND t.assignee_id = ?`
		args = append(args, assigneeID)
	}
	query += ` ORDER BY t.status = 'done', COALESCE(t.done_at, t.created_at) DESC, t.id DESC`
	out, err := scanTodos(s.db.QueryContext(ctx, query, args...))
	if err != nil {
		return nil, err
	}

	nClause, nArgs, include := notifyItemStatusWhere(status)
	if !include {
		return out, nil
	}
	nq := selectNotifyItems + `
		FROM items i
		JOIN sections s ON s.id = i.section_id
		JOIN participants ap ON ap.id = i.assignee_id
		LEFT JOIN participants cp ON cp.id = i.created_by
		LEFT JOIN participants ab ON ab.id = i.answered_by_id
		WHERE i.workspace_id = ? AND s.notify = ?
		  AND ap.sponsor_user_id IS NULL AND COALESCE(ap.revoked_at, 0) = 0` + nClause
	qArgs := append([]any{wsID, true}, nArgs...)
	if assigneeID > 0 {
		nq += ` AND i.assignee_id = ?`
		qArgs = append(qArgs, assigneeID)
	}
	rows, err := s.db.QueryContext(ctx, nq, qArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	added := false
	for rows.Next() {
		td, err := scanNotifyTodo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *td)
		added = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if added {
		sort.Slice(out, func(i, j int) bool { return todoSortLess(out[i], out[j]) })
	}
	return out, nil
}

// ListUserTodos lists TODOs (and notify=true section items, #919) assigned to
// userID's human participants across all workspaces userID is currently a
// member of, filtered by status ("active" by default).
func (s *Store) ListUserTodos(ctx context.Context, userID int64, status string) ([]UserTodo, error) {
	clause, statusArgs, err := todoStatusWhere(status, s.nowMillis())
	if err != nil {
		return nil, err
	}
	query := `SELECT t.id, t.workspace_id, t.title, t.body, t.creator_id, t.assignee_id,
		t.item_id, t.pr_url, t.status, t.snoozed_until, t.done_note, t.done_by_id,
		t.created_at, t.done_at,
		ap.name, ap.emoji,
		cp.name, cp.emoji,
		dp.name, dp.emoji,
		i.title, i.status,
		w.name
		FROM todos t
		JOIN workspaces w ON w.id = t.workspace_id
		JOIN participants ap ON ap.id = t.assignee_id
		LEFT JOIN participants cp ON cp.id = t.creator_id
		LEFT JOIN participants dp ON dp.id = t.done_by_id
		LEFT JOIN items i ON i.id = t.item_id
		WHERE ap.user_id = ?
		  AND EXISTS (SELECT 1 FROM memberships m WHERE m.workspace_id = t.workspace_id AND m.user_id = ?)` +
		clause +
		` ORDER BY t.status = 'done', COALESCE(t.done_at, t.created_at) DESC, t.id DESC`
	args := append([]any{userID, userID}, statusArgs...)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out := []UserTodo{}
	for rows.Next() {
		var wsName string
		td, err := scanTodo(rows, &wsName)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, UserTodo{Todo: *td, WorkspaceName: wsName})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	nClause, nArgs, include := notifyItemStatusWhere(status)
	if !include {
		return out, nil
	}
	nq := selectNotifyItems + `, w.name
		FROM items i
		JOIN sections s ON s.id = i.section_id
		JOIN workspaces w ON w.id = i.workspace_id
		JOIN participants ap ON ap.id = i.assignee_id
		LEFT JOIN participants cp ON cp.id = i.created_by
		LEFT JOIN participants ab ON ab.id = i.answered_by_id
		WHERE ap.user_id = ? AND ap.sponsor_user_id IS NULL AND COALESCE(ap.revoked_at, 0) = 0
		  AND s.notify = ?
		  AND EXISTS (SELECT 1 FROM memberships m WHERE m.workspace_id = i.workspace_id AND m.user_id = ?)` + nClause
	qArgs := append([]any{userID, true, userID}, nArgs...)
	nrows, err := s.db.QueryContext(ctx, nq, qArgs...)
	if err != nil {
		return nil, err
	}
	defer nrows.Close()
	added := false
	for nrows.Next() {
		var wsName string
		td, err := scanNotifyTodo(nrows, &wsName)
		if err != nil {
			return nil, err
		}
		out = append(out, UserTodo{Todo: *td, WorkspaceName: wsName})
		added = true
	}
	if err := nrows.Err(); err != nil {
		return nil, err
	}
	if added {
		sort.Slice(out, func(i, j int) bool { return todoSortLess(out[i].Todo, out[j].Todo) })
	}
	return out, nil
}

// CountOpenTodos returns the number of active (open and not currently snoozed)
// TODOs in actor's workspace assigned to actor, plus (for human participants)
// non-done items in notify=true sections assigned to actor (#919).
func (s *Store) CountOpenTodos(ctx context.Context, actor *Participant) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM todos
		WHERE workspace_id = ? AND assignee_id = ? AND status = ?
		  AND (snoozed_until IS NULL OR snoozed_until <= ?)`,
		actor.WorkspaceID, actor.ID, TodoStatusOpen, s.nowMillis()).Scan(&n); err != nil {
		return 0, err
	}
	if actor.Sponsor == nil && actor.RevokedAt == 0 {
		var secItems int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM items i
			JOIN sections s ON s.id = i.section_id
			WHERE i.workspace_id = ? AND i.assignee_id = ? AND i.status != ? AND s.notify = ?`,
			actor.WorkspaceID, actor.ID, StatusDone, true).Scan(&secItems); err != nil {
			return 0, err
		}
		n += secItems
	}
	return n, nil
}

// DoneTodo marks a TODO done with an optional completion note. Agents
// (actor.Sponsor != nil) must provide a non-empty note.
func (s *Store) DoneTodo(ctx context.Context, actor *Participant, id int64, note string) (*Todo, []Event, error) {
	note = strings.TrimSpace(note)
	if actor.Sponsor != nil && note == "" {
		return nil, nil, fmt.Errorf("%w: agents must provide a completion --note when marking a TODO done", ErrInvalid)
	}
	var td *Todo
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		cur, err := getTodoTx(t, actor.WorkspaceID, id)
		if err != nil {
			return err
		}
		if cur.Status == TodoStatusDone {
			return fmt.Errorf("%w: TODO #%d is already done", ErrConflict, id)
		}
		if _, err := t.Exec(`UPDATE todos SET status = ?, done_note = ?, done_by_id = ?, done_at = ?, snoozed_until = NULL WHERE id = ?`,
			TodoStatusDone, note, actor.ID, log.now, id); err != nil {
			return err
		}
		td, err = getTodoTx(t, actor.WorkspaceID, id)
		if err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "todo.done", actor.ID, td)
	})
	if err != nil {
		return nil, nil, err
	}
	return td, evs, nil
}

// ReopenTodo reopens a completed TODO and records todo.reopened.
func (s *Store) ReopenTodo(ctx context.Context, actor *Participant, id int64) (*Todo, []Event, error) {
	var td *Todo
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		cur, err := getTodoTx(t, actor.WorkspaceID, id)
		if err != nil {
			return err
		}
		if cur.Status != TodoStatusDone {
			return fmt.Errorf("%w: TODO #%d is not done", ErrConflict, id)
		}
		if _, err := t.Exec(`UPDATE todos SET status = ?, done_note = '', done_by_id = NULL, done_at = NULL, snoozed_until = NULL WHERE id = ?`,
			TodoStatusOpen, id); err != nil {
			return err
		}
		td, err = getTodoTx(t, actor.WorkspaceID, id)
		if err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "todo.reopened", actor.ID, td)
	})
	if err != nil {
		return nil, nil, err
	}
	return td, evs, nil
}

func parseSnoozeDuration(duration string, now int64) (*int64, error) {
	switch strings.ToLower(strings.TrimSpace(duration)) {
	case Snooze1Day:
		until := now + (24 * time.Hour).Milliseconds()
		return &until, nil
	case Snooze1Week:
		until := now + (7 * 24 * time.Hour).Milliseconds()
		return &until, nil
	case SnoozeNone, "off", "clear", "0", "":
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: snooze duration must be 1d, 1w or none", ErrInvalid)
	}
}

// SnoozeTodo snoozes an open TODO for 1d (24h), 1w (7d), or clears its snooze
// ("none"/"off"/"clear"/"0"), recording todo.snoozed.
func (s *Store) SnoozeTodo(ctx context.Context, actor *Participant, id int64, duration string) (*Todo, []Event, error) {
	var td *Todo
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		until, err := parseSnoozeDuration(duration, log.now)
		if err != nil {
			return err
		}
		cur, err := getTodoTx(t, actor.WorkspaceID, id)
		if err != nil {
			return err
		}
		if cur.Status == TodoStatusDone {
			return fmt.Errorf("%w: TODO #%d is done; reopen it first", ErrConflict, id)
		}
		if _, err := t.Exec(`UPDATE todos SET snoozed_until = ? WHERE id = ?`, until, id); err != nil {
			return err
		}
		td, err = getTodoTx(t, actor.WorkspaceID, id)
		if err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "todo.snoozed", actor.ID, td)
	})
	if err != nil {
		return nil, nil, err
	}
	return td, evs, nil
}

// DeleteTodo removes a TODO and records todo.deleted.
func (s *Store) DeleteTodo(ctx context.Context, actor *Participant, id int64) ([]Event, error) {
	return s.tx(ctx, func(t *tx, log *eventLog) error {
		cur, err := getTodoTx(t, actor.WorkspaceID, id)
		if err != nil {
			return err
		}
		if _, err := t.Exec(`DELETE FROM todos WHERE id = ?`, id); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "todo.deleted", actor.ID, cur)
	})
}

// AutoClosePRTodos closes any open TODOs in wsID linked to prURL (case-insensitive
// match after normalization), recording a system todo.done event for each.
func (s *Store) AutoClosePRTodos(ctx context.Context, wsID, prURL, note string) ([]Event, error) {
	norm, err := NormalizePRURL(prURL)
	if err != nil || norm == "" {
		return nil, nil
	}
	var hasOpen bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM todos WHERE workspace_id = ? AND status = ? AND lower(pr_url) = lower(?))`,
		wsID, TodoStatusOpen, norm).Scan(&hasOpen); err != nil || !hasOpen {
		return nil, err
	}
	note = strings.TrimSpace(note)
	return s.tx(ctx, func(t *tx, log *eventLog) error {
		ids, err := queryInts(t, `SELECT id FROM todos WHERE workspace_id = ? AND status = ? AND lower(pr_url) = lower(?) ORDER BY id`,
			wsID, TodoStatusOpen, norm)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := t.Exec(`UPDATE todos SET status = ?, done_note = ?, done_by_id = NULL, done_at = ?, snoozed_until = NULL WHERE id = ?`,
				TodoStatusDone, note, log.now, id); err != nil {
				return err
			}
			td, err := getTodoTx(t, wsID, id)
			if err != nil {
				return err
			}
			if err := log.add(wsID, "todo.done", 0, td); err != nil {
				return err
			}
		}
		return nil
	})
}

// OpenTodoPRURLs returns the distinct non-empty pr_url values on open TODOs in wsID.
func (s *Store) OpenTodoPRURLs(ctx context.Context, wsID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT pr_url FROM todos WHERE workspace_id = ? AND status = ? AND pr_url <> ''`,
		wsID, TodoStatusOpen)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// unlinkTodos clears item_id on TODOs referencing a deleted backlog item and
// records todo.updated for each.
func unlinkTodos(t *tx, log *eventLog, actor *Participant, itemID int64) error {
	ids, err := queryInts(t, `SELECT id FROM todos WHERE workspace_id = ? AND item_id = ? ORDER BY id`, actor.WorkspaceID, itemID)
	if err != nil || len(ids) == 0 {
		return err
	}
	if _, err := t.Exec(`UPDATE todos SET item_id = NULL WHERE item_id = ?`, itemID); err != nil {
		return err
	}
	for _, id := range ids {
		td, err := getTodoTx(t, actor.WorkspaceID, id)
		if err != nil {
			return err
		}
		if err := log.add(actor.WorkspaceID, "todo.updated", actor.ID, td); err != nil {
			return err
		}
	}
	return nil
}
