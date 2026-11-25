package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/digitalex/unison/internal/store"
)

// ---- Artifacts (#918) ----

const artifactCSP = "sandbox; default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'"

type artifactWithContent struct {
	*store.Artifact
	Content string `json:"content,omitempty"`
}

func parseOptionalItemID(raw string) (*int64, error) {
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "#"))
	if raw == "" {
		return nil, nil
	}
	if strings.EqualFold(raw, "none") || raw == "0" {
		zero := int64(0)
		return &zero, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		return nil, errors.New("item_id must be a non-negative integer")
	}
	return &v, nil
}

// listArtifacts returns workspace artifact metadata (optional ?item_id=N filter).
func (s *Server) listArtifacts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rawItem := firstNonEmpty(q.Get("item_id"), q.Get("item"))
	var itemID int64
	if rawItem != "" {
		parsed, err := strconv.ParseInt(strings.TrimPrefix(strings.TrimSpace(rawItem), "#"), 10, 64)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "item_id must be a positive integer")
			return
		}
		itemID = parsed
	}
	list, err := s.store.ListArtifacts(r.Context(), r.PathValue("ws"), itemID)
	s.respond(w, http.StatusOK, list, nil, err)
}

type parsedMultipartArtifact struct {
	hasFile  bool
	data     []byte
	fileName string
	name     *string
	title    *string
	itemID   *string
}

func readMultipartArtifact(r *http.Request) (*parsedMultipartArtifact, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, err
	}
	out := &parsedMultipartArtifact{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		switch part.FormName() {
		case "file":
			data, err := io.ReadAll(part)
			if err != nil {
				return nil, err
			}
			out.hasFile = true
			out.data = data
			out.fileName = part.FileName()
		case "name":
			b, err := io.ReadAll(io.LimitReader(part, 1024))
			if err != nil {
				return nil, err
			}
			v := string(b)
			out.name = &v
		case "title":
			b, err := io.ReadAll(io.LimitReader(part, 2048))
			if err != nil {
				return nil, err
			}
			v := string(b)
			out.title = &v
		case "item_id", "item":
			b, err := io.ReadAll(io.LimitReader(part, 128))
			if err != nil {
				return nil, err
			}
			v := string(b)
			out.itemID = &v
		}
	}
}

func handleUploadErr(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("artifacts can be at most %d MiB", store.MaxArtifactBytes>>20))
		return true
	}
	writeError(w, http.StatusBadRequest, err.Error())
	return true
}

// uploadArtifact creates a new artifact from a raw body or multipart/form-data.
func (s *Server) uploadArtifact(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, store.MaxArtifactBytes+64<<10)
	q := r.URL.Query()
	name := q.Get("name")
	title := q.Get("title")
	rawItem := firstNonEmpty(q.Get("item_id"), q.Get("item"))

	var data []byte
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt == "multipart/form-data" {
		mp, err := readMultipartArtifact(r)
		if handleUploadErr(w, err) {
			return
		}
		if !mp.hasFile {
			writeError(w, http.StatusBadRequest, `multipart upload has no "file" field`)
			return
		}
		data = mp.data
		if name == "" && mp.name != nil && strings.TrimSpace(*mp.name) != "" {
			name = *mp.name
		}
		if name == "" {
			name = mp.fileName
		}
		if title == "" && mp.title != nil {
			title = *mp.title
		}
		if rawItem == "" && mp.itemID != nil {
			rawItem = *mp.itemID
		}
	} else {
		var err error
		data, err = io.ReadAll(r.Body)
		if handleUploadErr(w, err) {
			return
		}
	}

	if len(data) > store.MaxArtifactBytes {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("artifacts can be at most %d MiB", store.MaxArtifactBytes>>20))
		return
	}
	itemID, err := parseOptionalItemID(rawItem)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if itemID != nil && *itemID == 0 {
		itemID = nil
	}
	art, evs, err := s.store.AddArtifact(r.Context(), actor(r), store.ArtifactInput{
		Name:   name,
		Title:  title,
		Data:   data,
		ItemID: itemID,
	})
	s.respond(w, http.StatusCreated, art, evs, err)
}

func isTruthyParam(v string) bool {
	return v == "1" || strings.EqualFold(v, "true")
}

// ifNoneMatch reports whether any ETag in If-None-Match headers matches etag
// (which is a strong quoted ETag like `"e3b0c442..."`).
func ifNoneMatch(headers []string, etag string) bool {
	for _, raw := range headers {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if raw == "*" {
			return true
		}
		for _, part := range strings.Split(raw, ",") {
			cand := strings.TrimSpace(part)
			if strings.HasPrefix(cand, "W/") || strings.HasPrefix(cand, "w/") {
				cand = strings.TrimSpace(cand[2:])
			}
			if cand == etag {
				return true
			}
		}
	}
	return false
}

// getArtifact serves an artifact's raw content (with ETag + sandbox CSP) or,
// when ?meta=1 is passed, its JSON metadata.
func (s *Server) getArtifact(w http.ResponseWriter, r *http.Request) {
	wsID, file := r.PathValue("ws"), r.PathValue("file")
	q := r.URL.Query()
	if isTruthyParam(q.Get("meta")) {
		if isTruthyParam(q.Get("content")) {
			a, data, err := s.store.GetArtifact(r.Context(), wsID, file)
			if err != nil {
				storeError(w, err)
				return
			}
			out := artifactWithContent{Artifact: a}
			if utf8.Valid(data) && !bytes.Contains(data, []byte{0}) {
				out.Content = string(data)
			}
			writeJSON(w, http.StatusOK, out)
			return
		}
		a, err := s.store.GetArtifactMeta(r.Context(), wsID, file)
		s.respond(w, http.StatusOK, a, nil, err)
		return
	}

	a, data, err := s.store.GetArtifact(r.Context(), wsID, file)
	if err != nil {
		storeError(w, err)
		return
	}
	etag := `"` + a.SHA256 + `"`
	h := w.Header()
	h.Set("Cache-Control", "private, no-cache")
	h.Set("ETag", etag)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", artifactCSP)
	h.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": a.Name}))
	h.Set("Content-Type", a.MimeType)

	if ifNoneMatch(r.Header.Values("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Length", strconv.Itoa(len(data)))
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

// replaceArtifact replaces an artifact's content in-place (PUT), keeping its
// permalink, incrementing version, and emitting artifact.updated.
func (s *Server) replaceArtifact(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, store.MaxArtifactBytes+64<<10)
	q := r.URL.Query()

	var patch store.ArtifactPatch
	if q.Has("title") {
		v := q.Get("title")
		patch.Title = &v
	}
	var rawItem string
	hasItem := false
	if q.Has("item_id") {
		rawItem, hasItem = q.Get("item_id"), true
	} else if q.Has("item") {
		rawItem, hasItem = q.Get("item"), true
	}

	var data []byte
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt == "multipart/form-data" {
		mp, err := readMultipartArtifact(r)
		if handleUploadErr(w, err) {
			return
		}
		if !mp.hasFile {
			writeError(w, http.StatusBadRequest, `multipart upload has no "file" field`)
			return
		}
		data = mp.data
		if patch.Title == nil && mp.title != nil {
			patch.Title = mp.title
		}
		if !hasItem && mp.itemID != nil {
			rawItem, hasItem = *mp.itemID, true
		}
	} else {
		var err error
		data, err = io.ReadAll(r.Body)
		if handleUploadErr(w, err) {
			return
		}
	}

	if len(data) > store.MaxArtifactBytes {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("artifacts can be at most %d MiB", store.MaxArtifactBytes>>20))
		return
	}
	patch.Data = &data
	if hasItem {
		parsed, err := parseOptionalItemID(rawItem)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if parsed == nil {
			zero := int64(0)
			parsed = &zero
		}
		patch.ItemID = parsed
	}

	art, evs, err := s.store.UpdateArtifact(r.Context(), actor(r), r.PathValue("file"), patch)
	s.respond(w, http.StatusOK, art, evs, err)
}

// patchArtifact updates an artifact's metadata and/or content in-place (PATCH),
// keeping its permalink and emitting artifact.updated.
func (s *Server) patchArtifact(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, store.MaxArtifactBytes+2<<20)
	q := r.URL.Query()
	var patch store.ArtifactPatch
	if q.Has("title") {
		v := q.Get("title")
		patch.Title = &v
	}
	if q.Has("item_id") || q.Has("item") {
		rawItem := q.Get("item_id")
		if !q.Has("item_id") {
			rawItem = q.Get("item")
		}
		parsed, err := parseOptionalItemID(rawItem)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if parsed == nil {
			zero := int64(0)
			parsed = &zero
		}
		patch.ItemID = parsed
	}

	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt == "multipart/form-data" {
		mp, err := readMultipartArtifact(r)
		if handleUploadErr(w, err) {
			return
		}
		if mp.hasFile {
			if len(mp.data) > store.MaxArtifactBytes {
				writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("artifacts can be at most %d MiB", store.MaxArtifactBytes>>20))
				return
			}
			patch.Data = &mp.data
		}
		if patch.Title == nil && mp.title != nil {
			patch.Title = mp.title
		}
		if patch.ItemID == nil && mp.itemID != nil {
			parsed, err := parseOptionalItemID(*mp.itemID)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			if parsed == nil {
				zero := int64(0)
				parsed = &zero
			}
			patch.ItemID = parsed
		}
	} else {
		var req struct {
			Title         *string         `json:"title"`
			ItemID        json.RawMessage `json:"item_id"`
			Content       *string         `json:"content"`
			ContentBase64 *string         `json:"content_base64"`
		}
		if !decode(w, r, &req) {
			return
		}
		if req.Title != nil {
			patch.Title = req.Title
		}
		if len(req.ItemID) > 0 {
			trimmed := strings.TrimSpace(string(req.ItemID))
			if trimmed == "null" {
				zero := int64(0)
				patch.ItemID = &zero
			} else {
				var idVal int64
				if err := json.Unmarshal(req.ItemID, &idVal); err != nil {
					var strVal string
					if json.Unmarshal(req.ItemID, &strVal) == nil {
						parsed, pErr := parseOptionalItemID(strVal)
						if pErr != nil {
							writeError(w, http.StatusBadRequest, pErr.Error())
							return
						}
						if parsed == nil {
							zero := int64(0)
							parsed = &zero
						}
						patch.ItemID = parsed
					} else {
						writeError(w, http.StatusBadRequest, "invalid item_id")
						return
					}
				} else {
					patch.ItemID = &idVal
				}
			}
		}
		if req.ContentBase64 != nil {
			decoded, err := base64.StdEncoding.DecodeString(*req.ContentBase64)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid content_base64: "+err.Error())
				return
			}
			if len(decoded) > store.MaxArtifactBytes {
				writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("artifacts can be at most %d MiB", store.MaxArtifactBytes>>20))
				return
			}
			patch.Data = &decoded
		} else if req.Content != nil {
			raw := []byte(*req.Content)
			if len(raw) > store.MaxArtifactBytes {
				writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("artifacts can be at most %d MiB", store.MaxArtifactBytes>>20))
				return
			}
			patch.Data = &raw
		}
	}

	art, evs, err := s.store.UpdateArtifact(r.Context(), actor(r), r.PathValue("file"), patch)
	s.respond(w, http.StatusOK, art, evs, err)
}

// deleteArtifact deletes an artifact and emits artifact.deleted.
func (s *Server) deleteArtifact(w http.ResponseWriter, r *http.Request) {
	file := r.PathValue("file")
	evs, err := s.store.DeleteArtifact(r.Context(), r.PathValue("ws"), actor(r).ID, file)
	s.respond(w, http.StatusOK, map[string]any{"deleted": file}, evs, err)
}
