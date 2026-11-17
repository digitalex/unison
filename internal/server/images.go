package server

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/digitalex/unison/internal/store"
)

// ---- Images (#292) ----

// uploadImage stores an image: the raw bytes as the body (name from
// ?name=), or a multipart/form-data "file" field (name from ?name= or the
// file name). It returns the image with its URL and a Markdown snippet.
func (s *Server) uploadImage(w http.ResponseWriter, r *http.Request) {
	// Room for the multipart envelope around a maximal image; the store
	// enforces the image limit itself.
	r.Body = http.MaxBytesReader(w, r.Body, store.MaxImageBytes+64<<10)
	name := r.URL.Query().Get("name")
	var data []byte
	var err error
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt == "multipart/form-data" {
		data, name, err = multipartImage(r, name)
	} else {
		data, err = io.ReadAll(r.Body)
	}
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("images can be at most %d MiB", store.MaxImageBytes>>20))
		return
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	im, evs, err := s.store.AddImage(r.Context(), actor(r), name, data)
	s.respond(w, http.StatusCreated, im, evs, err)
}

func multipartImage(r *http.Request, name string) ([]byte, string, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, "", err
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			return nil, "", errors.New(`multipart upload has no "file" field`)
		}
		if err != nil {
			return nil, "", err
		}
		if part.FormName() != "file" {
			continue
		}
		if name == "" {
			name = part.FileName()
		}
		data, err := io.ReadAll(part)
		return data, name, err
	}
}

// getImage serves an image's bytes. Images are user content: the type is
// the sniffed one, browsers must not guess another (nosniff), and the CSP
// keeps anything opened directly from running script.
func (s *Server) getImage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	im, data, err := s.store.GetImage(r.Context(), r.PathValue("ws"), id)
	if err != nil {
		storeError(w, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", im.ContentType)
	h.Set("Content-Length", strconv.Itoa(len(data)))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox")
	h.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": im.Name}))
	// An image never changes; private because workspaces are.
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Write(data)
}
