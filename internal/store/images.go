package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// MaxImageBytes is the largest image a workspace accepts (#292).
const MaxImageBytes = 5 << 20

// Image is a screenshot or other picture uploaded to a workspace, to show
// inline in Markdown (chat, backlog items, comments). Its bytes are served by
// GET /api/workspaces/{ws}/images/{id} (URL) and never sent in JSON.
type Image struct {
	ID          int64  `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	UploadedBy  int64  `json:"uploaded_by"`
	CreatedAt   int64  `json:"created_at"`
	URL         string `json:"url"`
	Markdown    string `json:"markdown"`
}

// ImageURL is the workspace-relative path an image is served from.
func ImageURL(wsID string, id int64) string {
	return fmt.Sprintf("/api/workspaces/%s/images/%d", wsID, id)
}

// sniffImage returns the content type of a PNG, JPEG, GIF or WebP image by
// its magic bytes, or "" for anything else (#292). The uploader's claimed
// type is never trusted.
func sniffImage(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(b, []byte("\xff\xd8\xff")):
		return "image/jpeg"
	case bytes.HasPrefix(b, []byte("GIF87a")), bytes.HasPrefix(b, []byte("GIF89a")):
		return "image/gif"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}

// imageName cleans an uploaded file name for use as Markdown alt text: the
// base name only, without characters that would break ![name](url), at most
// 100 characters. Empty becomes "image".
func imageName(raw string) string {
	n := strings.TrimSpace(raw)
	if i := strings.LastIndexAny(n, `/\`); i >= 0 {
		n = n[i+1:]
	}
	n = strings.Map(func(r rune) rune {
		switch {
		case r < ' ', r == 0x7f:
			return -1
		case strings.ContainsRune("[]()<>`", r):
			return -1
		}
		return r
	}, n)
	n = strings.TrimSpace(truncate(n, 100))
	if n == "" || n == "." {
		return "image"
	}
	return n
}

func (im *Image) fill() {
	im.URL = ImageURL(im.WorkspaceID, im.ID)
	im.Markdown = fmt.Sprintf("![%s](%s)", im.Name, im.URL)
}

// AddImage stores an image in the actor's workspace and records
// image.uploaded. data must be a PNG, JPEG, GIF or WebP of at most
// MaxImageBytes.
func (s *Store) AddImage(ctx context.Context, actor *Participant, name string, data []byte) (*Image, []Event, error) {
	if len(data) == 0 {
		return nil, nil, fmt.Errorf("%w: the image is empty", ErrInvalid)
	}
	if len(data) > MaxImageBytes {
		return nil, nil, fmt.Errorf("%w: images can be at most %d MiB", ErrInvalid, MaxImageBytes>>20)
	}
	ct := sniffImage(data)
	if ct == "" {
		return nil, nil, fmt.Errorf("%w: only PNG, JPEG, GIF and WebP images can be uploaded", ErrInvalid)
	}
	im := &Image{WorkspaceID: actor.WorkspaceID, Name: imageName(name), ContentType: ct, Size: int64(len(data)), UploadedBy: actor.ID}
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		im.CreatedAt = log.now
		var err error
		if im.ID, err = t.insert(`INSERT INTO images (workspace_id, name, content_type, size, data, uploaded_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "id",
			im.WorkspaceID, im.Name, im.ContentType, im.Size, data, im.UploadedBy, im.CreatedAt); err != nil {
			return err
		}
		im.fill()
		return log.add(actor.WorkspaceID, "image.uploaded", actor.ID, im)
	})
	if err != nil {
		return nil, nil, err
	}
	return im, evs, nil
}

// GetImage returns an image in workspace wsID and its bytes.
func (s *Store) GetImage(ctx context.Context, wsID string, id int64) (*Image, []byte, error) {
	im := &Image{}
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT id, workspace_id, name, content_type, size, uploaded_by, created_at, data
		FROM images WHERE workspace_id = ? AND id = ?`, wsID, id).Scan(
		&im.ID, &im.WorkspaceID, &im.Name, &im.ContentType, &im.Size, &im.UploadedBy, &im.CreatedAt, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, fmt.Errorf("image %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, nil, err
	}
	im.fill()
	return im, data, nil
}
