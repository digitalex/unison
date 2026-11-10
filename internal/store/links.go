package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Link is a custom reference link on a workspace (#299): a spec, doc,
// dashboard or any web page, by name. Links are in the order they were
// added (position), like repositories.
type Link struct {
	ID          int64  `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Position    int64  `json:"position"`
	AddedBy     int64  `json:"added_by"`
	CreatedAt   int64  `json:"created_at"`
}

// LinkInput holds the fields for adding a link.
type LinkInput struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// LinkPatch holds optional link field updates.
type LinkPatch struct {
	Name *string `json:"name"`
	URL  *string `json:"url"`
}

// MaxLinkName is the longest a link's name may be, in characters.
const MaxLinkName = 100

const linkCols = `id, workspace_id, name, url, position, added_by, created_at`

func scanLinks(rows *sql.Rows) ([]Link, error) {
	defer rows.Close()
	out := []Link{}
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.ID, &l.WorkspaceID, &l.Name, &l.URL, &l.Position, &l.AddedBy, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func listLinksTx(t *tx, wsID string) ([]Link, error) {
	rows, err := t.Query(`SELECT `+linkCols+` FROM workspace_links WHERE workspace_id = ? ORDER BY position, id`, wsID)
	if err != nil {
		return nil, err
	}
	return scanLinks(rows)
}

func linkName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("%w: a link needs a name", ErrInvalid)
	}
	if strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("%w: a link name must be one line of text", ErrInvalid)
	}
	if utf8.RuneCountInString(name) > MaxLinkName {
		return "", fmt.Errorf("%w: a link name is at most %d characters", ErrInvalid, MaxLinkName)
	}
	return name, nil
}

func getLink(t *tx, wsID string, id int64) (*Link, error) {
	rows, err := t.Query(`SELECT `+linkCols+` FROM workspace_links WHERE workspace_id = ? AND id = ?`, wsID, id)
	if err != nil {
		return nil, err
	}
	list, err := scanLinks(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("link %d: %w", id, ErrNotFound)
	}
	return &list[0], nil
}

// ListLinks returns a workspace's links in order.
func (s *Store) ListLinks(ctx context.Context, wsID string) ([]Link, error) {
	if _, err := s.GetWorkspace(ctx, wsID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+linkCols+` FROM workspace_links WHERE workspace_id = ? ORDER BY position, id`, wsID)
	if err != nil {
		return nil, err
	}
	return scanLinks(rows)
}

// AddLink adds a link at the end of the actor's workspace's links and
// records link.added.
func (s *Store) AddLink(ctx context.Context, actor *Participant, in LinkInput) (*Link, []Event, error) {
	name, err := linkName(in.Name)
	if err != nil {
		return nil, nil, err
	}
	u, err := httpURL("link", in.URL)
	if err != nil {
		return nil, nil, err
	}
	l := &Link{WorkspaceID: actor.WorkspaceID, Name: name, URL: u, AddedBy: actor.ID}
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		l.CreatedAt = log.now
		if err := t.QueryRow(`SELECT COALESCE(MAX(position), 0) + 1 FROM workspace_links WHERE workspace_id = ?`, l.WorkspaceID).Scan(&l.Position); err != nil {
			return err
		}
		var err error
		if l.ID, err = t.insert(`INSERT INTO workspace_links (workspace_id, name, url, position, added_by, created_at) VALUES (?, ?, ?, ?, ?, ?)`, "id",
			l.WorkspaceID, l.Name, l.URL, l.Position, l.AddedBy, l.CreatedAt); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "link.added", actor.ID, l)
	})
	if err != nil {
		return nil, nil, err
	}
	return l, evs, nil
}

// EditLink changes a link's name or URL and records link.updated.
func (s *Store) EditLink(ctx context.Context, actor *Participant, id int64, p LinkPatch) (*Link, []Event, error) {
	var l *Link
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var err error
		if l, err = getLink(t, actor.WorkspaceID, id); err != nil {
			return err
		}
		if p.Name != nil {
			if l.Name, err = linkName(*p.Name); err != nil {
				return err
			}
		}
		if p.URL != nil {
			if l.URL, err = httpURL("link", *p.URL); err != nil {
				return err
			}
		}
		if _, err := t.Exec(`UPDATE workspace_links SET name = ?, url = ? WHERE id = ?`, l.Name, l.URL, l.ID); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "link.updated", actor.ID, l)
	})
	if err != nil {
		return nil, nil, err
	}
	return l, evs, nil
}

// RemoveLink deletes a link, closes the gap in positions, and records
// link.removed.
func (s *Store) RemoveLink(ctx context.Context, actor *Participant, id int64) ([]Event, error) {
	return s.tx(ctx, func(t *tx, log *eventLog) error {
		l, err := getLink(t, actor.WorkspaceID, id)
		if err != nil {
			return err
		}
		if _, err := t.Exec(`DELETE FROM workspace_links WHERE id = ?`, l.ID); err != nil {
			return err
		}
		rest, err := listLinksTx(t, l.WorkspaceID)
		if err != nil {
			return err
		}
		for i, other := range rest {
			if _, err := t.Exec(`UPDATE workspace_links SET position = ? WHERE id = ?`, i+1, other.ID); err != nil {
				return err
			}
		}
		return log.add(actor.WorkspaceID, "link.removed", actor.ID, l)
	})
}
