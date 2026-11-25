package server

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

func TestSectionsAndDecisionChoicesAPI(t *testing.T) {
	ts, st := newServer(t)
	ctx := t.Context()

	alexUser, _ := st.LoginWithProvider(ctx, "github", store.Profile{Subject: "alex", Name: "Alex"})
	alexAcctTok, _, _ := st.CreateSession(ctx, alexUser.ID, store.SessionCLI, "cli")

	ws, _, _ := st.CreateWorkspaceFor(ctx, alexUser.ID, "Platform", "", "")
	alex, alexTok, _, _, _ := st.JoinAsUser(ctx, ws.ID, alexUser.ID, "Alex", "🧑")

	pass, _, _ := st.MintPass(ctx, ws.ID, alexUser.ID)
	_, wrenTok, _, err := st.JoinWithPass(ctx, ws.ID, pass, "Wren", "🪶")
	if err != nil {
		t.Fatal(err)
	}

	api := ts.URL + "/api/workspaces/" + ws.ID

	// Initially empty sections.
	status, secs := callList(t, api+"/sections", alexTok)
	if status != http.StatusOK || len(secs) != 0 {
		t.Fatalf("initial GET /sections = %d %v", status, secs)
	}

	// Create Triage and Decisions sections.
	status, triage := call(t, "POST", api+"/sections", alexTok, `{"name":"Triage","icon":"inbox"}`)
	if status != http.StatusCreated || triage["slug"] != "triage" || triage["icon"] != "inbox" || triage["notify"] != false {
		t.Fatalf("POST /sections triage = %d %v", status, triage)
	}
	status, decisions := call(t, "POST", api+"/sections", alexTok, `{"name":"Decisions","icon":"scale","notify":true}`)
	if status != http.StatusCreated || decisions["slug"] != "decisions" || decisions["notify"] != true {
		t.Fatalf("POST /sections decisions = %d %v", status, decisions)
	}
	decID := strconv.Itoa(int(decisions["id"].(float64)))

	// Move Decisions to position 1 via slug, then PATCH icon via numeric ID.
	status, moved := call(t, "POST", api+"/sections/decisions/move", alexTok, `{"position":1}`)
	if status != http.StatusOK || moved["position"] != 1.0 {
		t.Fatalf("POST /sections/decisions/move = %d %v", status, moved)
	}
	status, patched := call(t, "PATCH", api+"/sections/"+decID, alexTok, `{"icon":"clipboard-check"}`)
	if status != http.StatusOK || patched["icon"] != "clipboard-check" {
		t.Fatalf("PATCH /sections/{id} = %d %v", status, patched)
	}

	// Create an unassigned item in Triage: main Backlog stays empty and claim returns 404.
	status, trItem := call(t, "POST", api+"/backlog", wrenTok, `{"title":"Investigate flaky test","section":"triage"}`)
	if status != http.StatusCreated || trItem["section_slug"] != "triage" {
		t.Fatalf("POST /backlog triage item = %d %v", status, trItem)
	}
	if _, mainItems := callList(t, api+"/backlog", wrenTok); len(mainItems) != 0 {
		t.Fatalf("GET /backlog (default main) = %v, want empty", mainItems)
	}
	if _, trItems := callList(t, api+"/backlog?section=triage", wrenTok); len(trItems) != 1 {
		t.Fatalf("GET /backlog?section=triage = %v, want 1", trItems)
	}
	if status, _ := call(t, "POST", api+"/backlog/claim", wrenTok, `{}`); status != http.StatusNotFound {
		t.Fatalf("POST /backlog/claim with only section item = %d, want 404", status)
	}

	// Create two structured-choice items in Decisions (notify=true) assigned to Alex:
	// one with close_on_answer=false, one with close_on_answer=true.
	status, keepOpen := call(t, "POST", api+"/backlog", wrenTok,
		`{"title":"API pagination style","section":"decisions","assignee_id":`+itoa(alex.ID)+`,"choices":["Cursor","Offset"],"close_on_answer":false}`)
	if status != http.StatusCreated {
		t.Fatalf("POST /backlog keepOpen = %d %v", status, keepOpen)
	}
	keepOpenID := strconv.Itoa(int(keepOpen["id"].(float64)))

	status, autoClose := call(t, "POST", api+"/backlog", wrenTok,
		`{"title":"Release v1.2 today?","section":"decisions","assignee_id":`+itoa(alex.ID)+`,"choices":["Ship it","Wait until tomorrow"],"close_on_answer":true}`)
	if status != http.StatusCreated {
		t.Fatalf("POST /backlog autoClose = %d %v", status, autoClose)
	}
	autoCloseID := strconv.Itoa(int(autoClose["id"].(float64)))

	// GET /backlog?section=all returns all 3 items across Triage & Decisions.
	if _, allItems := callList(t, api+"/backlog?section=all", alexTok); len(allItems) != 3 {
		t.Fatalf("GET /backlog?section=all = %v, want 3", allItems)
	}

	// GET /me open_todos, GET /todos, and GET /api/me/todos surface both notify-section items!
	if _, me := call(t, "GET", api+"/me", alexTok, ""); me["open_todos"] != 2.0 {
		t.Fatalf("GET /me open_todos = %v, want 2", me["open_todos"])
	}
	if _, wsTodos := callList(t, api+"/todos?assignee=me", alexTok); len(wsTodos) != 2 {
		t.Fatalf("GET /todos?assignee=me = %v, want 2", wsTodos)
	}
	if _, myTodos := callList(t, ts.URL+"/api/me/todos", alexAcctTok); len(myTodos) != 2 || myTodos[0]["kind"] != "section_item" {
		t.Fatalf("GET /api/me/todos = %v, want 2 section_item entries", myTodos)
	}

	// Answer keepOpen -> records choice, stays open.
	status, ansOpen := call(t, "POST", api+"/backlog/"+keepOpenID+"/answer", alexTok, `{"choice":"Cursor"}`)
	if status != http.StatusOK || ansOpen["status"] != "open" || ansOpen["answered_choice"] != "Cursor" {
		t.Fatalf("POST /answer keepOpen = %d %v", status, ansOpen)
	}

	// Answer autoClose -> records choice AND atomically closes item!
	status, ansClosed := call(t, "POST", api+"/backlog/"+autoCloseID+"/answer", alexTok, `{"choice":"Ship it"}`)
	if status != http.StatusOK || ansClosed["status"] != "done" || ansClosed["answered_choice"] != "Ship it" {
		t.Fatalf("POST /answer autoClose = %d %v", status, ansClosed)
	}
	if _, me := call(t, "GET", api+"/me", alexTok, ""); me["open_todos"] != 1.0 {
		t.Fatalf("GET /me open_todos after auto-close = %v, want 1", me["open_todos"])
	}

	// Move keepOpen back to main Backlog via PATCH {"section_id": 0}, then delete Triage section.
	status, movedBack := call(t, "PATCH", api+"/backlog/"+keepOpenID, alexTok, `{"section_id":0}`)
	if status != http.StatusOK || movedBack["section_id"] != nil {
		t.Fatalf("PATCH section_id:0 = %d %v", status, movedBack)
	}
	if status, _ := call(t, "DELETE", api+"/sections/triage", alexTok, `{}`); status != http.StatusOK {
		t.Fatalf("DELETE /sections/triage = %d", status)
	}
	if _, mainItems := callList(t, api+"/backlog", alexTok); len(mainItems) != 2 {
		t.Fatalf("main backlog after moving & deleting section = %v, want 2 items", mainItems)
	}
}
