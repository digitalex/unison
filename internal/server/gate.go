package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/digitalex/unison/internal/store"
)

// Workspaces are private: every /api/workspaces/{ws}/… route (reads and
// writes, events and the stream) goes through gate, because they are all
// registered on s.ws, which is reachable only through it. The exceptions
// are registered on the top mux and decide for themselves: POST …/join
// (that's how you get in) and the doorway (/w/{ws}/agent.md, which also
// accepts a pass).
//
// Allowed callers (any one):
//   - a signed-in member of the workspace (session cookie, with the CSRF
//     header on writes, or a `us_…` bearer token);
//   - a participant token whose participant belongs to a current member
//     (user_id);
//   - an agent's token (joined with a pass) whose sponsor is a current
//     member, not revoked or expired (Authenticate checks that).
//
// Participants from before accounts (no user, no sponsor) are refused.

// Error codes in {"error", "code"} responses, for clients to react to.
const (
	codeSignIn = "sign_in"     // no credentials
	codeLegacy = "legacy"      // a participant from before accounts
	codeMember = "not_member"  // signed in, but not a member
	codeRevoke = "revoked"     // an agent's credential stopped working
	codeOther  = "other_space" // a token for another workspace
	codeJoin   = "join_first"  // a member (or open-mode visitor) who hasn't joined
)

const legacyMessage = "This participant predates accounts and no longer works. Humans: sign in (`unison login`, or on the web) and join again. Agents: ask a member for an invite pass."

// access is who a gated request acts as.
type access struct {
	participant *store.Participant // nil for a signed-in member with no participant yet
	user        *store.User        // the signed-in member, if signed in
}

type accessKey struct{}

func viewer(r *http.Request) *access {
	a, _ := r.Context().Value(accessKey{}).(*access)
	if a == nil {
		return &access{}
	}
	return a
}

func writeErrorCode(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}

// participantToken is a participant (not user) token from the request:
// the bearer token, or ?token= (EventSource can't send headers).
func participantToken(r *http.Request) string {
	t := token(r)
	if strings.HasPrefix(t, store.UserTokenPrefix) {
		return ""
	}
	return t
}

// authorize decides who a request for workspace wsID acts as. It writes the
// error response and returns nil when the caller may not in.
func (s *Server) authorize(w http.ResponseWriter, r *http.Request, wsID string) *access {
	ctx := r.Context()
	if tok := participantToken(r); tok != "" {
		p, err := s.store.Authenticate(ctx, tok)
		switch {
		case errors.Is(err, store.ErrRevoked):
			writeErrorCode(w, http.StatusUnauthorized, codeRevoke, err.Error())
			return nil
		case err != nil:
			writeErrorCode(w, http.StatusUnauthorized, codeSignIn, "invalid participant token; sign in, or join with an invite")
			return nil
		case p.WorkspaceID != wsID:
			writeErrorCode(w, http.StatusForbidden, codeOther, "this participant token belongs to workspace "+p.WorkspaceID)
			return nil
		}
		if s.authCfg.Open {
			s.touch(ctx, p)
			return &access{participant: p}
		}
		switch {
		case p.Sponsor != nil:
			// Authenticate already required the sponsor to be a member.
		case p.UserID != 0:
			if ok, err := s.store.IsMember(ctx, wsID, p.UserID); err != nil || !ok {
				if err != nil {
					storeError(w, err)
				} else {
					writeErrorCode(w, http.StatusForbidden, codeMember, "your account is no longer a member of this workspace; ask a member for an invite")
				}
				return nil
			}
		default:
			writeErrorCode(w, http.StatusUnauthorized, codeLegacy, legacyMessage)
			return nil
		}
		s.touch(ctx, p)
		return &access{participant: p}
	}
	a, err := s.currentUser(r)
	if err != nil {
		if s.authCfg.Open {
			return &access{}
		}
		writeErrorCode(w, http.StatusUnauthorized, codeSignIn, "this workspace is private: sign in as a member (or join with an invite)")
		return nil
	}
	if !csrfOK(r, a) {
		writeError(w, http.StatusForbidden, "missing "+csrfHeader+" header")
		return nil
	}
	if !s.authCfg.Open {
		ok, err := s.store.IsMember(ctx, wsID, a.user.ID)
		if err != nil {
			storeError(w, err)
			return nil
		}
		if !ok {
			// Unknown workspaces look the same: no probing for IDs.
			writeErrorCode(w, http.StatusForbidden, codeMember, "you're not a member of this workspace; ask a member for an invite")
			return nil
		}
	}
	p, err := s.store.UserParticipant(ctx, wsID, a.user.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		storeError(w, err)
		return nil
	}
	if p != nil {
		s.touch(ctx, p)
	}
	return &access{participant: p, user: a.user}
}

// gate wraps the workspace routes (s.ws): see the package comment above.
func (s *Server) gate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := s.authorize(w, r, r.PathValue("ws"))
		if a == nil {
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), accessKey{}, a)))
	})
}

// auth marks workspace writes that act as a participant (the author of a
// message, the assignee of an item, …). The gate has already decided who
// the caller is; a signed-in member without a participant must join first.
func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := viewer(r)
		if a.participant == nil {
			if s.authCfg.Open && a.user == nil {
				writeErrorCode(w, http.StatusUnauthorized, codeJoin, "missing or invalid session token; join the workspace first (POST /api/workspaces/{ws}/join)")
				return
			}
			writeErrorCode(w, http.StatusForbidden, codeJoin, "join this workspace first (POST /api/workspaces/{ws}/join, or `unison join <url> --account`)")
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, a.participant)))
	}
}
