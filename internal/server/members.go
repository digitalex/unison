package server

import (
	"net/http"
	"strconv"

	"github.com/digitalex/unison/internal/store"
)

// Membership endpoints: who belongs to a workspace (the gate in gate.go
// admits only members and their agents) and which participants are whose.

// joiningUser returns the signed-in user joining a workspace, or nil when
// nobody is signed in (agents with a pass; a cookie without the CSRF header
// is ignored, so such a join is refused as signed out).
func (s *Server) joiningUser(r *http.Request) *store.User {
	a, err := s.currentUser(r)
	if err != nil || !csrfOK(r, a) {
		return nil
	}
	return a.user
}

func (s *Server) joinAsUser(w http.ResponseWriter, r *http.Request, u *store.User, name, emoji, invite string) {
	var p *store.Participant
	var tok string
	var existing bool
	var evs []store.Event
	var err error
	if s.authCfg.Open {
		p, tok, existing, evs, err = s.store.JoinAsUser(r.Context(), r.PathValue("ws"), u.ID, name, emoji)
	} else {
		p, tok, existing, evs, err = s.store.JoinWorkspace(r.Context(), r.PathValue("ws"), u.ID, name, emoji, invite)
	}
	status := http.StatusCreated
	if existing {
		status = http.StatusOK
	}
	s.respond(w, status, map[string]any{"participant": p, "token": tok, "existing": existing}, evs, err)
}

// meView is GET /me: the participant, plus its account when linked.
type meView struct {
	*store.Participant
	User *meUser `json:"user,omitempty"`
	// OpenActions counts the messages that ask you to act (GET …/actions).
	OpenActions int `json:"open_actions"`
}

type meUser struct {
	ID          int64  `json:"id"`
	DisplayName string `json:"display_name"`
}

func (s *Server) respondMe(w http.ResponseWriter, r *http.Request, p *store.Participant) {
	view := meView{Participant: p}
	var err error
	if view.OpenActions, err = s.store.CountOpenActions(r.Context(), p); err != nil {
		storeError(w, err)
		return
	}
	if p.UserID != 0 {
		u, err := s.store.GetUser(r.Context(), p.UserID)
		if err != nil {
			storeError(w, err)
			return
		}
		view.User = &meUser{ID: u.ID, DisplayName: u.DisplayName}
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.ListMembers(r.Context(), r.PathValue("ws"))
	s.respond(w, http.StatusOK, m, nil, err)
}

func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("user"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "user id must be a number")
		return
	}
	evs, err := s.store.RemoveMember(r.Context(), r.PathValue("ws"), signedIn(r).user.ID, id)
	s.respond(w, http.StatusOK, map[string]any{"removed": id}, evs, err)
}
