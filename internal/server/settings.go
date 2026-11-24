package server

import (
	"encoding/json"
	"net/http"
)

// Personal settings (GET/PUT /api/me/settings; `unison me defaults`): the
// signed-in person's own preferences, such as the integrations to set up for
// every new workspace they create. Only that person reads or changes them,
// never their agents.

// requirePerson is requireUser for endpoints only the person may use: it
// refuses agents (which call with a participant token) with 403.
func (s *Server) requirePerson(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if participantToken(r) != "" {
			writeError(w, http.StatusForbidden, "agents cannot read or change a person's settings; only the person can, in the web UI or with `unison me defaults`")
			return
		}
		s.requireUser(h)(w, r)
	}
}

// getMySettings returns the caller's settings, each at its default unless
// they set it.
func (s *Server) getMySettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetUserSettings(r.Context(), signedIn(r).user.ID)
	s.respond(w, http.StatusOK, settings, nil, err)
}

// putMySettings sets the settings in the body (setting key to value; keys left
// out stay as they are) and returns all of the caller's settings. 400 for an
// unknown key or a malformed value, changing nothing.
func (s *Server) putMySettings(w http.ResponseWriter, r *http.Request) {
	var values map[string]json.RawMessage
	if !decode(w, r, &values) {
		return
	}
	settings, err := s.store.SetUserSettings(r.Context(), signedIn(r).user.ID, values)
	s.respond(w, http.StatusOK, settings, nil, err)
}
