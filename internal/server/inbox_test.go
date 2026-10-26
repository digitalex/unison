package server

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

// TestInboxAPI: an agent sends to its member's inbox; only that member (on
// any device, or with their account token) reads and dismisses it; other
// members and other workspaces can't; agents have no inbox to read.
func TestInboxAPI(t *testing.T) {
	ts, st := newServer(t)
	ctx := t.Context()
	ws, _, _ := st.CreateWorkspace(ctx, "W", "", "")
	other, _, _ := st.CreateWorkspace(ctx, "Other", "", "")
	alexP, alexTok, _, _ := joinMember(t, st, ws.ID, "Alex", "")
	_, bobTok, _, _ := joinMember(t, st, ws.ID, "Bob", "")
	_, carolTok, _, _ := joinMember(t, st, other.ID, "Carol", "")
	pass, _, _ := st.MintPass(ctx, ws.ID, alexP.UserID)
	_, wrenTok, _, err := st.JoinWithPass(ctx, ws.ID, pass, "Wren", "")
	if err != nil {
		t.Fatal(err)
	}
	alexUserTok, _, _ := st.CreateSession(ctx, alexP.UserID, store.SessionCLI, "cli")
	api := ts.URL + "/api/workspaces/" + ws.ID + "/inbox"

	status, e := call(t, "POST", api, wrenTok, `{"body":"PR #7 needs your review","url":"https://github.com/o/r/pull/7"}`)
	if status != http.StatusCreated || e["recipient_user_id"] != float64(alexP.UserID) || e["sender_name"] != "Wren" {
		t.Fatalf("send = %d %v", status, e)
	}
	id := strconv.Itoa(int(e["id"].(float64)))
	if status, out := call(t, "POST", api, bobTok, `{"to":"Wren","body":"x"}`); status != http.StatusBadRequest {
		t.Errorf("to an agent = %d %v", status, out)
	}
	if status, _ := call(t, "POST", api, bobTok, `{"body":"x"}`); status != http.StatusBadRequest {
		t.Errorf("a human without --to = %d", status)
	}
	call(t, "POST", api, alexTok, `{"to":"Bob","body":"For Bob"}`)

	for name, tok := range map[string]string{"participant": alexTok, "account token": alexUserTok} {
		if _, body := getAs(t, api, tok); !strings.Contains(body, "PR #7 needs your review") || strings.Contains(body, "For Bob") {
			t.Errorf("Alex's inbox via %s = %s", name, body)
		}
	}
	if _, body := getAs(t, api, bobTok); strings.Contains(body, "PR #7") || !strings.Contains(body, "For Bob") {
		t.Errorf("Bob's inbox = %s", body)
	}
	if status, _ := getAs(t, api, wrenTok); status != http.StatusForbidden {
		t.Errorf("an agent reading an inbox = %d, want 403", status)
	}
	if status, _ := getAs(t, api, carolTok); status != http.StatusForbidden && status != http.StatusUnauthorized {
		t.Errorf("another workspace = %d", status)
	}
	if status, _ := getAs(t, api, ""); status != http.StatusUnauthorized {
		t.Errorf("anonymous = %d", status)
	}
	if status, _ := call(t, "POST", api+"/"+id+"/dismiss", bobTok, ""); status != http.StatusNotFound {
		t.Errorf("Bob dismissing Alex's entry = %d, want 404", status)
	}
	if status, out := call(t, "POST", api+"/"+id+"/dismiss", alexTok, ""); status != http.StatusOK || out["dismissed_at"] == nil {
		t.Errorf("dismiss = %d %v", status, out)
	}
	if _, body := getAs(t, api, alexTok); strings.Contains(body, "PR #7") {
		t.Errorf("dismissed entry still listed: %s", body)
	}
	if _, body := getAs(t, api+"?all=1", alexTok); !strings.Contains(body, "PR #7") {
		t.Errorf("?all=1 = %s", body)
	}
	if status, out := call(t, "POST", api+"/"+id+"/restore", alexUserTok, ""); status != http.StatusOK || out["dismissed_at"] != nil {
		t.Errorf("restore = %d %v", status, out)
	}
	// The stream carries no content: just which inbox changed.
	evs, _ := st.EventsAfter(ctx, ws.ID, 0, 0)
	for _, ev := range evs {
		if strings.HasPrefix(ev.Type, "inbox.") && strings.Contains(string(ev.Data), "PR #7") {
			t.Errorf("%s event leaks the text: %s", ev.Type, ev.Data)
		}
	}
}
