package server

import (
	"net/http"

	"github.com/digitalex/unison/internal/store"
)

// ---- Ideas (#917) ----

func (s *Server) listIdeas(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListIdeas(r.Context(), r.PathValue("ws"), r.URL.Query().Get("status"))
	s.respond(w, http.StatusOK, list, nil, err)
}

func (s *Server) getIdea(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	d, err := s.store.GetIdea(r.Context(), r.PathValue("ws"), id)
	s.respond(w, http.StatusOK, d, nil, err)
}

func (s *Server) addIdea(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	d, evs, err := s.store.AddIdea(r.Context(), actor(r), req.Title, req.Body)
	s.respond(w, http.StatusCreated, d, evs, err)
}

func (s *Server) updateIdea(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var p store.IdeaPatch
	if !ok || !decode(w, r, &p) {
		return
	}
	d, evs, err := s.store.UpdateIdea(r.Context(), actor(r), id, p)
	s.respond(w, http.StatusOK, d, evs, err)
}

func (s *Server) deleteIdea(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	evs, err := s.store.DeleteIdea(r.Context(), actor(r), id)
	s.respond(w, http.StatusOK, map[string]any{"deleted": id}, evs, err)
}

// promoteIdea is "Move to backlog": it responds with {"idea", "item"}.
func (s *Server) promoteIdea(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var req struct {
		Position int `json:"position"`
	}
	if !ok || !decode(w, r, &req) {
		return
	}
	out, evs, err := s.store.PromoteIdea(r.Context(), actor(r), id, req.Position)
	if err == nil {
		s.track(eventItemCreated, participantActor(actor(r)))
	}
	s.respond(w, http.StatusOK, out, evs, err)
}

func (s *Server) listIdeaComments(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	list, err := s.store.ListIdeaComments(r.Context(), r.PathValue("ws"), id)
	s.respond(w, http.StatusOK, list, nil, err)
}

func (s *Server) addIdeaComment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var req struct {
		Body string `json:"body"`
	}
	if !ok || !decode(w, r, &req) {
		return
	}
	c, evs, err := s.store.AddIdeaComment(r.Context(), actor(r), id, req.Body)
	s.respond(w, http.StatusCreated, c, evs, err)
}

func (s *Server) deleteIdeaComment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	cid, ok := pathInt(w, r, "cid")
	if !ok {
		return
	}
	evs, err := s.store.DeleteIdeaComment(r.Context(), actor(r), id, cid)
	s.respond(w, http.StatusOK, map[string]any{"deleted": cid}, evs, err)
}
