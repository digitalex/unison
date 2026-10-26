package server

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// TestActionsAPI: an agent asks its member to act with a chat message; the
// member sees it under /actions (and the count on /me) until done; only
// those asked can mark it; a message that mentions nobody is refused; the
// inbox endpoints are gone.
func TestActionsAPI(t *testing.T) {
	ts, st := newServer(t)
	ctx := t.Context()
	ws, _, _ := st.CreateWorkspace(ctx, "W", "", "")
	alexP, alexTok, _, _ := joinMember(t, st, ws.ID, "Alex", "")
	_, bobTok, _, _ := joinMember(t, st, ws.ID, "Bob", "")
	pass, _, _ := st.MintPass(ctx, ws.ID, alexP.UserID)
	_, wrenTok, _, err := st.JoinWithPass(ctx, ws.ID, pass, "Wren", "")
	if err != nil {
		t.Fatal(err)
	}
	api := ts.URL + "/api/workspaces/" + ws.ID

	status, m := call(t, "POST", api+"/messages", wrenTok, `{"body":"@Alex PR #7 needs your review","action_required":true}`)
	if status != http.StatusCreated || m["action_required"] != true || len(m["action_for"].([]any)) != 1 || m["action_for"].([]any)[0] != float64(alexP.ID) {
		t.Fatalf("post = %d %v", status, m)
	}
	id := strconv.Itoa(int(m["id"].(float64)))
	if status, out := call(t, "POST", api+"/messages", wrenTok, `{"body":"anyone?","action_required":true}`); status != http.StatusBadRequest {
		t.Errorf("no mention = %d %v", status, out)
	}

	if _, body := getAs(t, api+"/actions", alexTok); !strings.Contains(body, "PR #7 needs your review") {
		t.Errorf("Alex's actions = %s", body)
	}
	if _, body := getAs(t, api+"/me", alexTok); !strings.Contains(body, `"open_actions":1`) {
		t.Errorf("Alex's /me = %s", body)
	}
	if _, body := getAs(t, api+"/actions", bobTok); strings.TrimSpace(body) != "[]" {
		t.Errorf("Bob's actions = %s", body)
	}
	if status, _ := getAs(t, api+"/actions", ""); status != http.StatusUnauthorized {
		t.Errorf("anonymous = %d", status)
	}

	if status, out := call(t, "POST", api+"/messages/"+id+"/done", bobTok, ``); status != http.StatusBadRequest {
		t.Errorf("Bob marking Alex's = %d %v", status, out)
	}
	if status, out := call(t, "POST", api+"/messages/"+id+"/done", alexTok, ``); status != http.StatusOK || len(out["done_by"].([]any)) != 1 {
		t.Errorf("done = %d %v", status, out)
	}
	if _, body := getAs(t, api+"/me", alexTok); !strings.Contains(body, `"open_actions":0`) {
		t.Errorf("after done, /me = %s", body)
	}
	if status, out := call(t, "DELETE", api+"/messages/"+id+"/done", alexTok, ``); status != http.StatusOK || out["done_by"] != nil {
		t.Errorf("undo = %d %v", status, out)
	}

	if status, _ := getAs(t, api+"/inbox", alexTok); status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
		t.Errorf("GET /inbox = %d, want it gone", status)
	}
}
