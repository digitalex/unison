package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxArtifactBytes is the largest artifact file a workspace accepts (#918).
	MaxArtifactBytes = 10 << 20
	// MaxArtifactNameLen is the maximum length of an artifact filename.
	MaxArtifactNameLen = 120
)

// ArtifactItem summarizes the backlog item an artifact is attached to.
type ArtifactItem struct {
	ID     int64  `json:"id"`
	Number int64  `json:"number"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// Artifact holds workspace artifact metadata (#918). Raw bytes are served from
// RawURL and never serialized in JSON metadata lists or SSE events.
type Artifact struct {
	ID            int64         `json:"id"`
	WorkspaceID   string        `json:"workspace_id"`
	Name          string        `json:"name"`
	Title         string        `json:"title"`
	MimeType      string        `json:"mime_type"`
	Size          int64         `json:"size"`
	SHA256        string        `json:"sha256"`
	ItemID        *int64        `json:"item_id,omitempty"`
	ItemNumber    int64         `json:"item_number,omitempty"`
	Item          *ArtifactItem `json:"item,omitempty"`
	UploadedBy    *int64        `json:"uploaded_by,omitempty"`
	UploaderName  string        `json:"uploader_name,omitempty"`
	UploaderEmoji string        `json:"uploader_emoji,omitempty"`
	UpdatedBy     *int64        `json:"updated_by,omitempty"`
	UpdaterName   string        `json:"updater_name,omitempty"`
	UpdaterEmoji  string        `json:"updater_emoji,omitempty"`
	CreatedAt     int64         `json:"created_at"`
	UpdatedAt     int64         `json:"updated_at"`
	Version       int64         `json:"version"`
	URL           string        `json:"url"`
	RawURL        string        `json:"raw_url"`
}

// ArtifactInput holds the fields for creating an artifact.
type ArtifactInput struct {
	Name   string
	Title  string
	Data   []byte
	ItemID *int64
}

// ArtifactPatch holds in-place updates for an existing artifact.
// A nil field leaves that attribute unchanged; ItemID pointing to 0 unlinks it.
type ArtifactPatch struct {
	Title  *string
	ItemID *int64
	Data   *[]byte
}

// ArtifactURL returns the SPA viewer permalink for an artifact.
func ArtifactURL(wsID, name string) string {
	return "/w/" + wsID + "/artifacts/" + url.PathEscape(name)
}

// ArtifactRawURL returns the API endpoint serving an artifact's raw bytes.
func ArtifactRawURL(wsID, name string) string {
	return "/api/workspaces/" + wsID + "/artifacts/" + url.PathEscape(name)
}

func (a *Artifact) fill() {
	a.URL = ArtifactURL(a.WorkspaceID, a.Name)
	a.RawURL = ArtifactRawURL(a.WorkspaceID, a.Name)
}

// ValidArtifactName validates and trims an artifact filename, rejecting empty
// names, names over 120 chars, path separators, '..', or control characters.
func ValidArtifactName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("%w: artifact name is required", ErrInvalid)
	}
	if !utf8.ValidString(name) {
		return "", fmt.Errorf("%w: artifact name must be valid UTF-8", ErrInvalid)
	}
	if len(name) > MaxArtifactNameLen || utf8.RuneCountInString(name) > MaxArtifactNameLen {
		return "", fmt.Errorf("%w: artifact name can be at most %d characters", ErrInvalid, MaxArtifactNameLen)
	}
	if name == "." || strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("%w: artifact name %q cannot contain path separators or '..'", ErrInvalid, name)
	}
	if strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("%w: artifact name cannot contain control characters", ErrInvalid)
	}
	return name, nil
}

func cleanArtifactTitle(title, fallbackName string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return fallbackName, nil
	}
	if strings.IndexFunc(title, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("%w: artifact title must be a single line without control characters", ErrInvalid)
	}
	if utf8.RuneCountInString(title) > 200 {
		return "", fmt.Errorf("%w: artifact title can be at most 200 characters", ErrInvalid)
	}
	return title, nil
}

// DetectArtifactMimeType infers an artifact's MIME type from its extension,
// falling back to http.DetectContentType on its bytes.
func DetectArtifactMimeType(name string, data []byte) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown", ".mdx":
		return "text/markdown; charset=utf-8"
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".json", ".jsonl", ".geojson":
		return "application/json; charset=utf-8"
	case ".txt", ".csv", ".tsv", ".log", ".yaml", ".yml", ".toml", ".ini", ".cfg", ".conf",
		".xml", ".sql", ".css", ".scss", ".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx",
		".go", ".py", ".rb", ".rs", ".c", ".cc", ".cpp", ".h", ".hpp", ".java", ".kt",
		".swift", ".sh", ".bash", ".zsh", ".diff", ".patch":
		return "text/plain; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".ico":
		return "image/x-icon"
	case ".pdf":
		return "application/pdf"
	}
	if len(data) == 0 {
		return "application/octet-stream"
	}
	return http.DetectContentType(data)
}

func artifactSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validateArtifactBytes(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("%w: artifact content cannot be empty", ErrInvalid)
	}
	if len(data) > MaxArtifactBytes {
		return fmt.Errorf("%w: artifacts can be at most %d MiB", ErrTooLarge, MaxArtifactBytes>>20)
	}
	return nil
}

const selectArtifactsMeta = `SELECT a.id, a.workspace_id, a.name, a.title, a.mime_type, a.size, a.sha256,
	a.item_id, a.uploaded_by, a.updated_by, a.created_at, a.updated_at, a.version,
	up.name, up.emoji,
	ed.name, ed.emoji,
	COALESCE(i.number, a.item_id), i.title, i.status
	FROM artifacts a
	LEFT JOIN participants up ON up.id = a.uploaded_by
	LEFT JOIN participants ed ON ed.id = a.updated_by
	LEFT JOIN items i ON i.id = a.item_id`

func scanArtifact(row interface{ Scan(...any) error }, extra ...any) (*Artifact, error) {
	var a Artifact
	var itemID, itemNum, uploadedBy, updatedBy sql.NullInt64
	var uploaderName, uploaderEmoji, updaterName, updaterEmoji, itemTitle, itemStatus sql.NullString
	dest := append([]any{
		&a.ID, &a.WorkspaceID, &a.Name, &a.Title, &a.MimeType, &a.Size, &a.SHA256,
		&itemID, &uploadedBy, &updatedBy, &a.CreatedAt, &a.UpdatedAt, &a.Version,
		&uploaderName, &uploaderEmoji,
		&updaterName, &updaterEmoji,
		&itemNum, &itemTitle, &itemStatus,
	}, extra...)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if itemID.Valid {
		a.ItemID = &itemID.Int64
		a.ItemNumber = itemNum.Int64
		a.Item = &ArtifactItem{ID: itemID.Int64, Number: itemNum.Int64, Title: itemTitle.String, Status: itemStatus.String}
	}
	if uploadedBy.Valid {
		a.UploadedBy = &uploadedBy.Int64
		a.UploaderName = uploaderName.String
		a.UploaderEmoji = uploaderEmoji.String
	}
	if updatedBy.Valid {
		a.UpdatedBy = &updatedBy.Int64
		a.UpdaterName = updaterName.String
		a.UpdaterEmoji = updaterEmoji.String
	}
	a.fill()
	return &a, nil
}

func scanArtifacts(rows *sql.Rows, err error) ([]Artifact, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Artifact{}
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func getArtifactMetaTx(t *tx, wsID, name string) (*Artifact, error) {
	a, err := scanArtifact(t.QueryRow(selectArtifactsMeta+` WHERE a.workspace_id = ? AND lower(a.name) = lower(?)`, wsID, name))
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("artifact %q: %w", name, ErrNotFound)
	}
	return a, err
}

// AddArtifact creates a new artifact in actor's workspace and records
// artifact.created. Returns ErrConflict if an artifact with the same
// case-insensitive name already exists in the workspace.
func (s *Store) AddArtifact(ctx context.Context, actor *Participant, in ArtifactInput) (*Artifact, []Event, error) {
	name, err := ValidArtifactName(in.Name)
	if err != nil {
		return nil, nil, err
	}
	title, err := cleanArtifactTitle(in.Title, name)
	if err != nil {
		return nil, nil, err
	}
	if err := validateArtifactBytes(in.Data); err != nil {
		return nil, nil, err
	}
	var itemID *int64
	if in.ItemID != nil && *in.ItemID != 0 {
		if *in.ItemID < 0 {
			return nil, nil, fmt.Errorf("%w: item_id must be positive", ErrInvalid)
		}
		v := *in.ItemID
		itemID = &v
	}
	mimeType := DetectArtifactMimeType(name, in.Data)
	sum := artifactSHA256(in.Data)

	var out *Artifact
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var existing string
		err := t.QueryRow(`SELECT name FROM artifacts WHERE workspace_id = ? AND lower(name) = lower(?)`, actor.WorkspaceID, name).Scan(&existing)
		if err == nil {
			return fmt.Errorf("%w: artifact %q already exists in this workspace (use `unison artifact update %s` to replace it)", ErrConflict, existing, existing)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var resolvedItemID *int64
		if itemID != nil {
			it, err := getItem(t, actor.WorkspaceID, *itemID)
			if errors.Is(err, ErrNotFound) {
				return fmt.Errorf("%w: there is no backlog item #%d in this workspace", ErrInvalid, *itemID)
			} else if err != nil {
				return err
			}
			resolvedItemID = &it.ID
		}
		_, err = t.insert(`INSERT INTO artifacts (workspace_id, name, title, mime_type, size, sha256, data, item_id, uploaded_by, updated_by, created_at, updated_at, version)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`, "id",
			actor.WorkspaceID, name, title, mimeType, int64(len(in.Data)), sum, in.Data, resolvedItemID, actor.ID, actor.ID, log.now, log.now)
		if err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("%w: artifact %q already exists in this workspace (use `unison artifact update %s` to replace it)", ErrConflict, name, name)
			}
			return err
		}
		out, err = getArtifactMetaTx(t, actor.WorkspaceID, name)
		if err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "artifact.created", actor.ID, out)
	})
	if err != nil {
		return nil, nil, err
	}
	return out, evs, nil
}

// UpdateArtifact updates an existing artifact in-place by name, keeping its
// exact name and permalink, incrementing version, and recording artifact.updated.
func (s *Store) UpdateArtifact(ctx context.Context, actor *Participant, name string, p ArtifactPatch) (*Artifact, []Event, error) {
	cleanName, err := ValidArtifactName(name)
	if err != nil {
		return nil, nil, err
	}
	if p.Data != nil {
		if err := validateArtifactBytes(*p.Data); err != nil {
			return nil, nil, err
		}
	}
	if p.Data == nil && p.Title == nil && p.ItemID == nil {
		return nil, nil, fmt.Errorf("%w: artifact update requires content, title, or item_id", ErrInvalid)
	}

	var out *Artifact
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		cur, err := getArtifactMetaTx(t, actor.WorkspaceID, cleanName)
		if err != nil {
			return err
		}
		title := cur.Title
		if p.Title != nil {
			title, err = cleanArtifactTitle(*p.Title, cur.Name)
			if err != nil {
				return err
			}
		}
		itemID := cur.ItemID
		if p.ItemID != nil {
			if *p.ItemID == 0 {
				itemID = nil
			} else if *p.ItemID < 0 {
				return fmt.Errorf("%w: item_id must be >= 0", ErrInvalid)
			} else {
				it, err := getItem(t, actor.WorkspaceID, *p.ItemID)
				if errors.Is(err, ErrNotFound) {
					return fmt.Errorf("%w: there is no backlog item #%d in this workspace", ErrInvalid, *p.ItemID)
				} else if err != nil {
					return err
				}
				v := it.ID
				itemID = &v
			}
		}

		nextVersion := cur.Version + 1
		if p.Data != nil {
			data := *p.Data
			mimeType := DetectArtifactMimeType(cur.Name, data)
			sum := artifactSHA256(data)
			if _, err := t.Exec(`UPDATE artifacts SET title = ?, mime_type = ?, size = ?, sha256 = ?, data = ?, item_id = ?, updated_by = ?, updated_at = ?, version = ? WHERE id = ?`,
				title, mimeType, int64(len(data)), sum, data, itemID, actor.ID, log.now, nextVersion, cur.ID); err != nil {
				return err
			}
		} else {
			if _, err := t.Exec(`UPDATE artifacts SET title = ?, item_id = ?, updated_by = ?, updated_at = ?, version = ? WHERE id = ?`,
				title, itemID, actor.ID, log.now, nextVersion, cur.ID); err != nil {
				return err
			}
		}

		out, err = getArtifactMetaTx(t, actor.WorkspaceID, cur.Name)
		if err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "artifact.updated", actor.ID, out)
	})
	if err != nil {
		return nil, nil, err
	}
	return out, evs, nil
}

// ListArtifacts lists artifact metadata in wsID (without raw bytes), ordered
// newest updated_at first, optionally filtered by itemID (> 0, resolved by
// per-workspace number or legacy global ID).
func (s *Store) ListArtifacts(ctx context.Context, wsID string, itemID int64) ([]Artifact, error) {
	if itemID < 0 {
		return nil, fmt.Errorf("%w: item_id must be >= 0", ErrInvalid)
	}
	q := selectArtifactsMeta + ` WHERE a.workspace_id = ?`
	args := []any{wsID}
	if itemID > 0 {
		realID, err := s.ResolveItemID(ctx, wsID, itemID)
		if errors.Is(err, ErrNotFound) {
			return []Artifact{}, nil
		}
		if err != nil {
			return nil, err
		}
		q += ` AND a.item_id = ?`
		args = append(args, realID)
	}
	q += ` ORDER BY a.updated_at DESC, a.id DESC`
	return scanArtifacts(s.db.QueryContext(ctx, q, args...))
}

// GetArtifactMeta returns metadata (without raw bytes) for an artifact by name.
func (s *Store) GetArtifactMeta(ctx context.Context, wsID, name string) (*Artifact, error) {
	cleanName, err := ValidArtifactName(name)
	if err != nil {
		return nil, err
	}
	a, err := scanArtifact(s.db.QueryRowContext(ctx, selectArtifactsMeta+` WHERE a.workspace_id = ? AND lower(a.name) = lower(?)`, wsID, cleanName))
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("artifact %q: %w", cleanName, ErrNotFound)
	}
	return a, err
}

// GetArtifact returns an artifact's metadata and raw bytes by name.
func (s *Store) GetArtifact(ctx context.Context, wsID, name string) (*Artifact, []byte, error) {
	cleanName, err := ValidArtifactName(name)
	if err != nil {
		return nil, nil, err
	}
	q := `SELECT a.id, a.workspace_id, a.name, a.title, a.mime_type, a.size, a.sha256,
		a.item_id, a.uploaded_by, a.updated_by, a.created_at, a.updated_at, a.version,
		up.name, up.emoji,
		ed.name, ed.emoji,
		COALESCE(i.number, a.item_id), i.title, i.status,
		a.data
		FROM artifacts a
		LEFT JOIN participants up ON up.id = a.uploaded_by
		LEFT JOIN participants ed ON ed.id = a.updated_by
		LEFT JOIN items i ON i.id = a.item_id
		WHERE a.workspace_id = ? AND lower(a.name) = lower(?)`
	var data []byte
	a, err := scanArtifact(s.db.QueryRowContext(ctx, q, wsID, cleanName), &data)
	if errors.Is(err, ErrNotFound) {
		return nil, nil, fmt.Errorf("artifact %q: %w", cleanName, ErrNotFound)
	}
	if err != nil {
		return nil, nil, err
	}
	return a, data, nil
}

// DeleteArtifact deletes an artifact by name and records artifact.deleted.
func (s *Store) DeleteArtifact(ctx context.Context, wsID string, actorID int64, name string) ([]Event, error) {
	cleanName, err := ValidArtifactName(name)
	if err != nil {
		return nil, err
	}
	return s.tx(ctx, func(t *tx, log *eventLog) error {
		cur, err := getArtifactMetaTx(t, wsID, cleanName)
		if err != nil {
			return err
		}
		if _, err := t.Exec(`DELETE FROM artifacts WHERE id = ?`, cur.ID); err != nil {
			return err
		}
		return log.add(wsID, "artifact.deleted", actorID, cur)
	})
}

// unlinkArtifacts clears item_id on artifacts referencing a deleted backlog item
// and records artifact.updated for each.
func unlinkArtifacts(t *tx, log *eventLog, actor *Participant, itemID int64) error {
	names, err := queryStrings(t, `SELECT name FROM artifacts WHERE workspace_id = ? AND item_id = ? ORDER BY id`, actor.WorkspaceID, itemID)
	if err != nil || len(names) == 0 {
		return err
	}
	if _, err := t.Exec(`UPDATE artifacts SET item_id = NULL WHERE workspace_id = ? AND item_id = ?`, actor.WorkspaceID, itemID); err != nil {
		return err
	}
	for _, name := range names {
		a, err := getArtifactMetaTx(t, actor.WorkspaceID, name)
		if err != nil {
			return err
		}
		if err := log.add(actor.WorkspaceID, "artifact.updated", actor.ID, a); err != nil {
			return err
		}
	}
	return nil
}
