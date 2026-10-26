// Package server is Unison's HTTP API server. It also serves the web UI and
// the agent doorway (`/agent.md`).
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digitalex/unison/internal/guide"
	"github.com/digitalex/unison/internal/store"
)

// Server handles Unison's HTTP API.
type Server struct {
	store *store.Store
	web   fs.FS
	mux   *http.ServeMux
	// ws holds the workspace routes; it is reachable only through gate.
	ws       *http.ServeMux
	wsRoutes []string // their patterns (the route-walk test checks each)

	// notify is closed and replaced whenever new events are recorded; it
	// wakes long-polls (`unison wait`) and SSE streams. The broadcast is
	// in-process, so every client must reach the same server: deployments run
	// a single instance (Cloud Run max-instances=1), even with Postgres.
	mu     sync.Mutex
	notify chan struct{}

	authCfg AuthConfig
}

// New returns a Server backed by st. web holds the built UI (index.html at
// its root) and may be nil.
func New(st *store.Store, web fs.FS) *Server {
	s := &Server{store: st, web: web, mux: http.NewServeMux(), ws: http.NewServeMux(), notify: make(chan struct{})}
	s.SetAuth(AuthConfig{})
	s.routes()
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// RunLeaseSweeper releases expired backlog leases every interval until ctx
// is done, so clients watching the stream see items return to the queue.
// (Expiry is also applied lazily on every backlog read and write.)
func (s *Server) RunLeaseSweeper(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			evs, err := s.store.ExpireLeases(ctx)
			if err != nil {
				log.Printf("lease sweeper: %v", err)
			}
			s.published(evs)
		}
	}
}

func (s *Server) routes() {
	const ws = "/api/workspaces/{ws}"
	m := s.mux
	m.HandleFunc("GET /api/workspaces", s.listWorkspaces)
	m.HandleFunc("POST /api/workspaces", s.createWorkspace)
	// Everything under a workspace goes through the gate (gate.go)...
	m.Handle(ws, s.gate(s.ws))
	m.Handle(ws+"/", s.gate(s.ws))
	// ...except joining, which is how you get in, and the doorway.
	m.HandleFunc("POST "+ws+"/join", s.join)
	m.HandleFunc("GET /agent.md", s.doorway)
	m.HandleFunc("GET /w/{ws}/agent.md", s.doorway)
	m.HandleFunc("POST /hooks/{token}", s.receiveHook)
	m.HandleFunc("GET /download/unison", s.download)
	s.authRoutes()
	m.HandleFunc("/", s.serveWeb)

	h := s.handleWS
	h("GET "+ws, s.getWorkspace)
	h("PATCH "+ws, s.auth(s.updateWorkspace))
	h("GET "+ws+"/repos", s.listRepos)
	h("POST "+ws+"/repos", s.auth(s.addRepo))
	h("PATCH "+ws+"/repos/{id}", s.auth(s.editRepo))
	h("DELETE "+ws+"/repos/{id}", s.auth(s.removeRepo))
	h("GET "+ws+"/me", s.auth(s.me))
	h("PATCH "+ws+"/me", s.auth(s.updateMe))
	h("POST "+ws+"/passes", s.requireUser(s.mintPass))
	h("GET "+ws+"/members", s.listMembers)
	h("DELETE "+ws+"/members/{user}", s.requireUser(s.removeMember))
	h("GET "+ws+"/invites", s.listInvites)
	h("POST "+ws+"/invites", s.requireUser(s.createInvite))
	h("DELETE "+ws+"/invites/{id}", s.requireUser(s.revokeInvite))
	h("GET "+ws+"/participants", s.listParticipants)
	h("GET "+ws+"/messages", s.listMessages)
	h("POST "+ws+"/messages", s.auth(s.postMessage))
	h("GET "+ws+"/inbox", s.listInbox)
	h("POST "+ws+"/inbox", s.auth(s.sendInbox))
	h("POST "+ws+"/inbox/{id}/dismiss", s.dismissInbox(true))
	h("POST "+ws+"/inbox/{id}/restore", s.dismissInbox(false))
	h("GET "+ws+"/messages/{id}", s.getMessage)
	h("PUT "+ws+"/messages/{id}/reactions/{emoji}", s.auth(s.react(true)))
	h("DELETE "+ws+"/messages/{id}/reactions/{emoji}", s.auth(s.react(false)))
	h("GET "+ws+"/backlog", s.listBacklog)
	h("POST "+ws+"/backlog", s.auth(s.addItem))
	h("POST "+ws+"/backlog/claim", s.auth(s.claimNext))
	h("GET "+ws+"/backlog/{id}", s.getItem)
	h("PATCH "+ws+"/backlog/{id}", s.auth(s.updateItem))
	h("DELETE "+ws+"/backlog/{id}", s.auth(s.deleteItem))
	h("POST "+ws+"/backlog/{id}/move", s.auth(s.moveItem))
	h("POST "+ws+"/backlog/{id}/assign", s.auth(s.assignItem))
	h("POST "+ws+"/backlog/{id}/release", s.auth(s.releaseItem))
	h("POST "+ws+"/backlog/{id}/close", s.auth(s.closeItem))
	h("POST "+ws+"/backlog/{id}/reopen", s.auth(s.reopenItem))
	h("GET "+ws+"/backlog/{id}/comments", s.listComments)
	h("GET "+ws+"/backlog/{id}/messages", s.listItemMessages)
	h("POST "+ws+"/backlog/{id}/comments", s.auth(s.addComment))
	h("DELETE "+ws+"/backlog/{id}/comments/{cid}", s.auth(s.deleteComment))
	h("POST "+ws+"/backlog/{id}/prs", s.auth(s.linkPR))
	h("DELETE "+ws+"/backlog/{id}/prs", s.auth(s.unlinkPR))
	h("GET "+ws+"/webhooks", s.listWebhooks)
	h("POST "+ws+"/webhooks", s.auth(s.addWebhook))
	h("GET "+ws+"/webhooks/{id}", s.getWebhook)
	h("PATCH "+ws+"/webhooks/{id}", s.auth(s.editWebhook))
	h("DELETE "+ws+"/webhooks/{id}", s.auth(s.deleteWebhook))
	h("POST "+ws+"/webhooks/{id}/rotate", s.auth(s.rotateWebhook))
	h("GET "+ws+"/webhooks/{id}/secret", s.auth(s.webhookSecret))
	h("GET "+ws+"/webhooks/{id}/deliveries", s.listDeliveries)
	h("GET "+ws+"/deliveries/{id}", s.auth(s.getDelivery))
	h("GET "+ws+"/events", s.events)
	h("GET "+ws+"/stream", s.stream)
}

// handleWS registers a workspace route behind the gate.
func (s *Server) handleWS(pattern string, h http.HandlerFunc) {
	s.ws.HandleFunc(pattern, h)
	s.wsRoutes = append(s.wsRoutes, pattern)
}

// ---- Plumbing ----

// published wakes everyone waiting for events.
func (s *Server) published(evs []store.Event) {
	if len(evs) == 0 {
		return
	}
	s.mu.Lock()
	close(s.notify)
	s.notify = make(chan struct{})
	s.mu.Unlock()
}

func (s *Server) waitChan() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.notify
}

type ctxKey struct{}

func token(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return r.URL.Query().Get("token")
}

// touch records activity, at most every few seconds per participant.
func (s *Server) touch(ctx context.Context, p *store.Participant) {
	if time.Since(time.UnixMilli(p.LastSeenAt)) < 5*time.Second {
		return
	}
	if err := s.store.Touch(ctx, p.ID); err != nil {
		log.Printf("touch participant %d: %v", p.ID, err)
	}
}

func actor(r *http.Request) *store.Participant {
	return r.Context().Value(ctxKey{}).(*store.Participant)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func storeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	default:
		log.Printf("internal error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// respond writes v (or err) and publishes events.
func (s *Server) respond(w http.ResponseWriter, status int, v any, evs []store.Event, err error) {
	if err != nil {
		storeError(w, err)
		return
	}
	s.published(evs)
	writeJSON(w, status, v)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.ContentLength == 0 {
		return true
	}
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) { return pathInt(w, r, "id") }

func pathInt(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimPrefix(r.PathValue(name), "#"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a number")
		return 0, false
	}
	return id, true
}

// ---- Workspaces ----

// listWorkspaces lists the caller's workspaces (all of them in open mode).
func (s *Server) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	if s.authCfg.Open {
		list, err := s.store.ListWorkspaces(r.Context())
		s.respond(w, http.StatusOK, list, nil, err)
		return
	}
	a, err := s.currentUser(r)
	if err != nil {
		writeErrorCode(w, http.StatusUnauthorized, codeSignIn, "sign in to see your workspaces")
		return
	}
	list, err := s.store.ListWorkspacesFor(r.Context(), a.user.ID)
	s.respond(w, http.StatusOK, list, nil, err)
}

func (s *Server) createWorkspace(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string            `json:"name"`
		RepoURL   string            `json:"repo_url"`
		Repos     []store.RepoInput `json:"repos"`
		CanvasURL string            `json:"canvas_url"`
	}
	creator, ok := s.mayCreateWorkspace(w, r)
	if !ok || !decode(w, r, &req) {
		return
	}
	var ws *store.Workspace
	var evs []store.Event
	var err error
	if creator != nil {
		ws, evs, err = s.store.CreateWorkspaceFor(r.Context(), creator.ID, req.Name, req.RepoURL, req.CanvasURL, req.Repos...)
	} else {
		ws, evs, err = s.store.CreateWorkspace(r.Context(), req.Name, req.RepoURL, req.CanvasURL, req.Repos...)
	}
	s.respond(w, http.StatusCreated, ws, evs, err)
}

func (s *Server) getWorkspace(w http.ResponseWriter, r *http.Request) {
	ws, err := s.store.GetWorkspace(r.Context(), r.PathValue("ws"))
	s.respond(w, http.StatusOK, ws, nil, err)
}

func (s *Server) updateWorkspace(w http.ResponseWriter, r *http.Request) {
	var p store.WorkspacePatch
	if !decode(w, r, &p) {
		return
	}
	ws, evs, err := s.store.UpdateWorkspace(r.Context(), r.PathValue("ws"), actor(r).ID, p)
	s.respond(w, http.StatusOK, ws, evs, err)
}

// ---- Repositories ----

func (s *Server) listRepos(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListRepos(r.Context(), r.PathValue("ws"))
	s.respond(w, http.StatusOK, list, nil, err)
}

func (s *Server) addRepo(w http.ResponseWriter, r *http.Request) {
	var req store.RepoInput
	if !decode(w, r, &req) {
		return
	}
	repo, evs, err := s.store.AddRepo(r.Context(), actor(r), req)
	s.respond(w, http.StatusCreated, repo, evs, err)
}

func (s *Server) editRepo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var p store.RepoPatch
	if !ok || !decode(w, r, &p) {
		return
	}
	repo, evs, err := s.store.EditRepo(r.Context(), actor(r), id, p)
	s.respond(w, http.StatusOK, repo, evs, err)
}

func (s *Server) removeRepo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	evs, err := s.store.RemoveRepo(r.Context(), actor(r), id)
	s.respond(w, http.StatusOK, map[string]any{"deleted": id}, evs, err)
}

// ---- Participants ----

func (s *Server) join(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Emoji  string `json:"emoji"`
		Pass   string `json:"pass"`
		Invite string `json:"invite"`
	}
	if !decode(w, r, &req) {
		return
	}
	// Agents join with a pass.
	if req.Pass != "" {
		p, tok, evs, err := s.store.JoinWithPass(r.Context(), r.PathValue("ws"), req.Pass, req.Name, req.Emoji)
		s.respond(w, http.StatusCreated, map[string]any{"participant": p, "token": tok}, evs, err)
		return
	}
	// Humans join signed in: as a member, or with an invite.
	if u := s.joiningUser(r); u != nil {
		s.joinAsUser(w, r, u, req.Name, req.Emoji, req.Invite)
		return
	}
	if !s.authCfg.Open {
		writeErrorCode(w, http.StatusUnauthorized, codeSignIn,
			"joining needs an invite: agents join with the pass from their invite (--pass); humans sign in (`unison login`, then `unison join <url> --account`)")
		return
	}
	p, tok, evs, err := s.store.Join(r.Context(), r.PathValue("ws"), req.Name, req.Emoji)
	s.respond(w, http.StatusCreated, map[string]any{"participant": p, "token": tok}, evs, err)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	s.respondMe(w, r, actor(r))
}

func (s *Server) updateMe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"`
	}
	if !decode(w, r, &req) {
		return
	}
	p, evs, err := s.store.SetStatus(r.Context(), actor(r), req.Status)
	s.respond(w, http.StatusOK, p, evs, err)
}

func (s *Server) listParticipants(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListParticipants(r.Context(), r.PathValue("ws"))
	s.respond(w, http.StatusOK, list, nil, err)
}

// ---- Chat ----

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := s.store.ListMessages(r.Context(), r.PathValue("ws"), limit)
	s.respond(w, http.StatusOK, list, nil, err)
}

func (s *Server) getMessage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	m, err := s.store.GetMessage(r.Context(), r.PathValue("ws"), id)
	s.respond(w, http.StatusOK, m, nil, err)
}

// react puts (on) or takes back your reaction {emoji} (an emoji or a name
// such as "+1") on message {id}, responding with the message.
func (s *Server) react(on bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		m, evs, err := s.store.React(r.Context(), actor(r), id, r.PathValue("emoji"), on)
		s.respond(w, http.StatusOK, m, evs, err)
	}
}

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body     string `json:"body"`
		ParentID *int64 `json:"parent_id"`
		ItemID   *int64 `json:"item_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	m, evs, err := s.store.PostMessage(r.Context(), actor(r), req.Body, req.ParentID, req.ItemID)
	s.respond(w, http.StatusCreated, m, evs, err)
}

// ---- Backlog ----

func (s *Server) listBacklog(w http.ResponseWriter, r *http.Request) {
	list, evs, err := s.store.ListBacklog(r.Context(), r.PathValue("ws"), r.URL.Query().Get("status"))
	s.respond(w, http.StatusOK, list, evs, err)
}

func (s *Server) getItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	it, evs, err := s.store.GetItem(r.Context(), r.PathValue("ws"), id)
	s.respond(w, http.StatusOK, it, evs, err)
}

func (s *Server) addItem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title    string `json:"title"`
		Body     string `json:"body"`
		Position int    `json:"position"`
	}
	if !decode(w, r, &req) {
		return
	}
	it, evs, err := s.store.AddItem(r.Context(), actor(r), req.Title, req.Body, req.Position)
	s.respond(w, http.StatusCreated, it, evs, err)
}

func (s *Server) updateItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var p store.ItemPatch
	if !ok || !decode(w, r, &p) {
		return
	}
	it, evs, err := s.store.UpdateItem(r.Context(), actor(r), id, p)
	s.respond(w, http.StatusOK, it, evs, err)
}

func (s *Server) deleteItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	evs, err := s.store.DeleteItem(r.Context(), actor(r), id)
	s.respond(w, http.StatusOK, map[string]any{"deleted": id}, evs, err)
}

func (s *Server) moveItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var req struct {
		Position int `json:"position"`
	}
	if !ok || !decode(w, r, &req) {
		return
	}
	it, evs, err := s.store.MoveItem(r.Context(), actor(r), id, req.Position)
	s.respond(w, http.StatusOK, it, evs, err)
}

func (s *Server) assignItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var req struct {
		AssigneeID int64 `json:"assignee_id"`
	}
	if !ok || !decode(w, r, &req) {
		return
	}
	if req.AssigneeID == 0 {
		req.AssigneeID = actor(r).ID
	}
	it, evs, err := s.store.AssignItem(r.Context(), actor(r), id, req.AssigneeID)
	s.respond(w, http.StatusOK, it, evs, err)
}

func (s *Server) claimNext(w http.ResponseWriter, r *http.Request) {
	it, evs, err := s.store.ClaimNext(r.Context(), actor(r))
	s.respond(w, http.StatusOK, it, evs, err)
}

func (s *Server) releaseItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	it, evs, err := s.store.ReleaseItem(r.Context(), actor(r), id)
	s.respond(w, http.StatusOK, it, evs, err)
}

func (s *Server) closeItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var req struct {
		Note string `json:"note"`
	}
	if !ok || !decode(w, r, &req) {
		return
	}
	it, evs, err := s.store.CloseItem(r.Context(), actor(r), id, req.Note)
	s.respond(w, http.StatusOK, it, evs, err)
}

func (s *Server) reopenItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	it, evs, err := s.store.ReopenItem(r.Context(), actor(r), id)
	s.respond(w, http.StatusOK, it, evs, err)
}

// ---- Comments and PR links ----

func (s *Server) listComments(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	list, err := s.store.ListComments(r.Context(), r.PathValue("ws"), id)
	s.respond(w, http.StatusOK, list, nil, err)
}

// listItemMessages lists the chat messages about a backlog item.
func (s *Server) listItemMessages(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	list, err := s.store.ItemMessages(r.Context(), r.PathValue("ws"), id)
	s.respond(w, http.StatusOK, list, nil, err)
}

func (s *Server) addComment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var req struct {
		Body string `json:"body"`
	}
	if !ok || !decode(w, r, &req) {
		return
	}
	c, evs, err := s.store.AddComment(r.Context(), actor(r), id, req.Body)
	s.respond(w, http.StatusCreated, c, evs, err)
}

func (s *Server) deleteComment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	cid, ok := pathInt(w, r, "cid")
	if !ok {
		return
	}
	evs, err := s.store.DeleteComment(r.Context(), actor(r), id, cid)
	s.respond(w, http.StatusOK, map[string]any{"deleted": cid}, evs, err)
}

// prURL reads the PR URL from a JSON body ({"url": ...}) or ?url=.
func prURL(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req struct {
		URL string `json:"url"`
	}
	if !decode(w, r, &req) {
		return "", false
	}
	return firstNonEmpty(req.URL, r.URL.Query().Get("url")), true
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func (s *Server) linkPR(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u, ok := prURL(w, r)
	if !ok {
		return
	}
	it, evs, err := s.store.LinkPR(r.Context(), actor(r), id, u)
	s.respond(w, http.StatusOK, it, evs, err)
}

func (s *Server) unlinkPR(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u, ok := prURL(w, r)
	if !ok {
		return
	}
	it, evs, err := s.store.UnlinkPR(r.Context(), actor(r), id, u)
	s.respond(w, http.StatusOK, it, evs, err)
}

// ---- Real-time ----

// events returns events after the `after` cursor. With `wait=N` (seconds,
// max 300) it long-polls until at least one event exists or N elapses.
// Without `after`, it returns only the current cursor so a client can start
// following from "now".
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	ctx, wsID, q := r.Context(), r.PathValue("ws"), r.URL.Query()
	if _, err := s.store.GetWorkspace(ctx, wsID); err != nil {
		storeError(w, err)
		return
	}
	if p, err := s.store.Authenticate(ctx, token(r)); err == nil && p.WorkspaceID == wsID {
		s.touch(ctx, p)
	}
	if q.Get("after") == "" {
		seq, err := s.store.LatestSeq(ctx, wsID)
		s.respond(w, http.StatusOK, map[string]any{"events": []store.Event{}, "cursor": seq}, nil, err)
		return
	}
	after, err := strconv.ParseInt(q.Get("after"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "after must be an event seq")
		return
	}
	wait, _ := strconv.Atoi(q.Get("wait"))
	deadline := time.NewTimer(time.Duration(min(max(wait, 0), 300)) * time.Second)
	defer deadline.Stop()
	for {
		ch := s.waitChan()
		evs, err := s.store.EventsAfter(ctx, wsID, after, 0)
		if err != nil {
			storeError(w, err)
			return
		}
		if len(evs) > 0 || wait <= 0 {
			cursor := after
			if len(evs) > 0 {
				cursor = evs[len(evs)-1].Seq
			}
			writeJSON(w, http.StatusOK, map[string]any{"events": evs, "cursor": cursor})
			return
		}
		select {
		case <-ch:
		case <-deadline.C:
			wait = 0
		case <-ctx.Done():
			return
		}
	}
}

// stream is a Server-Sent Events feed of a workspace's events, for browsers.
// Holding the stream open (with ?token=) keeps the participant present.
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	ctx, wsID := r.Context(), r.PathValue("ws")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	after, err := s.store.LatestSeq(ctx, wsID)
	if err != nil {
		storeError(w, err)
		return
	}
	var me *store.Participant
	if p, err := s.store.Authenticate(ctx, token(r)); err == nil && p.WorkspaceID == wsID {
		me = p
		s.store.Touch(ctx, p.ID)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprintf(w, "event: ready\ndata: {\"cursor\":%d}\n\n", after)
	flusher.Flush()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		ch := s.waitChan()
		evs, err := s.store.EventsAfter(ctx, wsID, after, 0)
		if err != nil {
			return
		}
		for _, e := range evs {
			raw, _ := json.Marshal(e)
			fmt.Fprintf(w, "data: %s\n\n", raw)
			after = e.Seq
		}
		flusher.Flush()
		select {
		case <-ch:
		case <-heartbeat.C:
			if me != nil {
				s.store.Touch(ctx, me.ID)
			}
			fmt.Fprint(w, ": heartbeat\n\n")
		case <-ctx.Done():
			return
		}
	}
}

// ---- Doorway & web ----

// doorway serves the self-contained agent guide, tailored to a workspace
// when the path names one.
func (s *Server) doorway(w http.ResponseWriter, r *http.Request) {
	origin := requestOrigin(r)
	var ws *store.Workspace
	var pass string
	if id := r.PathValue("ws"); id != "" {
		var err error
		if ws, err = s.store.GetWorkspace(r.Context(), id); err != nil {
			// Unknown and private look alike to outsiders.
			if _, ok := s.doorwayAccess(w, r, &store.Workspace{ID: id}); ok {
				storeError(w, err)
			}
			return
		}
		var ok bool
		if pass, ok = s.doorwayAccess(w, r, ws); !ok {
			return
		}
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	fmt.Fprint(w, guide.Doorway(origin, ws, pass))
}

// download serves this server's own binary, which is also the CLI. It only
// runs on the server's OS and architecture (see the X-Unison-Platform header).
func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	exe, err := os.Executable()
	if err != nil {
		writeError(w, http.StatusNotFound, "binary unavailable: "+err.Error())
		return
	}
	w.Header().Set("X-Unison-Platform", runtime.GOOS+"/"+runtime.GOARCH)
	w.Header().Set("Content-Disposition", `attachment; filename="unison"`)
	http.ServeFile(w, r, exe)
}

func (s *Server) serveWeb(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusNotFound, "no such API endpoint: "+r.Method+" "+r.URL.Path)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.web == nil {
		http.Error(w, "web UI not built; run `make web`", http.StatusNotFound)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path != "" {
		if f, err := s.web.Open(path); err == nil {
			f.Close()
			http.FileServerFS(s.web).ServeHTTP(w, r)
			return
		}
	}
	// Single-page app: every other path renders index.html.
	http.ServeFileFS(w, r, s.web, "index.html")
}
