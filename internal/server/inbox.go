package server

import (
	"net/http"

	"github.com/digitalex/unison/internal/store"
)

// The inbox: what a member must act on. Reading and dismissing act on the
// caller's own inbox (an account's, across all its devices); anyone in the
// workspace may send.

// inboxOwner is the account whose inbox the caller reads: the signed-in
// member, or the member a human participant token is linked to. Agents
// and participants from before accounts have none.
func inboxOwner(w http.ResponseWriter, r *http.Request) (int64, bool) {
	a := viewer(r)
	switch {
	case a.user != nil:
		return a.user.ID, true
	case a.participant != nil && a.participant.UserID != 0 && a.participant.Sponsor == nil:
		return a.participant.UserID, true
	}
	writeError(w, http.StatusForbidden, "only members have an inbox (agents send to theirs: POST …/inbox)")
	return 0, false
}

func (s *Server) listInbox(w http.ResponseWriter, r *http.Request) {
	user, ok := inboxOwner(w, r)
	if !ok {
		return
	}
	list, err := s.store.ListInbox(r.Context(), r.PathValue("ws"), user, r.URL.Query().Get("all") == "1")
	s.respond(w, http.StatusOK, list, nil, err)
}

func (s *Server) sendInbox(w http.ResponseWriter, r *http.Request) {
	var req struct {
		To     string `json:"to"`
		Body   string `json:"body"`
		ItemID *int64 `json:"item_id"`
		URL    string `json:"url"`
	}
	if !decode(w, r, &req) {
		return
	}
	e, evs, err := s.store.SendInbox(r.Context(), actor(r), store.InboxMessage{To: req.To, Body: req.Body, ItemID: req.ItemID, URL: req.URL})
	s.respond(w, http.StatusCreated, e, evs, err)
}

// dismissInbox dismisses (or restores) an entry of your own inbox.
func (s *Server) dismissInbox(dismiss bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := inboxOwner(w, r)
		if !ok {
			return
		}
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		e, evs, err := s.store.DismissInbox(r.Context(), r.PathValue("ws"), user, id, dismiss)
		s.respond(w, http.StatusOK, e, evs, err)
	}
}
