package server

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

// TestMessagesAboutItems: POST /messages with item_id ties the message to
// an item of the same workspace (400 otherwise); the item counts it and
// GET …/backlog/{id}/messages lists it, behind the usual gate.
func TestMessagesAboutItems(t *testing.T) {
	ts, st := newServer(t)
	ws, _, _ := st.CreateWorkspace(t.Context(), "W", "", "")
	other, _, _ := st.CreateWorkspace(t.Context(), "Other", "", "")
	_, ada, _, _ := joinMember(t, st, ws.ID, "Ada", "")
	_, carol, _, _ := joinMember(t, st, other.ID, "Carol", "")
	api := ts.URL + "/api/workspaces/" + ws.ID
	_, it := call(t, "POST", api+"/backlog", ada, `{"title":"Fix login"}`)
	id := strconv.Itoa(int(it["id"].(float64)))
	_, theirs := call(t, "POST", ts.URL+"/api/workspaces/"+other.ID+"/backlog", carol, `{"title":"Theirs"}`)
	theirID := strconv.Itoa(int(theirs["id"].(float64)))

	status, m := call(t, "POST", api+"/messages", ada, `{"body":"On it","item_id":`+id+`}`)
	if item, _ := m["item"].(map[string]any); status != http.StatusCreated || m["item_id"] != it["id"] || item["title"] != "Fix login" {
		t.Fatalf("post with item = %d %v", status, m)
	}
	for _, bad := range []string{theirID, "999"} {
		if status, out := call(t, "POST", api+"/messages", ada, `{"body":"x","item_id":`+bad+`}`); status != http.StatusBadRequest {
			t.Errorf("item_id %s = %d %v, want 400", bad, status, out)
		}
	}
	if status, _ := call(t, "POST", api+"/messages", ada, `{"body":"plain"}`); status != http.StatusCreated {
		t.Errorf("plain message = %d", status)
	}

	if _, body := getAs(t, api+"/backlog/"+id, ada); !strings.Contains(body, `"message_count":1`) {
		t.Errorf("item JSON = %s", body)
	}
	status, body := getAs(t, api+"/backlog/"+id+"/messages", ada)
	if status != http.StatusOK || !strings.Contains(body, "On it") || strings.Contains(body, "plain") {
		t.Errorf("item messages = %d %s", status, body)
	}
	if status, _ := getAs(t, api+"/backlog/999/messages", ada); status != http.StatusNotFound {
		t.Errorf("messages of a missing item = %d, want 404", status)
	}
	if _, body := getAs(t, api+"/messages", ada); !strings.Contains(body, `"item_id":`+id) {
		t.Errorf("chat log = %s", body)
	}
	for name, tok := range map[string]string{"anonymous": "", "another workspace": carol} {
		if status, _ := getAs(t, api+"/backlog/"+id+"/messages", tok); status != http.StatusUnauthorized && status != http.StatusForbidden {
			t.Errorf("%s = %d, want 401/403", name, status)
		}
	}
}

// TestReactionsAPI: PUT and DELETE …/messages/{id}/reactions/{emoji} (an
// emoji, URL-encoded, or a name) toggle your reaction; messages list their
// reactions; other emoji are refused; the stream hears message.reacted.
func TestReactionsAPI(t *testing.T) {
	ts, st := newServer(t)
	ws, _, _ := st.CreateWorkspace(t.Context(), "W", "", "")
	other, _, _ := st.CreateWorkspace(t.Context(), "Other", "", "")
	_, ada, _, _ := joinMember(t, st, ws.ID, "Ada", "")
	bob, bobTok, _, _ := joinMember(t, st, ws.ID, "Bob", "")
	_, carol, _, _ := joinMember(t, st, other.ID, "Carol", "")
	api := ts.URL + "/api/workspaces/" + ws.ID
	_, m := call(t, "POST", api+"/messages", ada, `{"body":"Shipped"}`)
	react := api + "/messages/" + strconv.Itoa(int(m["id"].(float64))) + "/reactions/"

	status, got := call(t, "PUT", react+url.PathEscape("👍"), bobTok, "")
	rs, _ := got["reactions"].([]any)
	if status != http.StatusOK || len(rs) != 1 {
		t.Fatalf("PUT 👍 = %d %v", status, got)
	}
	if r := rs[0].(map[string]any); r["emoji"] != "👍" || r["count"] != 1.0 || r["by"].([]any)[0] != float64(bob.ID) {
		t.Errorf("reaction = %v", r)
	}
	call(t, "PUT", react+"heart", ada, "")
	if _, body := getAs(t, api+"/messages", ada); !strings.Contains(body, `"reactions":[{"emoji":"👍","count":1`) || !strings.Contains(body, `"emoji":"❤️"`) {
		t.Errorf("messages = %s", body)
	}
	if status, _ := call(t, "PUT", react+url.PathEscape("🎉"), ada, ""); status != http.StatusBadRequest {
		t.Errorf("PUT 🎉 = %d, want 400", status)
	}
	if status, _ := call(t, "PUT", react+"+1", carol, ""); status != http.StatusForbidden && status != http.StatusUnauthorized {
		t.Errorf("another workspace's participant = %d", status)
	}
	if status, _ := call(t, "PUT", react+"+1", "", ""); status != http.StatusUnauthorized {
		t.Errorf("anonymous = %d", status)
	}
	if status, _ := call(t, "PUT", api+"/messages/999/reactions/+1", ada, ""); status != http.StatusNotFound {
		t.Errorf("missing message = %d, want 404", status)
	}
	status, got = call(t, "DELETE", react+"thumbsup", bobTok, "")
	if rs, _ := got["reactions"].([]any); status != http.StatusOK || len(rs) != 1 {
		t.Errorf("DELETE = %d %v", status, got)
	}
	evs, _ := st.EventsAfter(t.Context(), ws.ID, 0, 0)
	if n := countType(evs, "message.reacted"); n != 3 {
		t.Errorf("message.reacted events = %d, want 3", n)
	}
}

func countType(evs []store.Event, typ string) int {
	n := 0
	for _, e := range evs {
		if e.Type == typ {
			n++
		}
	}
	return n
}
