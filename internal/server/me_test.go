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

// An agent reports its model on joining with a pass, and changes it with
// PATCH …/me (#304).
func TestAgentModel(t *testing.T) {
	ts, st := newServer(t)
	ctx := t.Context()
	ws, _, _ := st.CreateWorkspace(ctx, "W", "", "")
	alexP, alexTok, _, _ := joinMember(t, st, ws.ID, "Alex", "")
	api := ts.URL + "/api/workspaces/" + ws.ID
	pass := func() string {
		t.Helper()
		p, _, err := st.MintPass(ctx, ws.ID, alexP.UserID)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	status, out := call(t, "POST", api+"/join", "", `{"pass":"`+pass()+`","name":"Quill","model":"sonnet-5"}`)
	if status != http.StatusCreated {
		t.Fatalf("join = %d %v", status, out)
	}
	quill, tok := out["participant"].(map[string]any), out["token"].(string)
	if quill["model"] != "sonnet-5" {
		t.Fatalf("joined participant = %v, want its model", quill)
	}
	// A bad model is refused before the pass is spent.
	bad := pass()
	if status, out := call(t, "POST", api+"/join", "", `{"pass":"`+bad+`","name":"Lark","model":"two\nlines"}`); status != http.StatusBadRequest {
		t.Errorf("join with a two-line model = %d %v, want 400", status, out)
	}
	if status, out := call(t, "POST", api+"/join", "", `{"pass":"`+bad+`","name":"Lark"}`); status != http.StatusCreated {
		t.Errorf("the same pass afterwards = %d %v, want it still good", status, out)
	}

	if status, p := call(t, "PATCH", api+"/me", tok, `{"model":"opus-5"}`); status != http.StatusOK || p["model"] != "opus-5" || p["name"] != "Quill" {
		t.Fatalf("model only = %d %v", status, p)
	}
	if status, p := call(t, "PATCH", api+"/me", tok, `{"status":"on #4"}`); status != http.StatusOK || p["model"] != "opus-5" {
		t.Fatalf("a status change dropped the model: %d %v", status, p)
	}
	if status, out := call(t, "PATCH", api+"/me", tok, `{"model":"`+strings.Repeat("m", 65)+`"}`); status != http.StatusBadRequest {
		t.Errorf("a 65-character model = %d %v, want 400", status, out)
	}
	// Teammates see it on the roster; members who said nothing have no model.
	_, roster := getAs(t, api+"/participants", alexTok)
	if !strings.Contains(roster, `"model":"opus-5"`) || strings.Count(roster, `"model"`) != 1 {
		t.Errorf("roster: %s", roster)
	}
	if status, p := call(t, "PATCH", api+"/me", tok, `{"model":""}`); status != http.StatusOK || p["model"] != nil {
		t.Fatalf("clearing = %d %v", status, p)
	}

	// The invite a member pastes into an agent names the flag.
	if prompt := invitePrompt(ts.URL, ws, "Alex", "up_x"); !strings.Contains(prompt, "--pass up_x --name <YourName> --emoji <emoji> --model <model>") {
		t.Errorf("invite prompt = %q", prompt)
	}
}
