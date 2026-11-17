package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

// GET /api/workspaces/stats answers anyone, with counts and nothing else
// (#301).
func TestWorkspaceStats(t *testing.T) {
	ts, st := newServer(t)
	for _, name := range []string{"Acme Antigravity", "Secret Project"} {
		if _, _, err := st.CreateWorkspace(t.Context(), name, "https://github.com/acme/secret", ""); err != nil {
			t.Fatal(err)
		}
	}

	// No cookie, no token: the server under test requires sign-in elsewhere.
	if status, _ := get(t, ts.URL+"/api/workspaces"); status != http.StatusUnauthorized {
		t.Fatalf("GET /api/workspaces signed out: %d, want 401", status)
	}
	status, body := get(t, ts.URL+"/api/workspaces/stats")
	if status != http.StatusOK {
		t.Fatalf("GET /api/workspaces/stats: %d %s", status, body)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"total": 2.0, "created_last_7_days": 2.0, "created_today": 2.0}
	if len(got) != len(want) {
		t.Fatalf("stats: %s, want exactly the three counts", body)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("stats[%q] = %v, want %v (%s)", k, got[k], v, body)
		}
	}

	// Only reading is public; the path is not a way around the gate.
	for _, method := range []string{"POST", "PATCH", "DELETE"} {
		req, _ := http.NewRequest(method, ts.URL+"/api/workspaces/stats", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s /api/workspaces/stats: %d, want 401", method, resp.StatusCode)
		}
	}
}
