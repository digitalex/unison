package store

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

const itemCols = `id, COALESCE(number, id), workspace_id, title, body, status, position, assignee_id, lease_expires_at, created_by, created_at, updated_at, closed_at, close_note, schedule_id,
	section_id, choices_json, close_on_answer, answered_choice, answered_by_id, answered_at,
	COALESCE((SELECT slug FROM sections s WHERE s.id = items.section_id), ''),
	COALESCE((SELECT name FROM sections s WHERE s.id = items.section_id), ''),
	COALESCE((SELECT icon FROM sections s WHERE s.id = items.section_id), ''),
	(SELECT notify FROM sections s WHERE s.id = items.section_id)`

func scanItem(row interface{ Scan(...any) error }) (*Item, error) {
	var it Item
	var assignee, lease, closed, schedule, sectionID, answeredBy, answeredAt sql.NullInt64
	var choicesJSON, secSlug, secName, secIcon string
	var secNotify sql.NullBool
	if err := row.Scan(
		&it.ID, &it.Number, &it.WorkspaceID, &it.Title, &it.Body, &it.Status, &it.Position, &assignee, &lease,
		&it.CreatedBy, &it.CreatedAt, &it.UpdatedAt, &closed, &it.CloseNote, &schedule,
		&sectionID, &choicesJSON, &it.CloseOnAnswer, &it.AnsweredChoice, &answeredBy, &answeredAt,
		&secSlug, &secName, &secIcon, &secNotify,
	); err != nil {
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
	if schedule.Valid {
		it.ScheduleID = &schedule.Int64
	}
	if sectionID.Valid {
		it.SectionID = &sectionID.Int64
		it.SectionSlug = secSlug
		it.SectionName = secName
		it.SectionIcon = secIcon
		it.SectionNotify = secNotify.Bool
	}
	if choicesJSON != "" && choicesJSON != "[]" {
		var choices []string
		if err := json.Unmarshal([]byte(choicesJSON), &choices); err == nil && len(choices) > 0 {
			it.Choices = choices
		}
	}
	if answeredBy.Valid {
		it.AnsweredByID = &answeredBy.Int64
	}
	if answeredAt.Valid {
		it.AnsweredAt = &answeredAt.Int64
	}
	it.Needs = []ItemNeed{}
	ready := it.CreatedAt
	it.ReadyAt = &ready
	it.PRs = []ItemPR{}
	return &it, nil
}

// ResolveItemID resolves a per-workspace item number (preferred) or legacy
// global item ID within wsID to the database primary key items.id (#920).
func ResolveItemID(t *tx, wsID string, ref int64) (int64, error) {
	if ref <= 0 {
		return 0, fmt.Errorf("backlog item #%d: %w", ref, ErrNotFound)
	}
	var id int64
	err := t.QueryRow(`SELECT id FROM items WHERE workspace_id = ? AND number = ?`, wsID, ref).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	err = t.QueryRow(`SELECT id FROM items WHERE workspace_id = ? AND id = ?`, wsID, ref).Scan(&id)
	if err == nil {
		return id, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("backlog item #%d: %w", ref, ErrNotFound)
	}
	return 0, err
}

// ResolveItemID resolves a per-workspace item number (preferred) or legacy
// global item ID within wsID to the database primary key items.id (#920).
func (s *Store) ResolveItemID(ctx context.Context, wsID string, ref int64) (int64, error) {
	t, err := s.db.beginRead(ctx)
	if err != nil {
		return 0, err
	}
	defer t.Rollback()
	return ResolveItemID(t, wsID, ref)
}

func getItemByID(t *tx, wsID string, id int64) (*Item, error) {
	it, err := scanItem(t.QueryRow(`SELECT `+itemCols+` FROM items WHERE workspace_id = ? AND id = ?`, wsID, id))
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("backlog item #%d: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return it, attachExtras(t, wsID, []*Item{it})
}

func getItem(t *tx, wsID string, ref int64) (*Item, error) {
	id, err := ResolveItemID(t, wsID, ref)
	if err != nil {
		return nil, err
	}
	return getItemByID(t, wsID, id)
}

func equalInt64Ptr(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// queuedIDs returns the IDs of open and in-progress items in the given section
// (nil = main Backlog) in priority order.
func queuedIDs(t *tx, wsID string, sectionID *int64) ([]int64, error) {
	var rows *sql.Rows
	var err error
	if sectionID == nil {
		rows, err = t.Query(`SELECT id FROM items WHERE workspace_id = ? AND section_id IS NULL AND status != ? ORDER BY position, id`, wsID, StatusDone)
	} else {
		rows, err = t.Query(`SELECT id FROM items WHERE workspace_id = ? AND section_id = ? AND status != ? ORDER BY position, id`, wsID, *sectionID, StatusDone)
	}
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

const renumberBatchSize = 500

// renumber assigns positions 1..n to ids in order.
func renumber(t *tx, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	for start := 0; start < len(ids); start += renumberBatchSize {
		end := start + renumberBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]

		var sb strings.Builder
		sb.WriteString("UPDATE items SET position = CASE id")
		args := make([]any, 0, len(chunk)*3)

		for i, id := range chunk {
			pos := start + i + 1
			sb.WriteString(" WHEN ? THEN CAST(? AS BIGINT)")
			args = append(args, id, pos)
		}
		sb.WriteString(" END WHERE id IN (")
		for i, id := range chunk {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString("?")
			args = append(args, id)
		}
		sb.WriteString(")")

		if _, err := t.Exec(sb.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

// insertAt places id at the 1-based position (clamped) within sectionID's queue
// (nil = main Backlog). position <= 0 means the bottom of that queue.
func insertAt(t *tx, wsID string, sectionID *int64, id int64, position int) error {
	ids, err := queuedIDs(t, wsID, sectionID)
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

// expireLeases releases in-progress items whose lease has run out, recording
// a backlog.lease_expired event for each.
func expireLeases(t *tx, log *eventLog, wsID string) error {
	query := `SELECT ` + itemCols + ` FROM items WHERE status = ? AND lease_expires_at <= ?`
	args := []any{StatusInProgress, log.now}
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
		if err := attachExtras(t, it.WorkspaceID, []*Item{it}); err != nil {
			return err
		}
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

// ListBacklog returns main-Backlog items (section_id IS NULL) with the given
// status ("" = open, paused and in_progress; "all" = everything). Queued items
// come in priority order; done items come most recently closed first.
func (s *Store) ListBacklog(ctx context.Context, wsID, status string) ([]Item, []Event, error) {
	return s.ListBacklogSection(ctx, wsID, status, "")
}

// ListBacklogSection returns items filtered by status and sectionFilter:
//   - "" / "none" / "backlog": main Backlog items (section_id IS NULL)
//   - "all": main Backlog and all custom sections
//   - <id|slug>: items in that custom section
func (s *Store) ListBacklogSection(ctx context.Context, wsID, status, sectionFilter string) ([]Item, []Event, error) {
	var out []Item
	evs, err := s.readBacklog(ctx, wsID, func(t *tx) error {
		var err error
		out, err = listItems(t, wsID, status, sectionFilter)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return out, evs, nil
}

func listItems(t *tx, wsID, status, sectionFilter string) ([]Item, error) {
	if status == "assigned" {
		status = StatusInProgress // Legacy CLI/API query compatibility (#146).
	}
	query := `SELECT ` + itemCols + ` FROM items WHERE workspace_id = ?`
	args := []any{wsID}

	secFilter := strings.TrimSpace(sectionFilter)
	switch strings.ToLower(secFilter) {
	case "", "none", "backlog":
		query += ` AND section_id IS NULL`
	case "all":
	default:
		sec, err := resolveSectionTx(t, wsID, secFilter)
		if err != nil {
			return nil, err
		}
		query += ` AND section_id = ?`
		args = append(args, sec.ID)
	}

	switch status {
	case "":
		query += ` AND status != ?`
		args = append(args, StatusDone)
	case "all":
	case StatusOpen, StatusInProgress, StatusPaused, StatusDone:
		query += ` AND status = ?`
		args = append(args, status)
	default:
		return nil, fmt.Errorf("%w: unknown status %q (want open, in_progress, paused, done or all)", ErrInvalid, status)
	}
	if strings.EqualFold(secFilter, "all") {
		query += ` ORDER BY status = 'done', COALESCE(section_id, 0), position, closed_at DESC, id`
	} else {
		query += ` ORDER BY status = 'done', position, closed_at DESC, id`
	}
	rows, err := t.Query(query, args...)
	if err != nil {
		return nil, err
	}
	var list []*Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := attachExtras(t, wsID, list); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(list))
	for _, it := range list {
		out = append(out, *it)
	}
	return out, nil
}

// Backlog view-only sort orders (#922). Sorting never mutates Item.Position.
const (
	SortPriority = "priority"
	SortNewest   = "newest"
	SortOldest   = "oldest"
	SortUpdated  = "updated"
	SortTitle    = "title"
)

// SortBacklogItems orders items in-memory by a view-only sort order (#922):
// "" or "priority" preserves ListBacklogSection's canonical order; "newest",
// "oldest", "updated", and "title" sort in-memory without touching DB state.
func SortBacklogItems(items []Item, sortBy string) error {
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "", SortPriority:
		return nil
	case SortNewest:
		slices.SortFunc(items, compareItemsNewest)
	case SortOldest:
		slices.SortFunc(items, compareItemsOldest)
	case SortUpdated:
		slices.SortFunc(items, compareItemsUpdated)
	case SortTitle:
		slices.SortFunc(items, compareItemsTitle)
	default:
		return fmt.Errorf("%w: unknown sort %q (want priority, newest, oldest, updated or title)", ErrInvalid, sortBy)
	}
	return nil
}

func compareItemsNewest(a, b Item) int {
	if c := cmp.Compare(b.CreatedAt, a.CreatedAt); c != 0 {
		return c
	}
	return cmp.Compare(b.ID, a.ID)
}

func compareItemsOldest(a, b Item) int {
	if c := cmp.Compare(a.CreatedAt, b.CreatedAt); c != 0 {
		return c
	}
	return cmp.Compare(a.ID, b.ID)
}

func compareItemsUpdated(a, b Item) int {
	if c := cmp.Compare(b.UpdatedAt, a.UpdatedAt); c != 0 {
		return c
	}
	return cmp.Compare(b.ID, a.ID)
}

func compareItemsTitle(a, b Item) int {
	if c := cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Position, b.Position); c != 0 {
		return c
	}
	return cmp.Compare(a.ID, b.ID)
}

// GetItem returns one backlog item.
func (s *Store) GetItem(ctx context.Context, wsID string, id int64) (*Item, []Event, error) {
	var it *Item
	evs, err := s.readBacklog(ctx, wsID, func(t *tx) error {
		var err error
		it, err = getItem(t, wsID, id)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return it, evs, nil
}

// readBacklog runs a backlog read. Reads release expired leases first, so
// they never show a stale assignee; that takes a write transaction, but
// only when some lease has actually expired (#106). Otherwise the read
// runs in a read-only transaction, off the write lock.
func (s *Store) readBacklog(ctx context.Context, wsID string, read func(*tx) error) ([]Event, error) {
	var expired bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM items WHERE workspace_id = ? AND status = ? AND lease_expires_at <= ?)`,
		wsID, StatusInProgress, s.nowMillis()).Scan(&expired); err != nil {
		return nil, err
	}
	if expired {
		return s.tx(ctx, func(t *tx, log *eventLog) error {
			if err := expireLeases(t, log, wsID); err != nil {
				return err
			}
			return read(t)
		})
	}
	t, err := s.db.beginRead(ctx)
	if err != nil {
		return nil, err
	}
	defer t.Rollback()
	return nil, read(t)
}

// ItemInput holds fields for creating an item, optionally in a custom section
// or with structured decision choices (#919).
type ItemInput struct {
	Title         string   `json:"title"`
	Body          string   `json:"body"`
	Position      int      `json:"position"`
	AssigneeID    int64    `json:"assignee_id"`
	SectionID     *int64   `json:"section_id"`
	Section       string   `json:"section"`
	Choices       []string `json:"choices"`
	CloseOnAnswer bool     `json:"close_on_answer"`
}

func resolveItemSection(t *tx, wsID string, sectionID *int64, sectionRef string) (*int64, error) {
	if sectionID != nil {
		if *sectionID == 0 {
			return nil, nil
		}
		if *sectionID < 0 {
			return nil, fmt.Errorf("%w: section_id must be >= 0", ErrInvalid)
		}
		sec, err := resolveSectionTx(t, wsID, strconv.FormatInt(*sectionID, 10))
		if err != nil {
			return nil, err
		}
		return &sec.ID, nil
	}
	ref := strings.TrimSpace(sectionRef)
	if ref == "" || strings.EqualFold(ref, "none") || strings.EqualFold(ref, "backlog") || ref == "0" {
		return nil, nil
	}
	sec, err := resolveSectionTx(t, wsID, ref)
	if err != nil {
		return nil, err
	}
	return &sec.ID, nil
}

// AddItem adds an unassigned backlog item at the 1-based position (<= 0: bottom).
func (s *Store) AddItem(ctx context.Context, actor *Participant, title, body string, position int) (*Item, []Event, error) {
	return s.AddItemWithOptions(ctx, actor, ItemInput{Title: title, Body: body, Position: position})
}

// AddItemAssigned adds an open backlog item at position (<= 0: bottom),
// optionally assigned to assigneeID (> 0) without starting progress (#146).
func (s *Store) AddItemAssigned(ctx context.Context, actor *Participant, title, body string, position int, assigneeID int64) (*Item, []Event, error) {
	return s.AddItemWithOptions(ctx, actor, ItemInput{Title: title, Body: body, Position: position, AssigneeID: assigneeID})
}

// AddItemWithOptions adds an open item in the main Backlog or a custom section,
// optionally with structured decision choices and close_on_answer (#919).
func (s *Store) AddItemWithOptions(ctx context.Context, actor *Participant, in ItemInput) (*Item, []Event, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, nil, fmt.Errorf("%w: item title is required", ErrInvalid)
	}
	if in.AssigneeID < 0 {
		return nil, nil, fmt.Errorf("%w: assignee_id must be >= 0", ErrInvalid)
	}
	_, choicesJSON, err := validChoices(in.Choices)
	if err != nil {
		return nil, nil, err
	}
	var it *Item
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var assignee *int64
		if in.AssigneeID > 0 {
			if err := checkParticipant(t, actor.WorkspaceID, in.AssigneeID); err != nil {
				return err
			}
			assignee = &in.AssigneeID
		}
		sectionID, err := resolveItemSection(t, actor.WorkspaceID, in.SectionID, in.Section)
		if err != nil {
			return err
		}
		it, err = addItemFull(t, log, actor, title, in.Body, in.Position, assignee, nil, sectionID, choicesJSON, in.CloseOnAnswer)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return it, evs, nil
}

// addItem inserts an open item in the main Backlog at position (<= 0: bottom)
// and records backlog.added. scheduleID marks a schedule's run (#112).
func addItem(t *tx, log *eventLog, actor *Participant, title, body string, position int, assignee *int64, scheduleID *int64) (*Item, error) {
	return addItemFull(t, log, actor, title, body, position, assignee, scheduleID, nil, "[]", false)
}

func nextItemNumber(t *tx, wsID string) (int64, error) {
	var num int64
	if err := t.QueryRow(`SELECT next_item_num FROM workspaces WHERE id = ?`, wsID).Scan(&num); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	var maxExisting int64
	if err := t.QueryRow(`SELECT COALESCE(MAX(number), 0) FROM items WHERE workspace_id = ?`, wsID).Scan(&maxExisting); err != nil {
		return 0, err
	}
	if num <= maxExisting {
		num = maxExisting + 1
	}
	if num < 1 {
		num = 1
	}
	if _, err := t.Exec(`UPDATE workspaces SET next_item_num = ? WHERE id = ?`, num+1, wsID); err != nil {
		return 0, err
	}
	return num, nil
}

func addItemFull(t *tx, log *eventLog, actor *Participant, title, body string, position int, assignee *int64, scheduleID *int64, sectionID *int64, choicesJSON string, closeOnAnswer bool) (*Item, error) {
	num, err := nextItemNumber(t, actor.WorkspaceID)
	if err != nil {
		return nil, err
	}
	id, err := t.insert(`INSERT INTO items (workspace_id, number, title, body, status, position, assignee_id, created_by, created_at, updated_at, schedule_id, section_id, choices_json, close_on_answer)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?)`, "id",
		actor.WorkspaceID, num, title, strings.TrimSpace(body), StatusOpen, assignee, actor.ID, log.now, log.now, scheduleID, sectionID, choicesJSON, closeOnAnswer)
	if err != nil {
		return nil, err
	}
	if err := insertAt(t, actor.WorkspaceID, sectionID, id, position); err != nil {
		return nil, err
	}
	it, err := getItemByID(t, actor.WorkspaceID, id)
	if err != nil {
		return nil, err
	}
	// A schedule's run is the system's doing (actor 0, like lease expiry),
	// though created_by credits the schedule's creator: events by yourself
	// don't wake you or push to you, and the creator should hear of it too.
	eventActor := actor.ID
	if scheduleID != nil {
		eventActor = 0
	}
	return it, log.add(actor.WorkspaceID, "backlog.added", eventActor, it)
}

// ItemPatch holds optional item field updates.
type ItemPatch struct {
	Title         *string   `json:"title"`
	Body          *string   `json:"body"`
	SectionID     *int64    `json:"section_id"` // 0 moves back to main Backlog
	Section       *string   `json:"section"`    // "" / "none" / "backlog" / "0" moves back to main Backlog
	ClearSection  bool      `json:"-"`          // set when caller explicitly sends "section_id": null
	Choices       *[]string `json:"choices"`    // empty slice clears choices
	CloseOnAnswer *bool     `json:"close_on_answer"`
}

// UpdateItem edits an item's title, body, section, choices, and/or close_on_answer.
func (s *Store) UpdateItem(ctx context.Context, actor *Participant, id int64, p ItemPatch) (*Item, []Event, error) {
	return s.mutateItem(ctx, actor, id, "backlog.updated", func(t *tx, log *eventLog, it *Item) error {
		now := log.now
		if p.Title != nil {
			if strings.TrimSpace(*p.Title) == "" {
				return fmt.Errorf("%w: item title cannot be empty", ErrInvalid)
			}
			it.Title = strings.TrimSpace(*p.Title)
		}
		if p.Body != nil {
			it.Body = strings.TrimSpace(*p.Body)
		}

		oldSectionID := it.SectionID
		newSectionID := it.SectionID
		switch {
		case p.ClearSection:
			newSectionID = nil
		case p.SectionID != nil:
			var err error
			newSectionID, err = resolveItemSection(t, actor.WorkspaceID, p.SectionID, "")
			if err != nil {
				return err
			}
		case p.Section != nil:
			var err error
			newSectionID, err = resolveItemSection(t, actor.WorkspaceID, nil, *p.Section)
			if err != nil {
				return err
			}
		}

		choicesJSON := "[]"
		if len(it.Choices) > 0 {
			b, _ := json.Marshal(it.Choices)
			choicesJSON = string(b)
		}
		answeredChoice := it.AnsweredChoice
		answeredBy := it.AnsweredByID
		answeredAt := it.AnsweredAt
		if p.Choices != nil {
			var choices []string
			var err error
			choices, choicesJSON, err = validChoices(*p.Choices)
			if err != nil {
				return err
			}
			it.Choices = choices
			if answeredChoice != "" && !slices.Contains(choices, answeredChoice) {
				answeredChoice = ""
				answeredBy = nil
				answeredAt = nil
			}
		}

		closeOnAnswer := it.CloseOnAnswer
		if p.CloseOnAnswer != nil {
			closeOnAnswer = *p.CloseOnAnswer
		}

		if _, err := t.Exec(`UPDATE items SET title = ?, body = ?, section_id = ?, choices_json = ?, close_on_answer = ?, answered_choice = ?, answered_by_id = ?, answered_at = ?, updated_at = ? WHERE id = ?`,
			it.Title, it.Body, newSectionID, choicesJSON, closeOnAnswer, answeredChoice, answeredBy, answeredAt, now, it.ID); err != nil {
			return err
		}

		if !equalInt64Ptr(oldSectionID, newSectionID) && it.Status != StatusDone {
			oldIDs, err := queuedIDs(t, it.WorkspaceID, oldSectionID)
			if err != nil {
				return err
			}
			if err := renumber(t, oldIDs); err != nil {
				return err
			}
			if err := insertAt(t, it.WorkspaceID, newSectionID, it.ID, 0); err != nil {
				return err
			}
		}
		return nil
	})
}

// AnswerItem records a structured choice decision on item id, adds a concise
// decision comment, emits backlog.answered, and — when close_on_answer is true —
// atomically closes the item in the same transaction (#919).
func (s *Store) AnswerItem(ctx context.Context, actor *Participant, id int64, choice string) (*Item, []Event, error) {
	choice = strings.TrimSpace(choice)
	if choice == "" {
		return nil, nil, fmt.Errorf("%w: choice is required", ErrInvalid)
	}
	var out *Item
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		if err := expireLeases(t, log, actor.WorkspaceID); err != nil {
			return err
		}
		it, err := getItem(t, actor.WorkspaceID, id)
		if err != nil {
			return err
		}
		if it.Status == StatusDone {
			return fmt.Errorf("%w: item #%d is already done; reopen it first", ErrConflict, it.DisplayNumber())
		}
		if len(it.Choices) == 0 {
			return fmt.Errorf("%w: item #%d has no choices", ErrInvalid, it.DisplayNumber())
		}
		matched, ok := matchItemChoice(it.Choices, choice)
		if !ok {
			return fmt.Errorf("%w: invalid choice %q (want one of: %s)", ErrInvalid, choice, strings.Join(it.Choices, ", "))
		}

		now := log.now
		if _, err := t.Exec(`UPDATE items SET answered_choice = ?, answered_by_id = ?, answered_at = ?, updated_at = ? WHERE id = ?`,
			matched, actor.ID, now, now, it.ID); err != nil {
			return err
		}

		note := "Decision: " + matched
		cid, err := t.insert(`INSERT INTO item_comments (item_id, workspace_id, author_id, body, created_at) VALUES (?, ?, ?, ?, ?)`, "id",
			it.ID, it.WorkspaceID, actor.ID, note, now)
		if err != nil {
			return err
		}
		c := &Comment{ID: cid, ItemID: it.ID, ItemNumber: it.DisplayNumber(), WorkspaceID: it.WorkspaceID, AuthorID: actor.ID, Body: note, CreatedAt: now}
		updated, err := getItemByID(t, actor.WorkspaceID, it.ID)
		if err != nil {
			return err
		}
		if err := log.add(actor.WorkspaceID, "comment.added", actor.ID, CommentEvent{Comment: c, Item: updated}); err != nil {
			return err
		}
		if err := log.add(actor.WorkspaceID, "backlog.answered", actor.ID, updated); err != nil {
			return err
		}

		if updated.CloseOnAnswer && updated.Status != StatusDone {
			if _, err := t.Exec(`UPDATE items SET status = ?, position = 0, lease_expires_at = NULL, closed_at = ?, close_note = ?, updated_at = ? WHERE id = ?`,
				StatusDone, now, note, now, updated.ID); err != nil {
				return err
			}
			ids, err := queuedIDs(t, updated.WorkspaceID, updated.SectionID)
			if err != nil {
				return err
			}
			if err := renumber(t, ids); err != nil {
				return err
			}
			updated, err = getItemByID(t, actor.WorkspaceID, updated.ID)
			if err != nil {
				return err
			}
			if err := log.add(actor.WorkspaceID, "backlog.closed", actor.ID, updated); err != nil {
				return err
			}
		}
		out = updated
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return out, evs, nil
}

func matchItemChoice(choices []string, input string) (string, bool) {
	for _, c := range choices {
		if c == input {
			return c, true
		}
	}
	for _, c := range choices {
		if strings.EqualFold(c, input) {
			return c, true
		}
	}
	if idx, err := strconv.Atoi(input); err == nil && idx >= 1 && idx <= len(choices) {
		return choices[idx-1], true
	}
	return "", false
}

// MoveItem moves a queued item to the 1-based position (clamped) within its section.
func (s *Store) MoveItem(ctx context.Context, actor *Participant, id int64, position int) (*Item, []Event, error) {
	if position < 1 {
		return nil, nil, fmt.Errorf("%w: position must be >= 1", ErrInvalid)
	}
	return s.mutateItem(ctx, actor, id, "backlog.moved", func(t *tx, log *eventLog, it *Item) error {
		now := log.now
		if it.Status == StatusDone {
			return fmt.Errorf("%w: item #%d is done; reopen it first", ErrConflict, it.DisplayNumber())
		}
		if _, err := t.Exec(`UPDATE items SET updated_at = ? WHERE id = ?`, now, it.ID); err != nil {
			return err
		}
		return insertAt(t, it.WorkspaceID, it.SectionID, it.ID, position)
	})
}

func checkParticipant(t *tx, wsID string, participantID int64) error {
	var exists int
	if err := t.QueryRow(`SELECT COUNT(*) FROM participants WHERE id = ? AND workspace_id = ?`, participantID, wsID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return fmt.Errorf("participant %d: %w", participantID, ErrNotFound)
	}
	return nil
}

// AssignItem sets (assigneeID > 0) or clears (assigneeID == 0) an item's
// assignee without changing its status or lease (#146): open items stay open
// with no lease, and in-progress items stay in progress.
func (s *Store) AssignItem(ctx context.Context, actor *Participant, id, assigneeID int64) (*Item, []Event, error) {
	if assigneeID < 0 {
		return nil, nil, fmt.Errorf("%w: assignee_id must be >= 0", ErrInvalid)
	}
	return s.mutateItemAs(ctx, actor, id, func(t *tx, log *eventLog, it *Item) (string, error) {
		if it.Status == StatusDone {
			return "", fmt.Errorf("%w: item #%d is done", ErrConflict, it.DisplayNumber())
		}
		if assigneeID == 0 {
			_, err := t.Exec(`UPDATE items SET assignee_id = NULL, updated_at = ? WHERE id = ?`, log.now, it.ID)
			return "backlog.unassigned", err
		}
		if err := checkParticipant(t, it.WorkspaceID, assigneeID); err != nil {
			return "", err
		}
		_, err := t.Exec(`UPDATE items SET assignee_id = ?, updated_at = ? WHERE id = ?`, assigneeID, log.now, it.ID)
		return "backlog.assigned", err
	})
}

// StartItem marks an item in progress by actor with a fresh 12-hour lease
// (#146), assigning it to actor if unassigned. Calling StartItem again on an
// item already in progress by actor renews the lease ("backlog.renewed").
func (s *Store) StartItem(ctx context.Context, actor *Participant, id int64) (*Item, []Event, error) {
	return s.mutateItemAs(ctx, actor, id, func(t *tx, log *eventLog, it *Item) (string, error) {
		eventType := "backlog.started"
		if it.Status == StatusInProgress {
			eventType = "backlog.renewed"
		}
		return eventType, startItem(t, it, actor.ID, log.now)
	})
}

func startItem(t *tx, it *Item, actorID, now int64) error {
	if err := checkParticipant(t, it.WorkspaceID, actorID); err != nil {
		return err
	}
	switch {
	case it.Status == StatusDone:
		return fmt.Errorf("%w: item #%d is done", ErrConflict, it.DisplayNumber())
	case it.Status == StatusPaused:
		return fmt.Errorf("%w: item #%d is paused; resume it first", ErrConflict, it.DisplayNumber())
	case it.AssigneeID != nil && *it.AssigneeID != actorID:
		if it.Status == StatusInProgress {
			return fmt.Errorf("%w: item #%d is already in progress by participant %d", ErrConflict, it.DisplayNumber(), *it.AssigneeID)
		}
		return fmt.Errorf("%w: item #%d is assigned to participant %d", ErrConflict, it.DisplayNumber(), *it.AssigneeID)
	}
	lease := now + LeaseDuration.Milliseconds()
	res, err := t.Exec(`UPDATE items SET status = ?, assignee_id = ?, lease_expires_at = ?, updated_at = ?
		WHERE id = ? AND status NOT IN (?, ?) AND (assignee_id IS NULL OR assignee_id = ?)`,
		StatusInProgress, actorID, lease, now, it.ID, StatusDone, StatusPaused, actorID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("%w: item #%d was claimed concurrently", ErrConflict, it.DisplayNumber())
	}
	it.Status, it.AssigneeID, it.LeaseExpiresAt = StatusInProgress, &actorID, &lease
	return nil
}

// ClaimNext atomically claims and starts the highest-priority open, unblocked
// item in the main Backlog (section_id IS NULL) that is unassigned or already
// assigned to actor. Paused items, blocked items (#921), and custom section
// items (#919) are skipped. Returns ErrNotFound if none.
func (s *Store) ClaimNext(ctx context.Context, actor *Participant) (*Item, []Event, error) {
	var it *Item
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		if err := expireLeases(t, log, actor.WorkspaceID); err != nil {
			return err
		}
		var err error
		it, err = scanItem(t.QueryRow(`SELECT `+itemCols+` FROM items
			WHERE workspace_id = ? AND section_id IS NULL AND status = ? AND (assignee_id IS NULL OR assignee_id = ?)
			  AND NOT EXISTS (
				SELECT 1 FROM item_needs n
				JOIN items dep ON dep.id = n.needs_item_id AND dep.workspace_id = n.workspace_id
				WHERE n.workspace_id = items.workspace_id AND n.item_id = items.id AND dep.status != ?
			  )
			ORDER BY position, id LIMIT 1`,
			actor.WorkspaceID, StatusOpen, actor.ID, StatusDone))
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("%w: no open backlog items", ErrNotFound)
		}
		if err != nil {
			return err
		}
		if err := startItem(t, it, actor.ID, log.now); err != nil {
			return err
		}
		if err := attachExtras(t, actor.WorkspaceID, []*Item{it}); err != nil {
			return err
		}
		it.UpdatedAt = log.now
		return log.add(actor.WorkspaceID, "backlog.started", actor.ID, it)
	})
	if err != nil {
		return nil, nil, err
	}
	return it, evs, nil
}

// StopItem moves an in-progress item back to open and clears its lease,
// keeping its assignee (#146).
func (s *Store) StopItem(ctx context.Context, actor *Participant, id int64) (*Item, []Event, error) {
	return s.mutateItem(ctx, actor, id, "backlog.stopped", func(t *tx, log *eventLog, it *Item) error {
		if it.Status != StatusInProgress {
			return fmt.Errorf("%w: item #%d is %s, not in progress", ErrConflict, it.DisplayNumber(), it.Status)
		}
		it.Status, it.LeaseExpiresAt = StatusOpen, nil
		_, err := t.Exec(`UPDATE items SET status = ?, lease_expires_at = NULL, updated_at = ? WHERE id = ?`, StatusOpen, log.now, it.ID)
		return err
	})
}

// PauseItem puts an open or in-progress item on hold ("paused"), clearing any
// active lease while keeping its assignee and queue position (#921).
func (s *Store) PauseItem(ctx context.Context, actor *Participant, id int64) (*Item, []Event, error) {
	return s.mutateItem(ctx, actor, id, "backlog.paused", func(t *tx, log *eventLog, it *Item) error {
		if it.Status == StatusDone {
			return fmt.Errorf("%w: cannot pause closed item #%d", ErrConflict, it.DisplayNumber())
		}
		if it.Status == StatusPaused {
			return fmt.Errorf("%w: item #%d is already paused", ErrConflict, it.DisplayNumber())
		}
		it.Status, it.LeaseExpiresAt = StatusPaused, nil
		_, err := t.Exec(`UPDATE items SET status = ?, lease_expires_at = NULL, updated_at = ? WHERE id = ?`, StatusPaused, log.now, it.ID)
		return err
	})
}

// ResumeItem returns a paused item to open, keeping its assignee and queue
// position (#921).
func (s *Store) ResumeItem(ctx context.Context, actor *Participant, id int64) (*Item, []Event, error) {
	return s.mutateItem(ctx, actor, id, "backlog.resumed", func(t *tx, log *eventLog, it *Item) error {
		if it.Status != StatusPaused {
			return fmt.Errorf("%w: item #%d is %s, not paused", ErrConflict, it.DisplayNumber(), it.Status)
		}
		it.Status, it.LeaseExpiresAt = StatusOpen, nil
		_, err := t.Exec(`UPDATE items SET status = ?, lease_expires_at = NULL, updated_at = ? WHERE id = ?`, StatusOpen, log.now, it.ID)
		return err
	})
}

// ReleaseItem returns an in-progress, paused, or assigned item to the unassigned open queue.
func (s *Store) ReleaseItem(ctx context.Context, actor *Participant, id int64) (*Item, []Event, error) {
	return s.mutateItem(ctx, actor, id, "backlog.released", func(t *tx, log *eventLog, it *Item) error {
		if it.Status == StatusDone || (it.Status == StatusOpen && it.AssigneeID == nil) {
			return fmt.Errorf("%w: item #%d is %s, not assigned or in progress", ErrConflict, it.DisplayNumber(), it.Status)
		}
		it.Status, it.AssigneeID, it.LeaseExpiresAt = StatusOpen, nil, nil
		_, err := t.Exec(`UPDATE items SET status = ?, assignee_id = NULL, lease_expires_at = NULL, updated_at = ? WHERE id = ?`, StatusOpen, log.now, it.ID)
		return err
	})
}

// CloseItem marks an item done with an optional note (e.g. a PR link).
func (s *Store) CloseItem(ctx context.Context, actor *Participant, id int64, note string) (*Item, []Event, error) {
	return s.mutateItem(ctx, actor, id, "backlog.closed", func(t *tx, log *eventLog, it *Item) error {
		now := log.now
		if it.Status == StatusDone {
			return fmt.Errorf("%w: item #%d is already done", ErrConflict, it.DisplayNumber())
		}
		it.Status, it.LeaseExpiresAt, it.ClosedAt, it.CloseNote = StatusDone, nil, &now, strings.TrimSpace(note)
		if _, err := t.Exec(`UPDATE items SET status = ?, position = 0, lease_expires_at = NULL, closed_at = ?, close_note = ?, updated_at = ? WHERE id = ?`,
			StatusDone, now, it.CloseNote, now, it.ID); err != nil {
			return err
		}
		if err := linkPRsInNote(t, log, actor, it, it.CloseNote); err != nil {
			return err
		}
		ids, err := queuedIDs(t, it.WorkspaceID, it.SectionID)
		if err != nil {
			return err
		}
		return renumber(t, ids)
	})
}

// ReopenItem returns a done item to the bottom of its section's open queue.
func (s *Store) ReopenItem(ctx context.Context, actor *Participant, id int64) (*Item, []Event, error) {
	return s.mutateItem(ctx, actor, id, "backlog.reopened", func(t *tx, log *eventLog, it *Item) error {
		now := log.now
		if it.Status != StatusDone {
			return fmt.Errorf("%w: item #%d is not done", ErrConflict, it.DisplayNumber())
		}
		if _, err := t.Exec(`UPDATE items SET status = ?, assignee_id = NULL, closed_at = NULL, close_note = '', answered_choice = '', answered_by_id = NULL, answered_at = NULL, updated_at = ? WHERE id = ?`,
			StatusOpen, now, it.ID); err != nil {
			return err
		}
		return insertAt(t, it.WorkspaceID, it.SectionID, it.ID, 0)
	})
}

// DeleteItem removes an item entirely.
func (s *Store) DeleteItem(ctx context.Context, actor *Participant, id int64) ([]Event, error) {
	return s.tx(ctx, func(t *tx, log *eventLog) error {
		it, err := getItem(t, actor.WorkspaceID, id)
		if err != nil {
			return err
		}
		if _, err := t.Exec(`DELETE FROM item_needs WHERE item_id = ? OR needs_item_id = ?`, it.ID, it.ID); err != nil {
			return err
		}
		for _, table := range []string{"item_comments", "item_prs"} {
			if _, err := t.Exec(`DELETE FROM `+table+` WHERE item_id = ?`, it.ID); err != nil {
				return err
			}
		}
		if err := unlinkMessages(t, log, actor, it.ID); err != nil {
			return err
		}
		if err := unlinkTodos(t, log, actor, it.ID); err != nil {
			return err
		}
		if err := unlinkArtifacts(t, log, actor, it.ID); err != nil {
			return err
		}
		if _, err := t.Exec(`DELETE FROM items WHERE id = ?`, it.ID); err != nil {
			return err
		}
		ids, err := queuedIDs(t, actor.WorkspaceID, it.SectionID)
		if err != nil {
			return err
		}
		if err := renumber(t, ids); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "backlog.deleted", actor.ID, it)
	})
}

// unlinkMessages keeps the chat messages about a deleted item, no longer
// tied to it, recording a message.updated event for each.
func unlinkMessages(t *tx, log *eventLog, actor *Participant, itemID int64) error {
	rows, err := t.Query(selectMessages+` WHERE m.workspace_id = ? AND m.item_id = ? ORDER BY m.id`, actor.WorkspaceID, itemID)
	msgs, err := scanMessages(rows, err)
	if err != nil || len(msgs) == 0 {
		return err
	}
	if _, err := t.Exec(`UPDATE messages SET item_id = NULL WHERE item_id = ?`, itemID); err != nil {
		return err
	}
	for _, m := range msgs {
		m.ItemID, m.Item = nil, nil
		if err := log.add(actor.WorkspaceID, "message.updated", actor.ID, m); err != nil {
			return err
		}
	}
	return nil
}

// mutateItem loads an item (after expiring stale leases), applies fn, then
// reloads it and records an event of the given type.
func (s *Store) mutateItem(ctx context.Context, actor *Participant, id int64, eventType string, fn func(*tx, *eventLog, *Item) error) (*Item, []Event, error) {
	return s.mutateItemAs(ctx, actor, id, func(t *tx, log *eventLog, it *Item) (string, error) {
		return eventType, fn(t, log, it)
	})
}

// mutateItemAs is mutateItem with the event type chosen by fn.
func (s *Store) mutateItemAs(ctx context.Context, actor *Participant, id int64, fn func(*tx, *eventLog, *Item) (string, error)) (*Item, []Event, error) {
	var it *Item
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		if err := expireLeases(t, log, actor.WorkspaceID); err != nil {
			return err
		}
		var err error
		if it, err = getItem(t, actor.WorkspaceID, id); err != nil {
			return err
		}
		realID := it.ID
		eventType, err := fn(t, log, it)
		if err != nil {
			return err
		}
		if it, err = getItemByID(t, actor.WorkspaceID, realID); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, eventType, actor.ID, it)
	})
	if err != nil {
		return nil, nil, err
	}
	return it, evs, nil
}
