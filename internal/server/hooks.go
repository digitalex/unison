package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/digitalex/unison/internal/store"
)

// maxWebhookBody is the largest webhook request accepted (413 above it).
const maxWebhookBody = 1 << 20

// requestOrigin is the scheme://host clients used to reach this server
// (honouring a TLS-terminating proxy's X-Forwarded-Proto), as used in the
// agent doorway and webhook receive URLs.
func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func receiveURL(r *http.Request, token string) string { return requestOrigin(r) + "/hooks/" + token }

// webhookWithSecret is the create/rotate/secret response: the webhook plus
// its token and receive URL. Only authenticated participants get it.
type webhookWithSecret struct {
	*store.Webhook
	Token string `json:"token"`
	URL   string `json:"url"`
}

func withSecret(r *http.Request, h *store.Webhook) *webhookWithSecret {
	if h == nil {
		return nil
	}
	return &webhookWithSecret{Webhook: h, Token: h.Token, URL: receiveURL(r, h.Token)}
}

// receiveHook handles POST /hooks/{token}: no session auth, the token in the
// URL is the credential (plus an optional GitHub HMAC signature).
func (s *Server) receiveHook(w http.ResponseWriter, r *http.Request) {
	h, err := s.store.WebhookByToken(r.Context(), r.PathValue("token"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "unknown or disabled webhook")
		} else {
			storeError(w, err)
		}
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("webhook body is larger than %d bytes", maxWebhookBody))
		} else {
			writeError(w, http.StatusBadRequest, "reading body: "+err.Error())
		}
		return
	}
	if h.SigningSecret != "" && !validSignature(h.SigningSecret, body, r.Header.Get("X-Hub-Signature-256")) {
		writeError(w, http.StatusUnauthorized, "missing or invalid X-Hub-Signature-256")
		return
	}
	in := parseDelivery(r.Header, body)
	d, evs, err := s.store.ReceiveDelivery(r.Context(), h, in)
	if err != nil {
		storeError(w, err)
		return
	}
	s.published(evs)
	writeJSON(w, http.StatusAccepted, map[string]any{"delivery": d.ID})
}

// validSignature checks a GitHub-style "sha256=<hex HMAC of body>" header in
// constant time.
func validSignature(secret string, body []byte, header string) bool {
	got, ok := strings.CutPrefix(header, "sha256=")
	if !ok {
		return false
	}
	sig, err := hex.DecodeString(got)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(sig, mac.Sum(nil))
}

// keptHeaders are the request headers stored with a delivery.
var keptHeaders = []string{"Content-Type", "User-Agent", "X-GitHub-Event", "X-GitHub-Delivery"}

func parseDelivery(header http.Header, body []byte) store.IncomingDelivery {
	in := store.IncomingDelivery{Source: "generic", Headers: map[string]string{}, Body: body}
	for _, k := range keptHeaders {
		if v := header.Get(k); v != "" {
			in.Headers[k] = v
		}
	}
	if ev := header.Get("X-GitHub-Event"); ev != "" {
		in.Source, in.Event = "github", ev
		in.Summary = "github: " + githubSummary(ev, body)
		return in
	}
	what := header.Get("Content-Type")
	if mt, _, err := mime.ParseMediaType(what); err == nil {
		what = mt
	}
	if what == "" {
		what = "no content type"
	}
	in.Summary = in.Source + ": " + what
	return in
}

// githubPayload holds the fields used to summarize common GitHub events.
type githubPayload struct {
	Action string `json:"action"`
	Number int    `json:"number"`
	Ref    string `json:"ref"`
	Zen    string `json:"zen"`

	PullRequest *struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Merged bool   `json:"merged"`
	} `json:"pull_request"`
	Issue *struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
	} `json:"issue"`
	Commits []json.RawMessage `json:"commits"`
	Release *struct {
		TagName string `json:"tag_name"`
		Name    string `json:"name"`
	} `json:"release"`
	CheckRun    *githubRun `json:"check_run"`
	CheckSuite  *githubRun `json:"check_suite"`
	WorkflowRun *githubRun `json:"workflow_run"`
	Sender      *struct {
		Login string `json:"login"`
	} `json:"sender"`
}

type githubRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HeadBranch string `json:"head_branch"`
	App        *struct {
		Name string `json:"name"`
	} `json:"app"`
}

func (r *githubRun) outcome() string {
	if r.Conclusion != "" {
		return r.Conclusion
	}
	return r.Status
}

// githubSummary describes a GitHub event in one line, best effort.
func githubSummary(event string, body []byte) string {
	var p githubPayload
	if json.Unmarshal(body, &p) != nil {
		return event
	}
	parts := []string{event}
	add := func(s ...string) {
		for _, v := range s {
			if v != "" {
				parts = append(parts, v)
			}
		}
	}
	quote := func(s string) string {
		if s == "" {
			return ""
		}
		return strconv.Quote(s)
	}
	switch event {
	case "pull_request", "pull_request_review", "pull_request_review_comment":
		action := p.Action
		if action == "closed" && p.PullRequest != nil && p.PullRequest.Merged {
			action = "merged"
		}
		add(action)
		if p.PullRequest != nil {
			add(fmt.Sprintf("#%d", p.PullRequest.Number), quote(p.PullRequest.Title))
		}
	case "push":
		add(strings.TrimPrefix(strings.TrimPrefix(p.Ref, "refs/heads/"), "refs/tags/"))
		n := len(p.Commits)
		add(fmt.Sprintf("(%d commit%s)", n, map[bool]string{true: "", false: "s"}[n == 1]))
	case "issues":
		add(p.Action)
		if p.Issue != nil {
			add(fmt.Sprintf("#%d", p.Issue.Number), quote(p.Issue.Title))
		}
	case "issue_comment":
		add(p.Action)
		if p.Issue != nil {
			add(fmt.Sprintf("on #%d", p.Issue.Number), quote(p.Issue.Title))
		}
	case "check_run", "check_suite", "workflow_run":
		run := map[string]*githubRun{"check_run": p.CheckRun, "check_suite": p.CheckSuite, "workflow_run": p.WorkflowRun}[event]
		if run != nil {
			name := run.Name
			if name == "" && run.App != nil {
				name = run.App.Name
			}
			add(quote(name), run.outcome())
			if run.HeadBranch != "" {
				add("on " + run.HeadBranch)
			}
		}
	case "release":
		add(p.Action)
		if p.Release != nil {
			add(p.Release.TagName, quote(p.Release.Name))
		}
	case "ping":
		add(quote(p.Zen))
	default:
		add(p.Action)
	}
	if p.Sender != nil && p.Sender.Login != "" && event != "ping" {
		add("by " + p.Sender.Login)
	}
	return strings.Join(parts, " ")
}

// ---- Webhook API ----

func (s *Server) listWebhooks(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListWebhooks(r.Context(), r.PathValue("ws"))
	s.respond(w, http.StatusOK, list, nil, err)
}

func (s *Server) getWebhook(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	h, err := s.store.GetWebhook(r.Context(), r.PathValue("ws"), id)
	s.respond(w, http.StatusOK, h, nil, err)
}

func (s *Server) addWebhook(w http.ResponseWriter, r *http.Request) {
	var req store.NewWebhook
	if !decode(w, r, &req) {
		return
	}
	h, evs, err := s.store.AddWebhook(r.Context(), actor(r), req)
	s.respond(w, http.StatusCreated, withSecret(r, h), evs, err)
}

func (s *Server) editWebhook(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var p store.WebhookPatch
	if !ok || !decode(w, r, &p) {
		return
	}
	h, evs, err := s.store.EditWebhook(r.Context(), actor(r), id, p)
	s.respond(w, http.StatusOK, h, evs, err)
}

func (s *Server) rotateWebhook(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	h, evs, err := s.store.RotateWebhook(r.Context(), actor(r), id)
	s.respond(w, http.StatusOK, withSecret(r, h), evs, err)
}

func (s *Server) webhookSecret(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	h, err := s.store.GetWebhook(r.Context(), r.PathValue("ws"), id)
	s.respond(w, http.StatusOK, withSecret(r, h), nil, err)
}

func (s *Server) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	evs, err := s.store.DeleteWebhook(r.Context(), actor(r), id)
	s.respond(w, http.StatusOK, map[string]any{"deleted": id}, evs, err)
}

func (s *Server) listDeliveries(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := s.store.ListDeliveries(r.Context(), r.PathValue("ws"), id, limit)
	s.respond(w, http.StatusOK, list, nil, err)
}

func (s *Server) getDelivery(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	d, err := s.store.GetDelivery(r.Context(), r.PathValue("ws"), id)
	s.respond(w, http.StatusOK, d, nil, err)
}
