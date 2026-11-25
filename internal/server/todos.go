package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/digitalex/unison/internal/store"
)

// listMyTodos handles GET /api/me/todos: cross-workspace TODOs assigned to the
// signed-in user across all workspaces they are a member of.
func (s *Server) listMyTodos(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	list, err := s.store.ListUserTodos(r.Context(), signedIn(r).user.ID, status)
	s.respond(w, http.StatusOK, list, nil, err)
}

// listTodos handles GET /api/workspaces/{ws}/todos.
func (s *Server) listTodos(w http.ResponseWriter, r *http.Request) {
	ws := r.PathValue("ws")
	status := r.URL.Query().Get("status")
	assigneeParam := strings.TrimSpace(r.URL.Query().Get("assignee"))

	var assigneeID int64
	switch {
	case assigneeParam == "":
	case strings.EqualFold(assigneeParam, "me"):
		p := viewer(r).participant
		if p == nil {
			s.respond(w, http.StatusOK, []store.Todo{}, nil, nil)
			return
		}
		assigneeID = p.ID
	default:
		if id, err := strconv.ParseInt(assigneeParam, 10, 64); err == nil && id > 0 {
			assigneeID = id
		} else {
			id, err := s.store.ResolveParticipantID(r.Context(), ws, assigneeParam)
			if err != nil {
				storeError(w, err)
				return
			}
			assigneeID = id
		}
	}

	list, err := s.store.ListTodos(r.Context(), ws, status, assigneeID)
	s.respond(w, http.StatusOK, list, nil, err)
}

// createTodo handles POST /api/workspaces/{ws}/todos.
func (s *Server) createTodo(w http.ResponseWriter, r *http.Request) {
	var req store.TodoInput
	if !decode(w, r, &req) {
		return
	}
	td, evs, err := s.store.CreateTodo(r.Context(), actor(r), req)
	s.respond(w, http.StatusCreated, td, evs, err)
}

// doneTodo handles POST /api/workspaces/{ws}/todos/{id}/done.
func (s *Server) doneTodo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var req struct {
		Note string `json:"note"`
	}
	if !ok || !decode(w, r, &req) {
		return
	}
	td, evs, err := s.store.DoneTodo(r.Context(), actor(r), id, req.Note)
	s.respond(w, http.StatusOK, td, evs, err)
}

// reopenTodo handles POST /api/workspaces/{ws}/todos/{id}/reopen.
func (s *Server) reopenTodo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	td, evs, err := s.store.ReopenTodo(r.Context(), actor(r), id)
	s.respond(w, http.StatusOK, td, evs, err)
}

// snoozeTodo handles POST /api/workspaces/{ws}/todos/{id}/snooze.
func (s *Server) snoozeTodo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var req struct {
		Duration string `json:"duration"`
	}
	if !ok || !decode(w, r, &req) {
		return
	}
	td, evs, err := s.store.SnoozeTodo(r.Context(), actor(r), id, req.Duration)
	s.respond(w, http.StatusOK, td, evs, err)
}

// deleteTodo handles DELETE /api/workspaces/{ws}/todos/{id}.
func (s *Server) deleteTodo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	evs, err := s.store.DeleteTodo(r.Context(), actor(r), id)
	s.respond(w, http.StatusOK, map[string]any{"deleted": id}, evs, err)
}
