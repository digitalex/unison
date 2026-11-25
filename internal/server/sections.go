package server

import (
	"net/http"

	"github.com/digitalex/unison/internal/store"
)

// listSections handles GET /api/workspaces/{ws}/sections.
func (s *Server) listSections(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListSections(r.Context(), r.PathValue("ws"))
	s.respond(w, http.StatusOK, list, nil, err)
}

// addSection handles POST /api/workspaces/{ws}/sections.
func (s *Server) addSection(w http.ResponseWriter, r *http.Request) {
	var req store.SectionInput
	if !decode(w, r, &req) {
		return
	}
	sec, evs, err := s.store.AddSection(r.Context(), actor(r), req)
	s.respond(w, http.StatusCreated, sec, evs, err)
}

// editSection handles PATCH /api/workspaces/{ws}/sections/{id} ({id} accepts numeric ID or slug).
func (s *Server) editSection(w http.ResponseWriter, r *http.Request) {
	var p store.SectionPatch
	if !decode(w, r, &p) {
		return
	}
	sec, evs, err := s.store.UpdateSection(r.Context(), actor(r), r.PathValue("id"), p)
	s.respond(w, http.StatusOK, sec, evs, err)
}

// moveSection handles POST /api/workspaces/{ws}/sections/{id}/move.
func (s *Server) moveSection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Position int `json:"position"`
	}
	if !decode(w, r, &req) {
		return
	}
	sec, evs, err := s.store.MoveSection(r.Context(), actor(r), r.PathValue("id"), req.Position)
	s.respond(w, http.StatusOK, sec, evs, err)
}

// deleteSection handles DELETE /api/workspaces/{ws}/sections/{id}.
func (s *Server) deleteSection(w http.ResponseWriter, r *http.Request) {
	sec, evs, err := s.store.DeleteSection(r.Context(), actor(r), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	s.respond(w, http.StatusOK, map[string]any{"deleted": sec.ID, "slug": sec.Slug}, evs, nil)
}
