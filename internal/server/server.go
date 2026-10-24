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

	// notify is closed and replaced whenever new events are recorded; it
	// wakes long-polls (`unison wait`) and SSE streams. The broadcast is
	// in-process, so every client must reach the same server: deployments run
	// a single instance (Cloud Run max-instances=1), even with Postgres.
	mu     sync.Mutex
	notify chan struct{}
}

// New returns a Server backed by st. web holds the built UI (index.html at
// its root) and may be nil.
func New(st *store.Store, web fs.FS) *Server {
	s := &Server{store: st, web: web, mux: http.NewServeMux(), notify: make(chan struct{})}
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
	m.HandleFunc("GET "+ws, s.getWorkspace)
	m.HandleFunc("PATCH "+ws, s.auth(s.updateWorkspace))
	m.HandleFunc("POST "+ws+"/join", s.join)
	m.HandleFunc("GET "+ws+"/me", s.auth(s.me))
	m.HandleFunc("PATCH "+ws+"/me", s.auth(s.updateMe))
	m.HandleFunc("GET "+ws+"/participants", s.listParticipants)
	m.HandleFunc("GET "+ws+"/messages", s.listMessages)
	m.HandleFunc("POST "+ws+"/messages", s.auth(s.postMessage))
	m.HandleFunc("GET "+ws+"/backlog", s.listBacklog)
	m.HandleFunc("POST "+ws+"/backlog", s.auth(s.addItem))
	m.HandleFunc("POST "+ws+"/backlog/claim", s.auth(s.claimNext))
	m.HandleFunc("GET "+ws+"/backlog/{id}", s.getItem)
	m.HandleFunc("PATCH "+ws+"/backlog/{id}", s.auth(s.updateItem))
	m.HandleFunc("DELETE "+ws+"/backlog/{id}", s.auth(s.deleteItem))
	m.HandleFunc("POST "+ws+"/backlog/{id}/move", s.auth(s.moveItem))
	m.HandleFunc("POST "+ws+"/backlog/{id}/assign", s.auth(s.assignItem))
	m.HandleFunc("POST "+ws+"/backlog/{id}/release", s.auth(s.releaseItem))
	m.HandleFunc("POST "+ws+"/backlog/{id}/close", s.auth(s.closeItem))
	m.HandleFunc("POST "+ws+"/backlog/{id}/reopen", s.auth(s.reopenItem))
	m.HandleFunc("GET "+ws+"/backlog/{id}/comments", s.listComments)
	m.HandleFunc("POST "+ws+"/backlog/{id}/comments", s.auth(s.addComment))
	m.HandleFunc("DELETE "+ws+"/backlog/{id}/comments/{cid}", s.auth(s.deleteComment))
	m.HandleFunc("POST "+ws+"/backlog/{id}/prs", s.auth(s.linkPR))
	m.HandleFunc("DELETE "+ws+"/backlog/{id}/prs", s.auth(s.unlinkPR))
	m.HandleFunc("GET "+ws+"/metrics", s.listMetrics)
	m.HandleFunc("POST "+ws+"/metrics", s.auth(s.addMetric))
	m.HandleFunc("GET "+ws+"/metrics/{id}", s.getMetric)
	m.HandleFunc("PATCH "+ws+"/metrics/{id}", s.auth(s.editMetric))
	m.HandleFunc("DELETE "+ws+"/metrics/{id}", s.auth(s.deleteMetric))
	m.HandleFunc("GET "+ws+"/events", s.events)
	m.HandleFunc("GET "+ws+"/stream", s.stream)
	m.HandleFunc("GET /agent.md", s.doorway)
	m.HandleFunc("GET /w/{ws}/agent.md", s.doorway)
	m.HandleFunc("GET /download/unison", s.download)
	m.HandleFunc("/", s.serveWeb)
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

// auth requires a session token belonging to the workspace in the path.
func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := s.store.Authenticate(r.Context(), token(r))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "missing or invalid session token; join the workspace first (POST /api/workspaces/{ws}/join)")
			return
		}
		if p.WorkspaceID != r.PathValue("ws") {
			writeError(w, http.StatusForbidden, "this session token belongs to workspace "+p.WorkspaceID)
			return
		}
		s.touch(r.Context(), p)
		h(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, p)))
	}
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

func (s *Server) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListWorkspaces(r.Context())
	s.respond(w, http.StatusOK, list, nil, err)
}

func (s *Server) createWorkspace(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		RepoURL   string `json:"repo_url"`
		CanvasURL string `json:"canvas_url"`
	}
	if !decode(w, r, &req) {
		return
	}
	ws, evs, err := s.store.CreateWorkspace(r.Context(), req.Name, req.RepoURL, req.CanvasURL)
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

// ---- Participants ----

func (s *Server) join(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string `json:"name"`
		Emoji string `json:"emoji"`
	}
	if !decode(w, r, &req) {
		return
	}
	p, tok, evs, err := s.store.Join(r.Context(), r.PathValue("ws"), req.Name, req.Emoji)
	s.respond(w, http.StatusCreated, map[string]any{"participant": p, "token": tok}, evs, err)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, actor(r))
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

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body     string `json:"body"`
		ParentID *int64 `json:"parent_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	m, evs, err := s.store.PostMessage(r.Context(), actor(r), req.Body, req.ParentID)
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

// ---- Metrics ----

func (s *Server) listMetrics(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListMetrics(r.Context(), r.PathValue("ws"))
	s.respond(w, http.StatusOK, list, nil, err)
}

func (s *Server) getMetric(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	m, err := s.store.GetMetric(r.Context(), r.PathValue("ws"), id)
	s.respond(w, http.StatusOK, m, nil, err)
}

func (s *Server) addMetric(w http.ResponseWriter, r *http.Request) {
	var req store.NewMetric
	if !decode(w, r, &req) {
		return
	}
	m, evs, err := s.store.AddMetric(r.Context(), actor(r), req)
	s.respond(w, http.StatusCreated, m, evs, err)
}

func (s *Server) editMetric(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var p store.MetricPatch
	if !ok || !decode(w, r, &p) {
		return
	}
	m, evs, err := s.store.EditMetric(r.Context(), actor(r), id, p)
	s.respond(w, http.StatusOK, m, evs, err)
}

func (s *Server) deleteMetric(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	evs, err := s.store.DeleteMetric(r.Context(), actor(r), id)
	s.respond(w, http.StatusOK, map[string]any{"deleted": id}, evs, err)
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
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	origin := scheme + "://" + r.Host
	var ws *store.Workspace
	if id := r.PathValue("ws"); id != "" {
		var err error
		if ws, err = s.store.GetWorkspace(r.Context(), id); err != nil {
			storeError(w, err)
			return
		}
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	fmt.Fprint(w, guide.Doorway(origin, ws))
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
