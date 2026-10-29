package server

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

func TestSchedulesAPI(t *testing.T) {
	ts, st := newServer(t)
	u, _ := st.LoginWithProvider(t.Context(), "github", store.Profile{Subject: "ada", Name: "Ada"})
	userTok, _, _ := st.CreateSession(t.Context(), u.ID, store.SessionCLI, "")
	_, ws := call(t, "POST", ts.URL+"/api/workspaces", userTok, `{"name":"Recurring"}`)
	wsID := ws["id"].(string)
	_, token, _, _, _ := st.JoinAsUser(t.Context(), wsID, u.ID, "Ada", "")
	base := ts.URL + "/api/workspaces/" + wsID + "/schedules"

	if status, _ := call(t, "POST", base, "", `{"title":"Triage","cadence":"daily"}`); status != http.StatusUnauthorized {
		t.Errorf("unauthenticated add = %d, want 401", status)
	}
	if status, _ := call(t, "POST", base, token, `{"title":"Triage","cadence":"hourly"}`); status != http.StatusBadRequest {
		t.Errorf("hourly = %d, want 400", status)
	}
	status, sc := call(t, "POST", base, token, `{"title":"Deps","body":"Update dependencies","cadence":"weekly","weekday":"fri"}`)
	if status != http.StatusCreated || sc["weekday"] != "fri" || sc["next_due_at"] == nil {
		t.Fatalf("add = %d %v", status, sc)
	}
	one := base + "/" + strconv.Itoa(int(sc["id"].(float64)))
	if status, got := call(t, "PATCH", one, token, `{"cadence":"daily"}`); status != http.StatusOK || got["cadence"] != "daily" || got["weekday"] != nil {
		t.Errorf("edit = %d %v", status, got)
	}
	if _, body := getAs(t, base, token); !strings.Contains(body, `"title":"Deps"`) {
		t.Errorf("list = %s", body)
	}
	if status, _ := call(t, "DELETE", one, token, ""); status != http.StatusOK {
		t.Errorf("remove = %d", status)
	}
	if status, _ := call(t, "DELETE", one, token, ""); status != http.StatusNotFound {
		t.Errorf("remove twice = %d, want 404", status)
	}
}
