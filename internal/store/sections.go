package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Section limits and supported Lucide icon names (#919).
const (
	MaxSectionName     = 60
	MaxSectionSlug     = 60
	DefaultSectionIcon = "folder"
	MaxItemChoices     = 10
	MaxChoiceLen       = 120
)

// SectionIcons are the supported Lucide icon names for custom sections (#919).
var SectionIcons = []string{
	"inbox",
	"scale",
	"flask-conical",
	"sparkles",
	"bug",
	"compass",
	"shield",
	"layers",
	"lightbulb",
	"bookmark",
	"flag",
	"help-circle",
	"folder",
	"zap",
	"clipboard-check",
}

// Section is an optional custom workspace section alongside Backlog (#919).
type Section struct {
	ID          int64  `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	Notify      bool   `json:"notify"`
	Position    int64  `json:"position"`
	CreatedBy   *int64 `json:"created_by,omitempty"`
	CreatedAt   int64  `json:"created_at"`
}

// SectionInput holds the fields for creating a section.
type SectionInput struct {
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Icon   string `json:"icon"`
	Notify bool   `json:"notify"`
}

// SectionPatch holds optional updates to a section.
type SectionPatch struct {
	Name   *string `json:"name"`
	Slug   *string `json:"slug"`
	Icon   *string `json:"icon"`
	Notify *bool   `json:"notify"`
}

const sectionCols = `id, workspace_id, slug, name, icon, notify, position, created_by, created_at`

func scanSection(row interface{ Scan(...any) error }) (*Section, error) {
	var sec Section
	var createdBy sql.NullInt64
	if err := row.Scan(&sec.ID, &sec.WorkspaceID, &sec.Slug, &sec.Name, &sec.Icon, &sec.Notify, &sec.Position, &createdBy, &sec.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if createdBy.Valid {
		sec.CreatedBy = &createdBy.Int64
	}
	return &sec, nil
}

func scanSections(rows *sql.Rows) ([]Section, error) {
	defer rows.Close()
	out := []Section{}
	for rows.Next() {
		sec, err := scanSection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sec)
	}
	return out, rows.Err()
}

func validSectionName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("%w: section name is required", ErrInvalid)
	}
	if strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("%w: section name must be one line of text", ErrInvalid)
	}
	if utf8.RuneCountInString(name) > MaxSectionName {
		return "", fmt.Errorf("%w: section name is at most %d characters", ErrInvalid, MaxSectionName)
	}
	return name, nil
}

var (
	sectionSlugRe    = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	sectionNonAlnum  = regexp.MustCompile(`[^a-z0-9]+`)
	sectionDigitsAll = regexp.MustCompile(`^[0-9]+$`)
	reservedSlugs    = []string{"all", "none", "backlog"}
)

func deriveSectionSlug(name string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.Trim(sectionNonAlnum.ReplaceAllString(s, "-"), "-")
	if s == "" {
		return "", fmt.Errorf("%w: section slug is required when name has no ASCII letters or digits", ErrInvalid)
	}
	if slices.Contains(reservedSlugs, s) || sectionDigitsAll.MatchString(s) {
		s = "s-" + s
	}
	if len(s) > MaxSectionSlug {
		s = strings.TrimRight(s[:MaxSectionSlug], "-")
	}
	return validSectionSlug(s)
}

func validSectionSlug(raw string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(raw))
	if slug == "" {
		return "", fmt.Errorf("%w: section slug cannot be empty", ErrInvalid)
	}
	if len(slug) > MaxSectionSlug {
		return "", fmt.Errorf("%w: section slug is at most %d characters", ErrInvalid, MaxSectionSlug)
	}
	if !sectionSlugRe.MatchString(slug) {
		return "", fmt.Errorf("%w: section slug %q must be lowercase letters, digits, and hyphens", ErrInvalid, raw)
	}
	if slices.Contains(reservedSlugs, slug) {
		return "", fmt.Errorf("%w: section slug %q is reserved", ErrInvalid, slug)
	}
	if sectionDigitsAll.MatchString(slug) {
		return "", fmt.Errorf("%w: section slug %q cannot be purely numeric", ErrInvalid, slug)
	}
	return slug, nil
}

func validSectionIcon(raw string) (string, error) {
	icon := strings.ToLower(strings.TrimSpace(raw))
	if icon == "" {
		return DefaultSectionIcon, nil
	}
	if !slices.Contains(SectionIcons, icon) {
		return "", fmt.Errorf("%w: unsupported section icon %q (supported: %s)", ErrInvalid, raw, strings.Join(SectionIcons, ", "))
	}
	return icon, nil
}

func validChoices(raw []string) ([]string, string, error) {
	if len(raw) == 0 {
		return nil, "[]", nil
	}
	if len(raw) > MaxItemChoices {
		return nil, "", fmt.Errorf("%w: an item can have at most %d choices", ErrInvalid, MaxItemChoices)
	}
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, c := range raw {
		v := strings.TrimSpace(c)
		if v == "" {
			return nil, "", fmt.Errorf("%w: choice label cannot be empty", ErrInvalid)
		}
		if strings.IndexFunc(v, unicode.IsControl) >= 0 {
			return nil, "", fmt.Errorf("%w: choice label must be one line of text", ErrInvalid)
		}
		if utf8.RuneCountInString(v) > MaxChoiceLen {
			return nil, "", fmt.Errorf("%w: choice label is at most %d characters", ErrInvalid, MaxChoiceLen)
		}
		if seen[v] {
			return nil, "", fmt.Errorf("%w: duplicate choice %q", ErrInvalid, v)
		}
		seen[v] = true
		out = append(out, v)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, "", err
	}
	return out, string(b), nil
}

func listSectionsTx(t *tx, wsID string) ([]Section, error) {
	rows, err := t.Query(`SELECT `+sectionCols+` FROM sections WHERE workspace_id = ? ORDER BY position, id`, wsID)
	if err != nil {
		return nil, err
	}
	return scanSections(rows)
}

func resolveSectionTx(t *tx, wsID, ref string) (*Section, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("%w: section id or slug is required", ErrInvalid)
	}
	if id, err := strconv.ParseInt(strings.TrimPrefix(ref, "#"), 10, 64); err == nil && id > 0 {
		sec, err := scanSection(t.QueryRow(`SELECT `+sectionCols+` FROM sections WHERE workspace_id = ? AND id = ?`, wsID, id))
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("section #%d: %w", id, ErrNotFound)
		}
		return sec, err
	}
	slug := strings.ToLower(ref)
	sec, err := scanSection(t.QueryRow(`SELECT `+sectionCols+` FROM sections WHERE workspace_id = ? AND slug = ?`, wsID, slug))
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("section %q: %w", ref, ErrNotFound)
	}
	return sec, err
}

func renumberSections(t *tx, ids []int64) error {
	for i, id := range ids {
		if _, err := t.Exec(`UPDATE sections SET position = ? WHERE id = ?`, i+1, id); err != nil {
			return err
		}
	}
	return nil
}

// ListSections returns a workspace's custom sections in position order.
func (s *Store) ListSections(ctx context.Context, wsID string) ([]Section, error) {
	if _, err := s.GetWorkspace(ctx, wsID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+sectionCols+` FROM sections WHERE workspace_id = ? ORDER BY position, id`, wsID)
	if err != nil {
		return nil, err
	}
	return scanSections(rows)
}

// GetSection looks up a section in wsID by numeric ID or slug.
func (s *Store) GetSection(ctx context.Context, wsID, idOrSlug string) (*Section, error) {
	t, err := s.db.beginRead(ctx)
	if err != nil {
		return nil, err
	}
	defer t.Rollback()
	return resolveSectionTx(t, wsID, idOrSlug)
}

// AddSection creates a custom section at the end of actor's workspace sections
// and records section.created.
func (s *Store) AddSection(ctx context.Context, actor *Participant, in SectionInput) (*Section, []Event, error) {
	name, err := validSectionName(in.Name)
	if err != nil {
		return nil, nil, err
	}
	var slug string
	if strings.TrimSpace(in.Slug) == "" {
		slug, err = deriveSectionSlug(name)
	} else {
		slug, err = validSectionSlug(in.Slug)
	}
	if err != nil {
		return nil, nil, err
	}
	icon, err := validSectionIcon(in.Icon)
	if err != nil {
		return nil, nil, err
	}

	var sec *Section
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var pos int64
		if err := t.QueryRow(`SELECT COALESCE(MAX(position), 0) + 1 FROM sections WHERE workspace_id = ?`, actor.WorkspaceID).Scan(&pos); err != nil {
			return err
		}
		id, err := t.insert(`INSERT INTO sections (workspace_id, slug, name, icon, notify, position, created_by, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "id",
			actor.WorkspaceID, slug, name, icon, in.Notify, pos, actor.ID, log.now)
		if err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("%w: section slug %q already exists in this workspace", ErrConflict, slug)
			}
			return err
		}
		sec, err = scanSection(t.QueryRow(`SELECT `+sectionCols+` FROM sections WHERE id = ?`, id))
		if err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "section.created", actor.ID, sec)
	})
	if err != nil {
		return nil, nil, err
	}
	return sec, evs, nil
}

// UpdateSection updates a section's name, slug, icon, or notify flag and
// records section.updated.
func (s *Store) UpdateSection(ctx context.Context, actor *Participant, idOrSlug string, p SectionPatch) (*Section, []Event, error) {
	var sec *Section
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		cur, err := resolveSectionTx(t, actor.WorkspaceID, idOrSlug)
		if err != nil {
			return err
		}
		name := cur.Name
		if p.Name != nil {
			if name, err = validSectionName(*p.Name); err != nil {
				return err
			}
		}
		slug := cur.Slug
		if p.Slug != nil {
			if slug, err = validSectionSlug(*p.Slug); err != nil {
				return err
			}
		}
		icon := cur.Icon
		if p.Icon != nil {
			if icon, err = validSectionIcon(*p.Icon); err != nil {
				return err
			}
		}
		notify := cur.Notify
		if p.Notify != nil {
			notify = *p.Notify
		}
		if _, err := t.Exec(`UPDATE sections SET name = ?, slug = ?, icon = ?, notify = ? WHERE id = ?`,
			name, slug, icon, notify, cur.ID); err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("%w: section slug %q already exists in this workspace", ErrConflict, slug)
			}
			return err
		}
		sec, err = scanSection(t.QueryRow(`SELECT `+sectionCols+` FROM sections WHERE id = ?`, cur.ID))
		if err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "section.updated", actor.ID, sec)
	})
	if err != nil {
		return nil, nil, err
	}
	return sec, evs, nil
}

// MoveSection moves a section to the 1-based position (clamped) and records
// section.moved.
func (s *Store) MoveSection(ctx context.Context, actor *Participant, idOrSlug string, position int) (*Section, []Event, error) {
	if position < 1 {
		return nil, nil, fmt.Errorf("%w: position must be >= 1", ErrInvalid)
	}
	var sec *Section
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		cur, err := resolveSectionTx(t, actor.WorkspaceID, idOrSlug)
		if err != nil {
			return err
		}
		all, err := listSectionsTx(t, actor.WorkspaceID)
		if err != nil {
			return err
		}
		rest := make([]int64, 0, len(all))
		for _, other := range all {
			if other.ID != cur.ID {
				rest = append(rest, other.ID)
			}
		}
		pos := position
		if pos > len(rest)+1 {
			pos = len(rest) + 1
		}
		ordered := append(append(append([]int64{}, rest[:pos-1]...), cur.ID), rest[pos-1:]...)
		if err := renumberSections(t, ordered); err != nil {
			return err
		}
		sec, err = scanSection(t.QueryRow(`SELECT `+sectionCols+` FROM sections WHERE id = ?`, cur.ID))
		if err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "section.moved", actor.ID, sec)
	})
	if err != nil {
		return nil, nil, err
	}
	return sec, evs, nil
}

// DeleteSection deletes a custom section, moves any remaining items in it back
// to the bottom of the main Backlog (section_id = NULL), closes any gap in
// section positions, and records section.deleted.
func (s *Store) DeleteSection(ctx context.Context, actor *Participant, idOrSlug string) (*Section, []Event, error) {
	var deleted *Section
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		cur, err := resolveSectionTx(t, actor.WorkspaceID, idOrSlug)
		if err != nil {
			return err
		}
		movingIDs, err := queuedIDs(t, actor.WorkspaceID, &cur.ID)
		if err != nil {
			return err
		}
		mainIDs, err := queuedIDs(t, actor.WorkspaceID, nil)
		if err != nil {
			return err
		}
		if _, err := t.Exec(`UPDATE items SET section_id = NULL, updated_at = ? WHERE workspace_id = ? AND section_id = ?`,
			log.now, actor.WorkspaceID, cur.ID); err != nil {
			return err
		}
		if len(movingIDs) > 0 {
			if err := renumber(t, append(mainIDs, movingIDs...)); err != nil {
				return err
			}
		}
		if _, err := t.Exec(`DELETE FROM sections WHERE id = ?`, cur.ID); err != nil {
			return err
		}
		rest, err := listSectionsTx(t, actor.WorkspaceID)
		if err != nil {
			return err
		}
		restIDs := make([]int64, 0, len(rest))
		for _, other := range rest {
			restIDs = append(restIDs, other.ID)
		}
		if err := renumberSections(t, restIDs); err != nil {
			return err
		}
		deleted = cur
		return log.add(actor.WorkspaceID, "section.deleted", actor.ID, cur)
	})
	if err != nil {
		return nil, nil, err
	}
	return deleted, evs, nil
}
