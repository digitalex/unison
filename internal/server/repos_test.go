package server

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

func TestReposAPI(t *testing.T) {
	ts, st := newServer(t)
	u, _ := st.LoginWithProvider(t.Context(), "github", store.Profile{Subject: "ada", Name: "Ada"})
	userTok, _, _ := st.CreateSession(t.Context(), u.ID, store.SessionCLI, "")
	status, ws := call(t, "POST", ts.URL+"/api/workspaces", userTok,
		`{"name":"Multi","repo_url":"https://github.com/acme/api","repos":[{"url":"https://github.com/acme/web","label":"frontend"}]}`)
	if repos, _ := ws["repos"].([]any); status != http.StatusCreated || len(repos) != 2 || ws["repo_url"] != "https://github.com/acme/api" {
		t.Fatalf("create = %d %v", status, ws)
	}
	wsID := ws["id"].(string)
	_, token, _, _, _ := st.JoinAsUser(t.Context(), wsID, u.ID, "Ada", "") // The creator is a member.
	base := ts.URL + "/api/workspaces/" + wsID + "/repos"

	if status, _ := call(t, "POST", base, "", `{"url":"https://github.com/acme/docs"}`); status != http.StatusUnauthorized {
		t.Errorf("unauthenticated add = %d, want 401", status)
	}
	// A label from older clients is ignored: repositories go by their URL.
	status, docs := call(t, "POST", base, token, `{"url":"https://github.com/acme/docs","label":"docs"}`)
	if _, hasLabel := docs["label"]; status != http.StatusCreated || docs["position"] != 3.0 || hasLabel {
		t.Fatalf("add = %d %v", status, docs)
	}
	if status, _ := call(t, "POST", base, token, `{"url":"https://github.com/acme/docs"}`); status != http.StatusConflict {
		t.Errorf("duplicate = %d, want 409", status)
	}
	if status, _ := call(t, "POST", base, token, `{"url":"ssh://git@github.com/acme/x"}`); status != http.StatusBadRequest {
		t.Errorf("non-http URL = %d, want 400", status)
	}
	docsURL := base + "/" + strconv.Itoa(int(docs["id"].(float64)))
	if status, got := call(t, "PATCH", docsURL, token, `{"url":"https://github.com/acme/handbook"}`); status != http.StatusOK || got["url"] != "https://github.com/acme/handbook" {
		t.Errorf("edit = %d %v", status, got)
	}
	if status, _ := call(t, "DELETE", docsURL, token, ""); status != http.StatusOK {
		t.Errorf("remove = %d", status)
	}
	if status, _ := call(t, "DELETE", docsURL, token, ""); status != http.StatusNotFound {
		t.Errorf("remove twice = %d, want 404", status)
	}
	if _, body := getAs(t, base, token); !strings.Contains(body, "acme/web") || strings.Contains(body, "handbook") || strings.Contains(body, `"label"`) {
		t.Errorf("list = %s", body)
	}
	// The legacy repo_url PATCH edits the first repo.
	if status, got := call(t, "PATCH", ts.URL+"/api/workspaces/"+wsID, token, `{"repo_url":"https://github.com/acme/backend"}`); status != http.StatusOK || got["repo_url"] != "https://github.com/acme/backend" {
		t.Errorf("PATCH repo_url = %d %v", status, got)
	}
}
