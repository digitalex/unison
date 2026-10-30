package server

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"
	"time"
)

// App Review sign-in (#139): POST /api/auth/review {code, label} with the
// server's UNISON_REVIEW_CODE returns {token}, an app session for the App
// Review account (store.ReviewLogin), like POST /api/auth/app/token does
// after a GitHub or Google sign-in. Without UNISON_REVIEW_CODE it's 404.

// reviewRetry is how long after a wrong code every attempt is refused, so
// the code can't be guessed quickly.
const reviewRetry = time.Second

// reviewLimiter remembers the last wrong code.
type reviewLimiter struct {
	mu    sync.Mutex
	last  time.Time
	retry time.Duration // reviewRetry; tests shorten it
}

func (s *Server) reviewSignIn(w http.ResponseWriter, r *http.Request) {
	want := s.authCfg.ReviewCode
	if want == "" {
		writeError(w, http.StatusNotFound, "App Review sign-in is not enabled on this server")
		return
	}
	var req struct {
		Code  string `json:"code"`
		Label string `json:"label"`
	}
	if !decode(w, r, &req) {
		return
	}
	l := &s.reviewFails
	l.mu.Lock()
	recent := time.Since(l.last) < l.retry
	ok := !recent && subtle.ConstantTimeCompare([]byte(strings.TrimSpace(req.Code)), []byte(want)) == 1
	if !ok && !recent {
		l.last = time.Now()
	}
	l.mu.Unlock()
	switch {
	case recent:
		writeError(w, http.StatusTooManyRequests, "too many attempts: wait a second and try again")
		return
	case !ok:
		writeError(w, http.StatusUnauthorized, "that access code isn't right")
		return
	}
	token, err := s.store.ReviewLogin(r.Context(), firstNonEmpty(strings.TrimSpace(req.Label), "App Review"))
	s.respond(w, http.StatusOK, map[string]any{"token": token}, nil, err)
}
