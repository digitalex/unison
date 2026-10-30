package server

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/digitalex/unison/internal/store"
	"github.com/digitalex/unison/internal/store/storetest"
)

func newServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(storetest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	web := fstest.MapFS{"index.html": {Data: []byte("<html>unison</html>")}, "assets/app.js": {Data: []byte("js")}}
	ts := httptest.NewServer(New(st, web))
	t.Cleanup(ts.Close)
	return ts, st
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestRouting(t *testing.T) {
	ts, st := newServer(t)
	ws, _, err := st.CreateWorkspace(t.Context(), "Unison", "https://github.com/digitalex/unison", "https://isocan.io/p/prj_x")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, want string
		status     int
	}{
		{"/", "unison</html>", 200},
		{"/w/" + ws.ID, "unison</html>", 200}, // SPA fallback.
		{"/assets/app.js", "js", 200},
		{"/api/nope", `"error"`, 404},
		// Workspaces are private: unknown and existing look alike to outsiders.
		{"/api/workspaces/missing", `"error"`, 401},
		{"/w/" + ws.ID + "/agent.md", "private", 401},
		{"/w/missing/agent.md", "private", 401},
		{"/agent.md", "unison join", 200},
	} {
		status, body := get(t, ts.URL+tc.path)
		if status != tc.status || !strings.Contains(body, tc.want) {
			t.Errorf("GET %s = %d %q; want %d containing %q", tc.path, status, body, tc.status, tc.want)
		}
	}
}

func TestMutationsNeedToken(t *testing.T) {
	ts, st := newServer(t)
	ws, _, _ := st.CreateWorkspace(t.Context(), "W", "", "")
	other, _, _ := st.CreateWorkspace(t.Context(), "Other", "", "")
	_, otherToken, _, _ := joinMember(t, st, other.ID, "Ada", "")
	for token, want := range map[string]int{"": 401, "bogus": 401, otherToken: 403} {
		req, _ := http.NewRequest("POST", ts.URL+"/api/workspaces/"+ws.ID+"/messages", strings.NewReader(`{"body":"hi"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("post with token %q = %d, want %d", token, resp.StatusCode, want)
		}
	}
}

func TestStreamDeliversEvents(t *testing.T) {
	ts, st := newServer(t)
	ws, _, _ := st.CreateWorkspace(t.Context(), "W", "", "")
	_, token, _, _ := joinMember(t, st, ws.ID, "Ada", "")
	resp, err := http.Get(ts.URL + "/api/workspaces/" + ws.ID + "/stream?token=" + token)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	lines := bufio.NewScanner(resp.Body)
	lines.Scan() // event: ready
	req, _ := http.NewRequest("POST", ts.URL+"/api/workspaces/"+ws.ID+"/backlog", strings.NewReader(`{"title":"Build Unison"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	if r, err := http.DefaultClient.Do(req); err != nil {
		t.Fatal(err)
	} else {
		r.Body.Close()
	}
	for lines.Scan() {
		if line := lines.Text(); strings.HasPrefix(line, "data: ") && strings.Contains(line, "backlog.added") {
			return
		}
	}
	t.Error("stream ended without a backlog.added event")
}

// call makes an API request and returns the status and decoded JSON body.
func call(t *testing.T, method, url, token, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

// joinMember makes a signed-in member join wsID (a user created for name)
// and returns what store.Join does. Workspaces are private: tests that once
// joined anonymously join as members.
func joinMember(t *testing.T, st *store.Store, wsID, name, emoji string) (*store.Participant, string, []store.Event, error) {
	t.Helper()
	u, err := st.LoginWithProvider(t.Context(), "test", store.Profile{Subject: wsID + "/" + name, Name: name})
	if err != nil {
		t.Fatal(err)
	}
	p, tok, _, evs, err := st.JoinAsUser(t.Context(), wsID, u.ID, name, emoji)
	return p, tok, evs, err
}

// getAs is get with a bearer token.
func getAs(t *testing.T, url, token string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// Events wake only the waiters of their own workspace (#107).
func TestPublishedWakesOnlyItsWorkspace(t *testing.T) {
	s := &Server{notify: map[string]chan struct{}{}}
	a, b := s.waitChan("ws_a"), s.waitChan("ws_b")
	if s.waitChan("ws_a") != a {
		t.Fatal("waiters of one workspace should share a channel")
	}
	s.published([]store.Event{{WorkspaceID: "ws_b", Type: "message.created"}})
	select {
	case <-a:
		t.Error("an event in ws_b woke ws_a")
	default:
	}
	select {
	case <-b:
	default:
		t.Error("an event in ws_b didn't wake ws_b")
	}
	if len(s.notify) != 1 {
		t.Errorf("notify = %v, want only ws_a's channel left", s.notify)
	}
}

func TestBacklogAssignmentAndProgressAPI(t *testing.T) {
	ts, st := newServer(t)
	ws, _, _ := st.CreateWorkspace(t.Context(), "W", "", "")
	adaP, ada, _, _ := joinMember(t, st, ws.ID, "Ada", "")
	bobP, bob, _, _ := joinMember(t, st, ws.ID, "Bob", "")
	base := ts.URL + "/api/workspaces/" + ws.ID + "/backlog"

	// Creating with assignee_id leaves the item open (no lease).
	status, it := call(t, "POST", base, ada, `{"title":"Pre-assigned","assignee_id":`+itoa(adaP.ID)+`}`)
	if status != http.StatusCreated || it["status"] != store.StatusOpen || int64(it["assignee_id"].(float64)) != adaP.ID || it["lease_expires_at"] != nil {
		t.Fatalf("POST /backlog assigned = %d %v", status, it)
	}
	itemURL := base + "/" + itoa(int64(it["id"].(float64)))

	// Bob cannot start an item assigned to Ada; Ada can.
	if status, _ := call(t, "POST", itemURL+"/start", bob, ""); status != http.StatusConflict {
		t.Errorf("POST /start by Bob = %d, want 409", status)
	}
	status, started := call(t, "POST", itemURL+"/start", ada, "")
	if status != http.StatusOK || started["status"] != store.StatusInProgress || started["lease_expires_at"] == nil {
		t.Fatalf("POST /start by Ada = %d %v", status, started)
	}

	// Reassigning while in progress keeps in_progress and updates assignee_id.
	status, reassigned := call(t, "POST", itemURL+"/assign", ada, `{"assignee_id":`+itoa(bobP.ID)+`}`)
	if status != http.StatusOK || reassigned["status"] != store.StatusInProgress || int64(reassigned["assignee_id"].(float64)) != bobP.ID {
		t.Fatalf("POST /assign to Bob = %d %v", status, reassigned)
	}

	// Stopping moves back to open while keeping assignee_id; assignee_id 0 unassigns.
	status, stopped := call(t, "POST", itemURL+"/stop", bob, "")
	if status != http.StatusOK || stopped["status"] != store.StatusOpen || int64(stopped["assignee_id"].(float64)) != bobP.ID || stopped["lease_expires_at"] != nil {
		t.Fatalf("POST /stop = %d %v", status, stopped)
	}
	status, unassigned := call(t, "POST", itemURL+"/assign", bob, `{"assignee_id":0}`)
	if status != http.StatusOK || unassigned["status"] != store.StatusOpen || unassigned["assignee_id"] != nil {
		t.Fatalf("POST /assign 0 = %d %v", status, unassigned)
	}
	status, selfAssigned := call(t, "POST", itemURL+"/assign", ada, "")
	if status != http.StatusOK || selfAssigned["status"] != store.StatusOpen || int64(selfAssigned["assignee_id"].(float64)) != adaP.ID {
		t.Fatalf("POST /assign empty body = %d %v", status, selfAssigned)
	}
	status, nullUnassigned := call(t, "POST", itemURL+"/assign", ada, `{"assignee_id":null}`)
	if status != http.StatusOK || nullUnassigned["status"] != store.StatusOpen || nullUnassigned["assignee_id"] != nil {
		t.Fatalf("POST /assign null = %d %v", status, nullUnassigned)
	}
}

func itoa(n int64) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}
