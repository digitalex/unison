package server

import (
	"bufio"
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
		{"/api/workspaces/missing", `"error"`, 404},
		{"/w/" + ws.ID + "/agent.md", "https://isocan.io/p/prj_x", 200},
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
	_, otherToken, _, _ := st.Join(t.Context(), other.ID, "Ada", "")
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
	_, token, _, _ := st.Join(t.Context(), ws.ID, "Ada", "")
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
