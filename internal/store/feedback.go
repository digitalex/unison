package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Feedback (#281): in-app bug reports and suggestions with an optional
// screenshot and page context, triaged on the Feedback screen until promoted
// to the backlog ("Move to backlog") or dismissed. Feedback reports are
// numbered on their own ("feedback N", never "#N", which links to a backlog
// item).

// Feedback kinds.
const (
	FeedbackBug        = "bug"
	FeedbackSuggestion = "suggestion"
)

// Feedback severities.
const (
	SeverityLow      = "low"
	SeverityMedium   = "medium"
	SeverityHigh     = "high"
	SeverityCritical = "critical"
)

// Feedback statuses.
const (
	FeedbackOpen      = "open"
	FeedbackPromoted  = "promoted"
	FeedbackDismissed = "dismissed"
)

const maxFeedbackPageLen = 500

// Feedback is a submitted feedback report in a workspace.
type Feedback struct {
	ID          int64  `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Kind        string `json:"kind"`
	Severity    string `json:"severity"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	ImageURL    string `json:"image_url,omitempty"`
	PageURL     string `json:"page_url,omitempty"`
	Status      string `json:"status"`
	CreatedBy   int64  `json:"created_by"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
	// PromotedItemID is the backlog item the feedback report was moved to,
	// with its per-workspace number, who moved it and when; nil unless promoted.
	PromotedItemID     *int64 `json:"promoted_item_id,omitempty"`
	PromotedItemNumber *int64 `json:"promoted_item_number,omitempty"`
	PromotedBy         *int64 `json:"promoted_by,omitempty"`
	PromotedAt         *int64 `json:"promoted_at,omitempty"`
}

// FeedbackInput holds fields for creating a feedback report.
type FeedbackInput struct {
	Kind     string `json:"kind"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	ImageURL string `json:"image_url"`
	PageURL  string `json:"page_url"`
}

// FeedbackPatch holds optional field updates for a feedback report.
type FeedbackPatch struct {
	Kind     *string `json:"kind"`
	Severity *string `json:"severity"`
	Title    *string `json:"title"`
	Body     *string `json:"body"`
	ImageURL *string `json:"image_url"`
	PageURL  *string `json:"page_url"`
	Status   *string `json:"status"`
}

// FeedbackPromotion is what PromoteFeedback returns: the feedback report, now
// promoted, and the backlog item created from it.
type FeedbackPromotion struct {
	Feedback *Feedback `json:"feedback"`
	Item     *Item     `json:"item"`
}

const feedbackCols = `id, workspace_id, kind, severity, title, body, image_url, page_url, status, created_by, created_at, updated_at, promoted_item_id,
	CASE WHEN promoted_item_id IS NOT NULL THEN COALESCE((SELECT i.number FROM items i WHERE i.id = feedback_reports.promoted_item_id), promoted_item_id) END,
	promoted_by, promoted_at`

func scanFeedback(row interface{ Scan(...any) error }) (*Feedback, error) {
	var f Feedback
	var item, itemNum, by, at sql.NullInt64
	if err := row.Scan(&f.ID, &f.WorkspaceID, &f.Kind, &f.Severity, &f.Title, &f.Body, &f.ImageURL, &f.PageURL, &f.Status,
		&f.CreatedBy, &f.CreatedAt, &f.UpdatedAt, &item, &itemNum, &by, &at); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if item.Valid {
		f.PromotedItemID = &item.Int64
	}
	if itemNum.Valid {
		f.PromotedItemNumber = &itemNum.Int64
	}
	if by.Valid {
		f.PromotedBy = &by.Int64
	}
	if at.Valid {
		f.PromotedAt = &at.Int64
	}
	return &f, nil
}

func getFeedback(t *tx, wsID string, id int64) (*Feedback, error) {
	f, err := scanFeedback(t.QueryRow(`SELECT `+feedbackCols+` FROM feedback_reports WHERE workspace_id = ? AND id = ?`, wsID, id))
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("feedback %d: %w", id, ErrNotFound)
	}
	return f, err
}

func (s *Store) readFeedback(ctx context.Context, read func(*tx) error) error {
	t, err := s.db.beginRead(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback()
	return read(t)
}

func validFeedbackKind(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", FeedbackBug:
		return FeedbackBug, nil
	case FeedbackSuggestion:
		return FeedbackSuggestion, nil
	default:
		return "", fmt.Errorf("%w: unknown feedback kind %q (want bug or suggestion)", ErrInvalid, raw)
	}
}

func validFeedbackSeverity(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", SeverityMedium:
		return SeverityMedium, nil
	case SeverityLow:
		return SeverityLow, nil
	case SeverityHigh:
		return SeverityHigh, nil
	case SeverityCritical:
		return SeverityCritical, nil
	default:
		return "", fmt.Errorf("%w: unknown feedback severity %q (want low, medium, high or critical)", ErrInvalid, raw)
	}
}

var wsImageURLRe = regexp.MustCompile(`^(?:https?://[^/\s]+)?(/api/workspaces/([\w-]+)/images/\d+)$`)

func validFeedbackImageURL(wsID, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if m := wsImageURLRe.FindStringSubmatch(raw); m != nil {
		if m[2] != wsID {
			return "", fmt.Errorf("%w: image belongs to another workspace", ErrInvalid)
		}
		return m[1], nil
	}
	return httpURL("image", raw)
}

func validFeedbackPageURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if utf8.RuneCountInString(raw) > maxFeedbackPageLen {
		return "", fmt.Errorf("%w: page URL can be at most %d characters", ErrInvalid, maxFeedbackPageLen)
	}
	for _, r := range raw {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%w: page URL cannot contain control characters", ErrInvalid)
		}
	}
	return raw, nil
}

// ListFeedback returns feedback reports matching status ("" or "open",
// "promoted", "dismissed", or "all"): open ones newest first, then promoted
// ones most recently promoted first, then dismissed ones.
func (s *Store) ListFeedback(ctx context.Context, wsID, status string) ([]Feedback, error) {
	query := `SELECT ` + feedbackCols + ` FROM feedback_reports WHERE workspace_id = ?`
	args := []any{wsID}
	switch status {
	case "", FeedbackOpen:
		query += ` AND status = ?`
		args = append(args, FeedbackOpen)
	case FeedbackPromoted, FeedbackDismissed:
		query += ` AND status = ?`
		args = append(args, status)
	case "all":
	default:
		return nil, fmt.Errorf("%w: unknown feedback status %q (want open, promoted, dismissed or all)", ErrInvalid, status)
	}
	query += ` ORDER BY CASE status WHEN 'open' THEN 0 WHEN 'promoted' THEN 1 ELSE 2 END,
		CASE WHEN status = 'promoted' THEN COALESCE(promoted_at, created_at) ELSE created_at END DESC, id DESC`
	out := []Feedback{}
	err := s.readFeedback(ctx, func(t *tx) error {
		rows, err := t.Query(query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			f, err := scanFeedback(rows)
			if err != nil {
				return err
			}
			out = append(out, *f)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetFeedback returns one feedback report.
func (s *Store) GetFeedback(ctx context.Context, wsID string, id int64) (*Feedback, error) {
	var f *Feedback
	err := s.readFeedback(ctx, func(t *tx) error {
		var err error
		f, err = getFeedback(t, wsID, id)
		return err
	})
	return f, err
}

// CreateFeedback adds an open feedback report and records feedback.created.
func (s *Store) CreateFeedback(ctx context.Context, actor *Participant, in FeedbackInput) (*Feedback, []Event, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, nil, fmt.Errorf("%w: feedback title is required", ErrInvalid)
	}
	kind, err := validFeedbackKind(in.Kind)
	if err != nil {
		return nil, nil, err
	}
	severity, err := validFeedbackSeverity(in.Severity)
	if err != nil {
		return nil, nil, err
	}
	imageURL, err := validFeedbackImageURL(actor.WorkspaceID, in.ImageURL)
	if err != nil {
		return nil, nil, err
	}
	pageURL, err := validFeedbackPageURL(in.PageURL)
	if err != nil {
		return nil, nil, err
	}
	var f *Feedback
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		id, err := t.insert(`INSERT INTO feedback_reports
			(workspace_id, kind, severity, title, body, image_url, page_url, status, created_by, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "id",
			actor.WorkspaceID, kind, severity, title, strings.TrimSpace(in.Body), imageURL, pageURL, FeedbackOpen, actor.ID, log.now, log.now)
		if err != nil {
			return err
		}
		if f, err = getFeedback(t, actor.WorkspaceID, id); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "feedback.created", actor.ID, f)
	})
	if err != nil {
		return nil, nil, err
	}
	return f, evs, nil
}

// UpdateFeedback edits a feedback report and records feedback.updated.
func (s *Store) UpdateFeedback(ctx context.Context, actor *Participant, id int64, p FeedbackPatch) (*Feedback, []Event, error) {
	var f *Feedback
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var err error
		if f, err = getFeedback(t, actor.WorkspaceID, id); err != nil {
			return err
		}
		if p.Title != nil {
			if strings.TrimSpace(*p.Title) == "" {
				return fmt.Errorf("%w: feedback title cannot be empty", ErrInvalid)
			}
			f.Title = strings.TrimSpace(*p.Title)
		}
		if p.Body != nil {
			f.Body = strings.TrimSpace(*p.Body)
		}
		if p.Kind != nil {
			if f.Kind, err = validFeedbackKind(*p.Kind); err != nil {
				return err
			}
		}
		if p.Severity != nil {
			if f.Severity, err = validFeedbackSeverity(*p.Severity); err != nil {
				return err
			}
		}
		if p.ImageURL != nil {
			if f.ImageURL, err = validFeedbackImageURL(actor.WorkspaceID, *p.ImageURL); err != nil {
				return err
			}
		}
		if p.PageURL != nil {
			if f.PageURL, err = validFeedbackPageURL(*p.PageURL); err != nil {
				return err
			}
		}
		if p.Status != nil {
			next := strings.ToLower(strings.TrimSpace(*p.Status))
			switch next {
			case FeedbackOpen, FeedbackDismissed:
				if f.Status == FeedbackPromoted {
					return fmt.Errorf("%w: feedback %d was already moved to the backlog", ErrConflict, f.ID)
				}
				f.Status = next
			default:
				return fmt.Errorf("%w: invalid feedback status %q (want open or dismissed)", ErrInvalid, *p.Status)
			}
		}
		if _, err := t.Exec(`UPDATE feedback_reports
			SET kind = ?, severity = ?, title = ?, body = ?, image_url = ?, page_url = ?, status = ?, updated_at = ?
			WHERE id = ?`,
			f.Kind, f.Severity, f.Title, f.Body, f.ImageURL, f.PageURL, f.Status, log.now, f.ID); err != nil {
			return err
		}
		f.UpdatedAt = log.now
		return log.add(actor.WorkspaceID, "feedback.updated", actor.ID, f)
	})
	if err != nil {
		return nil, nil, err
	}
	return f, evs, nil
}

// DismissFeedback marks an open feedback report dismissed and records feedback.updated.
func (s *Store) DismissFeedback(ctx context.Context, actor *Participant, id int64) (*Feedback, []Event, error) {
	status := FeedbackDismissed
	return s.UpdateFeedback(ctx, actor, id, FeedbackPatch{Status: &status})
}

// ReopenFeedback reopens a dismissed feedback report and records feedback.updated.
func (s *Store) ReopenFeedback(ctx context.Context, actor *Participant, id int64) (*Feedback, []Event, error) {
	status := FeedbackOpen
	return s.UpdateFeedback(ctx, actor, id, FeedbackPatch{Status: &status})
}

// DeleteFeedback removes a feedback report and records feedback.deleted.
// Any backlog item it was promoted to remains untouched.
func (s *Store) DeleteFeedback(ctx context.Context, actor *Participant, id int64) ([]Event, error) {
	return s.tx(ctx, func(t *tx, log *eventLog) error {
		f, err := getFeedback(t, actor.WorkspaceID, id)
		if err != nil {
			return err
		}
		if _, err := t.Exec(`DELETE FROM feedback_reports WHERE id = ?`, f.ID); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "feedback.deleted", actor.ID, f)
	})
}

// PromoteFeedback moves a feedback report to the backlog ("Move to backlog"):
// one transaction adds a backlog item at position (<= 0: bottom) with the
// report's title and a body preserving its metadata, Markdown description,
// screenshot, and provenance, then marks the report promoted to that item. It
// records backlog.added and feedback.promoted.
func (s *Store) PromoteFeedback(ctx context.Context, actor *Participant, id int64, position int) (*FeedbackPromotion, []Event, error) {
	var out FeedbackPromotion
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		f, err := getFeedback(t, actor.WorkspaceID, id)
		if err != nil {
			return err
		}
		if f.Status == FeedbackPromoted {
			num := int64(0)
			if f.PromotedItemNumber != nil && *f.PromotedItemNumber > 0 {
				num = *f.PromotedItemNumber
			} else if f.PromotedItemID != nil {
				num = *f.PromotedItemID
			}
			return fmt.Errorf("%w: feedback %d was already moved to the backlog as #%d", ErrConflict, f.ID, num)
		}
		body := promotedFeedbackBody(f)
		if out.Item, err = addItem(t, log, actor, f.Title, body, position, nil, nil); err != nil {
			return err
		}
		if _, err := t.Exec(`UPDATE feedback_reports
			SET status = ?, promoted_item_id = ?, promoted_by = ?, promoted_at = ?, updated_at = ?
			WHERE id = ?`,
			FeedbackPromoted, out.Item.ID, actor.ID, log.now, log.now, f.ID); err != nil {
			return err
		}
		if out.Feedback, err = getFeedback(t, actor.WorkspaceID, f.ID); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "feedback.promoted", actor.ID, out.Feedback)
	})
	if err != nil {
		return nil, nil, err
	}
	return &out, evs, nil
}

func titleCase(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func promotedFeedbackBody(f *Feedback) string {
	var b strings.Builder
	kindLabel := map[string]string{
		FeedbackBug:        "Bug",
		FeedbackSuggestion: "Suggestion",
	}[f.Kind]
	if kindLabel == "" {
		kindLabel = titleCase(f.Kind)
	}
	fmt.Fprintf(&b, "**Type:** %s · **Severity:** %s", kindLabel, titleCase(f.Severity))
	if f.PageURL != "" {
		fmt.Fprintf(&b, " · **Page:** `%s`", strings.ReplaceAll(f.PageURL, "`", ""))
	}
	if f.Body != "" {
		b.WriteString("\n\n")
		b.WriteString(f.Body)
	}
	if f.ImageURL != "" && !strings.Contains(f.Body, f.ImageURL) {
		b.WriteString("\n\n")
		fmt.Fprintf(&b, "![Screenshot](%s)", f.ImageURL)
	}
	b.WriteString("\n\n---\n\n")
	fmt.Fprintf(&b, "_Moved here from feedback %d (`unison feedback show %d`)._", f.ID, f.ID)
	return b.String()
}
