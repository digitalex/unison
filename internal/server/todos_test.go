package server

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/digitalex/unison/internal/store"
)

func TestTodosAPIAndAutoClose(t *testing.T) {
	ts, st := newServer(t)
	ctx := t.Context()
	now := time.Date(2026, 11, 25, 16, 0, 0, 0, time.UTC)
	st.SetClock(func() time.Time { return now })

	alexUser, _ := st.LoginWithProvider(ctx, "github", store.Profile{Subject: "alex", Name: "Alex"})
	bobUser, _ := st.LoginWithProvider(ctx, "github", store.Profile{Subject: "bob", Name: "Bob"})
	alexAcctTok, _, _ := st.CreateSession(ctx, alexUser.ID, store.SessionCLI, "test")

	ws1, _, _ := st.CreateWorkspaceFor(ctx, alexUser.ID, "Core", "https://github.com/acme/core", "")
	ws2, _, _ := st.CreateWorkspaceFor(ctx, alexUser.ID, "Infra", "", "")
	alex1, alexTok1, _, _, _ := st.JoinAsUser(ctx, ws1.ID, alexUser.ID, "Alex", "🧑")
	_, bobTok1, _, _, _ := st.JoinAsUser(ctx, ws1.ID, bobUser.ID, "Bob", "🐙")
	alex2, alexTok2, _, _, _ := st.JoinAsUser(ctx, ws2.ID, alexUser.ID, "Alex", "🧑")

	pass, _, _ := st.MintPass(ctx, ws1.ID, alexUser.ID)
	wren, wrenTok, _, err := st.JoinWithPass(ctx, ws1.ID, pass, "Wren", "🪶")
	if err != nil {
		t.Fatal(err)
	}

	api1 := ts.URL + "/api/workspaces/" + ws1.ID
	api2 := ts.URL + "/api/workspaces/" + ws2.ID

	_, item := call(t, "POST", api1+"/backlog", wrenTok, `{"title":"Ship OAuth"}`)
	itemID := strconv.Itoa(int(item["id"].(float64)))

	// Reject agent assignees and non-existent items with 400.
	if status, _ := call(t, "POST", api1+"/todos", wrenTok, `{"title":"Agent todo","assignee_id":`+itoa(wren.ID)+`}`); status != http.StatusBadRequest {
		t.Errorf("assigning TODO to agent = %d, want 400", status)
	}
	if status, _ := call(t, "POST", api1+"/todos", wrenTok, `{"title":"Missing item","assignee":"Alex","item_id":999}`); status != http.StatusBadRequest {
		t.Errorf("non-existent item = %d, want 400", status)
	}

	// Create TODOs in both workspaces.
	status, td1 := call(t, "POST", api1+"/todos", wrenTok,
		`{"title":"Review PR #14","body":"Needs security sign-off","assignee":"Alex","item_id":`+itemID+`,"pr_url":"https://github.com/acme/core/pull/14"}`)
	if status != http.StatusCreated || td1["assignee_id"] != float64(alex1.ID) {
		t.Fatalf("POST /todos ws1 = %d %v", status, td1)
	}
	td1ID := strconv.Itoa(int(td1["id"].(float64)))

	status, td2 := call(t, "POST", api2+"/todos", alexTok2,
		`{"title":"Approve DNS record","assignee_id":`+itoa(alex2.ID)+`}`)
	if status != http.StatusCreated {
		t.Fatalf("POST /todos ws2 = %d %v", status, td2)
	}
	td2ID := strconv.Itoa(int(td2["id"].(float64)))

	// GET /me includes open_todos.
	if _, me := call(t, "GET", api1+"/me", alexTok1, ""); me["open_todos"] != 1.0 {
		t.Errorf("GET /me open_todos = %v, want 1", me["open_todos"])
	}

	// Cross-workspace GET /api/me/todos returns both active TODOs with workspace_name.
	if status, _ := getAs(t, ts.URL+"/api/me/todos", ""); status != http.StatusUnauthorized {
		t.Errorf("unauthenticated /api/me/todos = %d, want 401", status)
	}
	status, myTodos := callList(t, ts.URL+"/api/me/todos", alexAcctTok)
	if status != http.StatusOK || len(myTodos) != 2 {
		t.Fatalf("GET /api/me/todos = %d %v, want 2", status, myTodos)
	}

	// Snooze 1d hides from active & open_todos & /api/me/todos?status=active, then reappears after 24h.
	status, snoozed := call(t, "POST", api2+"/todos/"+td2ID+"/snooze", alexTok2, `{"duration":"1d"}`)
	if status != http.StatusOK || snoozed["snoozed_until"] == nil {
		t.Fatalf("snooze 1d = %d %v", status, snoozed)
	}
	if _, me := call(t, "GET", api2+"/me", alexTok2, ""); me["open_todos"] != 0.0 {
		t.Errorf("open_todos while snoozed = %v, want 0", me["open_todos"])
	}
	if _, active := callList(t, ts.URL+"/api/me/todos?status=active", alexAcctTok); len(active) != 1 {
		t.Errorf("active /api/me/todos while snoozed = %v, want 1", active)
	}
	if _, sn := callList(t, api2+"/todos?status=snoozed", alexTok2); len(sn) != 1 {
		t.Errorf("snoozed list = %v, want 1", sn)
	}
	now = now.Add(12 * time.Hour)
	getAs(t, api1+"/me", wrenTok) // keep Wren active across 24h idle window
	now = now.Add(13 * time.Hour)
	if _, me := call(t, "GET", api2+"/me", alexTok2, ""); me["open_todos"] != 1.0 {
		t.Errorf("open_todos after snooze elapsed = %v, want 1", me["open_todos"])
	}

	// Agent done requires note (400 without note, 200 with note); human done works without note.
	if status, out := call(t, "POST", api1+"/todos/"+td1ID+"/done", wrenTok, `{}`); status != http.StatusBadRequest {
		t.Errorf("agent done without note = %d %v, want 400", status, out)
	}
	if status, out := call(t, "POST", api1+"/todos/"+td1ID+"/done", wrenTok, `{"note":"Checked and verified"}`); status != http.StatusOK || out["done_note"] != "Checked and verified" {
		t.Fatalf("agent done with note = %d %v", status, out)
	}
	if status, out := call(t, "POST", api1+"/todos/"+td1ID+"/reopen", bobTok1, `{}`); status != http.StatusOK || out["status"] != "open" {
		t.Fatalf("reopen = %d %v", status, out)
	}
	if status, out := call(t, "POST", api2+"/todos/"+td2ID+"/done", alexTok2, `{}`); status != http.StatusOK || out["status"] != "done" {
		t.Fatalf("human done without note = %d %v", status, out)
	}

	// GitHub pull_request closed webhook auto-closes td1 in ws1!
	_, hook := call(t, "POST", api1+"/webhooks", alexTok1, `{"name":"GitHub","instructions":"Watch PRs"}`)
	hookURL := hook["url"].(string)
	payload := []byte(`{"action":"closed","number":14,"pull_request":{"number":14,"title":"Ship OAuth","merged":true,"html_url":"https://github.com/acme/core/pull/14"}}`)
	headers := map[string]string{"Content-Type": "application/json", "X-GitHub-Event": "pull_request"}
	if code := postHook(t, hookURL, payload, headers); code != http.StatusAccepted {
		t.Fatalf("postHook = %d, want 202", code)
	}
	_, doneTodos := callList(t, api1+"/todos?status=done", alexTok1)
	if len(doneTodos) != 1 || doneTodos[0]["id"] != td1["id"] || doneTodos[0]["done_note"] != "Auto-closed: PR merged" {
		t.Fatalf("after PR merged webhook, doneTodos = %v", doneTodos)
	}

	// Also verify /reviews refresh auto-closes open PR-linked TODOs whose PR is closed on GitHub.
	_, td3 := call(t, "POST", api1+"/todos", wrenTok, `{"title":"Review PR #22","assignee":"Alex","pr_url":"https://github.com/acme/core/pull/22"}`)
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/core/pulls":
			w.Write([]byte(`[]`))
		case "/repos/acme/core/pulls/22":
			w.Write([]byte(`{"state":"closed","merged":false}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer gh.Close()
	defer func(old string) { githubAPI = old }(githubAPI)
	githubAPI = gh.URL

	call(t, "PATCH", api1, alexTok1, `{"github_token":"ghp_test"}`)
	if status, _ := call(t, "GET", api1+"/reviews", alexTok1, ""); status != http.StatusOK {
		t.Fatalf("GET /reviews = %d", status)
	}
	_, doneAfterReviews := callList(t, api1+"/todos?status=done", alexTok1)
	foundTd3 := false
	for _, d := range doneAfterReviews {
		if d["id"] == td3["id"] && d["done_note"] == "Auto-closed: PR closed" {
			foundTd3 = true
		}
	}
	if !foundTd3 {
		t.Errorf("expected td3 auto-closed on /reviews refresh, got %v", doneAfterReviews)
	}

	// Delete TODO.
	if status, _ := call(t, "DELETE", api2+"/todos/"+td2ID, alexTok2, ""); status != http.StatusOK {
		t.Errorf("DELETE /todos/%s = %d", td2ID, status)
	}
	if _, body := getAs(t, api2+"/events?after=0", alexTok2); !strings.Contains(body, `"todo.deleted"`) {
		t.Errorf("events missing todo.deleted: %s", body)
	}
}
