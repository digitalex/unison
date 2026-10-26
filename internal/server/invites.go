package server

import (
	"net/http"
	"net/url"

	"github.com/digitalex/unison/internal/store"
)

// Invites for humans (agents use passes): by email, or by a revocable link.

// inviteURL is an invite link. The token is in the fragment, which browsers
// never send to the server, so it stays out of request logs (the join page
// moves it out of the address bar, and posts it in request bodies only).
func inviteURL(r *http.Request, wsID, token string) string {
	return requestOrigin(r) + "/w/" + wsID + "/join#invite=" + url.QueryEscape(token)
}

func (s *Server) listInvites(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListInvites(r.Context(), r.PathValue("ws"))
	s.respond(w, http.StatusOK, list, nil, err)
}

// createInvite: {"email": "…"} invites an address; {"link": true} creates
// an invite link (returned once, with its URL). Both work for 48 hours; an
// "expires_in" from older clients is ignored.
func (s *Server) createInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
		Link  bool   `json:"link"`
	}
	if !decode(w, r, &req) {
		return
	}
	ws, u := r.PathValue("ws"), signedIn(r).user
	if req.Link {
		inv, token, evs, err := s.store.CreateLinkInvite(r.Context(), ws, u.ID)
		resp := map[string]any{"invite": inv}
		if err == nil {
			resp["token"], resp["url"] = token, inviteURL(r, ws, token)
		}
		s.respond(w, http.StatusCreated, resp, evs, err)
		return
	}
	inv, evs, err := s.store.CreateEmailInvite(r.Context(), ws, u.ID, req.Email)
	s.respond(w, http.StatusCreated, map[string]any{"invite": inv}, evs, err)
}

func (s *Server) revokeInvite(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	inv, evs, err := s.store.RevokeInvite(r.Context(), r.PathValue("ws"), signedIn(r).user.ID, id)
	s.respond(w, http.StatusOK, inv, evs, err)
}

// lookupInvite tells a signed-in visitor of an invite link which workspace
// it is for (the join page). Signed-out visitors learn nothing. The token
// comes in the body ({"token"}), never the URL, to keep it out of logs.
func (s *Server) lookupInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &req) {
		return
	}
	l, err := s.store.LookupInviteLink(r.Context(), req.Token)
	s.respond(w, http.StatusOK, l, nil, err)
}

// doorwayAccess decides whether a request may read a workspace's doorway
// (its name, repositories and canvas): whoever the gate admits, or the
// holder of a valid pass for it (?pass=, not used up by reading).
func (s *Server) doorwayAccess(w http.ResponseWriter, r *http.Request, ws *store.Workspace) (pass string, ok bool) {
	if s.authCfg.Open {
		return "", true
	}
	if pass = r.URL.Query().Get("pass"); pass != "" {
		if err := s.store.CheckPass(r.Context(), ws.ID, pass); err != nil {
			http.Error(w, err.Error()+"\n\nThe general guide is at "+requestOrigin(r)+"/agent.md.", http.StatusUnauthorized)
			return "", false
		}
		return pass, true
	}
	return "", s.authorize(w, r, ws.ID) != nil
}
