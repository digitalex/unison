package server

import (
	"net/http"
	"strings"
	"testing"
)

// PATCH …/me takes name, emoji and status, each optional; one left out
// stays as it is (#303).
func TestUpdateMe(t *testing.T) {
	ts, st := newServer(t)
	ws, _, _ := st.CreateWorkspace(t.Context(), "W", "", "")
	_, alexTok, _, _ := joinMember(t, st, ws.ID, "Alex", "🦊")
	_, bobTok, _, _ := joinMember(t, st, ws.ID, "Bob", "")
	api := ts.URL + "/api/workspaces/" + ws.ID

	if status, p := call(t, "PATCH", api+"/me", alexTok, `{"status":"reviewing"}`); status != http.StatusOK || p["status"] != "reviewing" || p["name"] != "Alex" {
		t.Fatalf("status only = %d %v", status, p)
	}
	// A rename keeps the status (before #303, leaving it out cleared it).
	if status, p := call(t, "PATCH", api+"/me", alexTok, `{"name":"Lex"}`); status != http.StatusOK || p["name"] != "Lex" || p["emoji"] != "🦊" || p["status"] != "reviewing" {
		t.Fatalf("name only = %d %v", status, p)
	}
	if status, p := call(t, "PATCH", api+"/me", alexTok, `{"emoji":"🐺"}`); status != http.StatusOK || p["name"] != "Lex" || p["emoji"] != "🐺" || p["status"] != "reviewing" {
		t.Fatalf("emoji only = %d %v", status, p)
	}
	if status, p := call(t, "PATCH", api+"/me", alexTok, `{}`); status != http.StatusOK || p["name"] != "Lex" || p["status"] != "reviewing" {
		t.Fatalf("nothing = %d %v", status, p)
	}
	if status, p := call(t, "PATCH", api+"/me", alexTok, `{"status":""}`); status != http.StatusOK || p["status"] != "" {
		t.Fatalf("clearing the status = %d %v", status, p)
	}

	for body, want := range map[string]int{
		`{"name":"two words"}`: http.StatusBadRequest,
		`{"name":"a@b"}`:       http.StatusBadRequest,
		`{"name":""}`:          http.StatusBadRequest,
		`{"emoji":""}`:         http.StatusBadRequest,
		`{"name":"bob"}`:       http.StatusConflict, // Bob's, whatever the case
	} {
		if status, out := call(t, "PATCH", api+"/me", alexTok, body); status != want || out["error"] == nil {
			t.Errorf("PATCH /me %s = %d %v, want %d", body, status, out, want)
		}
	}
	if status, _ := call(t, "PATCH", api+"/me", "", `{"name":"Mallory"}`); status != http.StatusUnauthorized {
		t.Errorf("renaming without a token = %d, want 401", status)
	}

	// Everyone sees the rename: the roster, and the event stream.
	if _, body := getAs(t, api+"/participants", bobTok); !strings.Contains(body, `"name":"Lex"`) || strings.Contains(body, `"name":"Alex"`) {
		t.Errorf("roster after the rename: %s", body)
	}
	if _, body := getAs(t, api+"/events?after=0", bobTok); !strings.Contains(body, `"type":"participant.updated"`) {
		t.Errorf("no participant.updated among the events: %s", body)
	}
	// The new name is the one that reaches Lex; mentions resolve when written.
	if status, m := call(t, "POST", api+"/messages", bobTok, `{"body":"@Lex look at this","action_required":true}`); status != http.StatusCreated || len(m["action_for"].([]any)) != 1 {
		t.Errorf("mentioning the new name = %d %v", status, m)
	}
	if status, _ := call(t, "POST", api+"/messages", bobTok, `{"body":"@Alex look at this","action_required":true}`); status != http.StatusBadRequest {
		t.Errorf("an action for the old name = %d, want 400 (it names nobody now)", status)
	}
}
