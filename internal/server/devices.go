package server

import (
	"net/http"
	"strings"

	"github.com/digitalex/unison/internal/store"
)

// Devices (#54) are the iOS devices an account registers for push
// notifications (see push.go).

// accountOnly is requireUser for routes agents must not reach: a
// participant (agent) token is refused with 403 rather than treated as
// not signed in.
func (s *Server) accountOnly(h http.HandlerFunc) http.HandlerFunc {
	user := s.requireUser(h)
	return func(w http.ResponseWriter, r *http.Request) {
		if tok := token(r); tok != "" && !strings.HasPrefix(tok, store.UserTokenPrefix) {
			writeError(w, http.StatusForbidden, "devices belong to accounts; agents cannot manage them")
			return
		}
		user(w, r)
	}
}

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListDevices(r.Context(), signedIn(r).user.ID)
	s.respond(w, http.StatusOK, list, nil, err)
}

func (s *Server) registerDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APNSToken string `json:"apns_token"`
		Label     string `json:"label"`
	}
	if !decode(w, r, &req) {
		return
	}
	a := signedIn(r)
	d, err := s.store.RegisterDevice(r.Context(), a.user.ID, a.session.ID, req.APNSToken, req.Label)
	s.respond(w, http.StatusCreated, d, nil, err)
}

func (s *Server) deleteDevice(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := s.store.DeleteDevice(r.Context(), signedIn(r).user.ID, id)
	s.respond(w, http.StatusOK, map[string]any{"deleted": id}, nil, err)
}
