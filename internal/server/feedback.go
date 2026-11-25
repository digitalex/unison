package server

import (
	"net/http"

	"github.com/digitalex/unison/internal/store"
)

// ---- Feedback (#281) ----

func (s *Server) listFeedback(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListFeedback(r.Context(), r.PathValue("ws"), r.URL.Query().Get("status"))
	s.respond(w, http.StatusOK, list, nil, err)
}

func (s *Server) getFeedback(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	f, err := s.store.GetFeedback(r.Context(), r.PathValue("ws"), id)
	s.respond(w, http.StatusOK, f, nil, err)
}

func (s *Server) createFeedback(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind     string `json:"kind"`
		Type     string `json:"type"`
		Severity string `json:"severity"`
		Title    string `json:"title"`
		Body     string `json:"body"`
		ImageURL string `json:"image_url"`
		PageURL  string `json:"page_url"`
	}
	if !decode(w, r, &req) {
		return
	}
	kind := req.Kind
	if kind == "" {
		kind = req.Type
	}
	f, evs, err := s.store.CreateFeedback(r.Context(), actor(r), store.FeedbackInput{
		Kind:     kind,
		Severity: req.Severity,
		Title:    req.Title,
		Body:     req.Body,
		ImageURL: req.ImageURL,
		PageURL:  req.PageURL,
	})
	s.respond(w, http.StatusCreated, f, evs, err)
}

func (s *Server) updateFeedback(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var req struct {
		store.FeedbackPatch
		Type *string `json:"type"`
	}
	if !ok || !decode(w, r, &req) {
		return
	}
	if req.Kind == nil && req.Type != nil {
		req.Kind = req.Type
	}
	f, evs, err := s.store.UpdateFeedback(r.Context(), actor(r), id, req.FeedbackPatch)
	s.respond(w, http.StatusOK, f, evs, err)
}

func (s *Server) dismissFeedback(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	f, evs, err := s.store.DismissFeedback(r.Context(), actor(r), id)
	s.respond(w, http.StatusOK, f, evs, err)
}

func (s *Server) reopenFeedback(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	f, evs, err := s.store.ReopenFeedback(r.Context(), actor(r), id)
	s.respond(w, http.StatusOK, f, evs, err)
}

func (s *Server) promoteFeedback(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var req struct {
		Position int `json:"position"`
	}
	if !ok || !decode(w, r, &req) {
		return
	}
	out, evs, err := s.store.PromoteFeedback(r.Context(), actor(r), id, req.Position)
	if err == nil {
		s.track(eventItemCreated, participantActor(actor(r)))
	}
	s.respond(w, http.StatusOK, out, evs, err)
}

func (s *Server) deleteFeedback(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	evs, err := s.store.DeleteFeedback(r.Context(), actor(r), id)
	s.respond(w, http.StatusOK, map[string]any{"deleted": id}, evs, err)
}
