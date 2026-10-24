package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/digitalex/unison/internal/store"
)

// Membership endpoints. Nothing here gates access yet: they record who
// belongs to a workspace and which participants are whose.

// joiningUser returns the signed-in user joining a workspace, or nil for an
// anonymous join (agents, or a cookie without the CSRF header, which is
// ignored rather than refused so plain joins behave exactly as before).
func (s *Server) joiningUser(r *http.Request) *store.User {
	a, err := s.currentUser(r)
	if err != nil || !csrfOK(r, a) {
		return nil
	}
	return a.user
}

func (s *Server) joinAsUser(w http.ResponseWriter, r *http.Request, u *store.User, name, emoji string) {
	p, tok, existing, evs, err := s.store.JoinAsUser(r.Context(), r.PathValue("ws"), u.ID, name, emoji)
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
}

type meUser struct {
	ID          int64  `json:"id"`
	DisplayName string `json:"display_name"`
}

func (s *Server) respondMe(w http.ResponseWriter, r *http.Request, p *store.Participant) {
	view := meView{Participant: p}
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

// claim links a participant to the signed-in user. Proof of possession: the
// participant's token, in the body (`participant_token`) or as the bearer
// token when the user is authenticated by the session cookie.
func (s *Server) claim(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ParticipantToken string `json:"participant_token"`
	}
	if !decode(w, r, &req) {
		return
	}
	tok := req.ParticipantToken
	if h := r.Header.Get("Authorization"); tok == "" && strings.HasPrefix(h, "Bearer ") && !strings.HasPrefix(h, "Bearer "+store.UserTokenPrefix) {
		tok = strings.TrimPrefix(h, "Bearer ")
	}
	p, evs, err := s.store.Claim(r.Context(), r.PathValue("ws"), signedIn(r).user.ID, tok)
	s.respond(w, http.StatusOK, p, evs, err)
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
