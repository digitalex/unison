package server

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

// TestLinksAPI (#299): GET/POST …/links and PATCH/DELETE …/links/{id}, and
// the workspace lists its links.
func TestLinksAPI(t *testing.T) {
	ts, st := newServer(t)
	u, _ := st.LoginWithProvider(t.Context(), "github", store.Profile{Subject: "ada", Name: "Ada"})
	userTok, _, _ := st.CreateSession(t.Context(), u.ID, store.SessionCLI, "")
	_, ws := call(t, "POST", ts.URL+"/api/workspaces", userTok, `{"name":"Linked"}`)
	wsID := ws["id"].(string)
	_, token, _, _, _ := st.JoinAsUser(t.Context(), wsID, u.ID, "Ada", "")
	base := ts.URL + "/api/workspaces/" + wsID + "/links"

	if status, _ := call(t, "POST", base, "", `{"name":"Spec","url":"https://example.com/spec"}`); status != http.StatusUnauthorized {
		t.Errorf("unauthenticated add = %d, want 401", status)
	}
	if status, _ := call(t, "POST", base, token, `{"name":"Spec","url":"example.com"}`); status != http.StatusBadRequest {
		t.Errorf("bad URL = %d, want 400", status)
	}
	if status, _ := call(t, "POST", base, token, `{"name":"","url":"https://example.com"}`); status != http.StatusBadRequest {
		t.Errorf("no name = %d, want 400", status)
	}
	status, spec := call(t, "POST", base, token, `{"name":"Spec","url":"https://example.com/spec"}`)
	if status != http.StatusCreated || spec["name"] != "Spec" || spec["position"] != 1.0 {
		t.Fatalf("add = %d %v", status, spec)
	}
	one := base + "/" + strconv.Itoa(int(spec["id"].(float64)))
	if status, got := call(t, "PATCH", one, token, `{"name":"Design doc","url":"https://docs.example.com/d"}`); status != http.StatusOK || got["name"] != "Design doc" || got["url"] != "https://docs.example.com/d" {
		t.Errorf("edit = %d %v", status, got)
	}
	if _, body := getAs(t, base, token); !strings.Contains(body, `"name":"Design doc"`) {
		t.Errorf("list = %s", body)
	}
	if _, body := getAs(t, ts.URL+"/api/workspaces/"+wsID, token); !strings.Contains(body, `"links":[{`) {
		t.Errorf("workspace lacks its links: %s", body)
	}
	if status, _ := call(t, "DELETE", one, token, ""); status != http.StatusOK {
		t.Errorf("remove = %d", status)
	}
	if status, _ := call(t, "DELETE", one, token, ""); status != http.StatusNotFound {
		t.Errorf("remove twice = %d, want 404", status)
	}
}
