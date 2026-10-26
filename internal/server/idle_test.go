package server

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestAgentIdleLogout: an agent that makes any request (waiting for events
// included) at least every 6 hours stays logged in; one that makes none for
// longer is logged out with a 401 "idle". Humans never are.
func TestAgentIdleLogout(t *testing.T) {
	ts, st := newServer(t)
	ctx := t.Context()
	now := time.Now()
	st.SetClock(func() time.Time { return now })
	ws, _, _ := st.CreateWorkspace(ctx, "W", "", "")
	alex, alexTok, _, _ := joinMember(t, st, ws.ID, "Alex", "")
	join := func(name string) string {
		pass, _, _ := st.MintPass(ctx, ws.ID, alex.UserID)
		_, tok, _, err := st.JoinWithPass(ctx, ws.ID, pass, name, "")
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	parked, idle, busy := join("Parked"), join("Idle"), join("Busy")
	api := ts.URL + "/api/workspaces/" + ws.ID

	// Parked on `wait` (event polls) for 7 hours: still in.
	for range 7 {
		now = now.Add(time.Hour)
		if status, body := getAs(t, api+"/events?after=0", parked); status != http.StatusOK {
			t.Fatalf("waiting agent's poll = %d %s", status, body)
		}
	}
	// Busy: a request at 5h59m keeps it in for another 5h59m.
	now = now.Add(-7 * time.Hour)
	now = now.Add(5*time.Hour + 59*time.Minute)
	if status, _ := getAs(t, api+"/backlog", busy); status != http.StatusOK {
		t.Errorf("agent at 5h59m = %d", status)
	}
	now = now.Add(5*time.Hour + 59*time.Minute)
	if status, _ := getAs(t, api+"/backlog", busy); status != http.StatusOK {
		t.Errorf("agent 5h59m after its last request = %d", status)
	}
	if status, _ := getAs(t, api+"/backlog", parked); status != http.StatusOK {
		t.Errorf("parked agent = %d", status)
	}
	// Idle: nothing since it joined (11h58m ago): logged out, every route.
	for _, path := range []string{"/me", "/events?after=0", "/backlog"} {
		status, out := call(t, "GET", api+path, idle, "")
		if status != http.StatusUnauthorized || out["code"] != "idle" || !strings.Contains(out["error"].(string), "6 hours without any activity") {
			t.Errorf("idle agent %s = %d %v", path, status, out)
		}
	}
	if p, _ := st.Authenticate(ctx, idle); p != nil {
		t.Errorf("idle agent still authenticates: %+v", p)
	}
	// The roster shows it logged out (revoked as idle); the row stays.
	_, body := getAs(t, api+"/participants", alexTok)
	if !strings.Contains(body, `"name":"Idle"`) || !strings.Contains(body, `"revoke_reason":"idle"`) {
		t.Errorf("participants = %s", body)
	}
	// Humans are never logged out for idling.
	now = now.Add(100 * time.Hour)
	if status, _ := getAs(t, api+"/me", alexTok); status != http.StatusOK {
		t.Errorf("human after 100h = %d", status)
	}
	// And 6h01m with no request at all logs out even the parked agent.
	if status, out := call(t, "GET", api+"/me", parked, ""); status != http.StatusUnauthorized || out["code"] != "idle" {
		t.Errorf("parked agent after 100h without requests = %d %v", status, out)
	}
}
