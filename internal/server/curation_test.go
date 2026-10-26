package server

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestCommentsAndPRsAPI(t *testing.T) {
	ts, st := newServer(t)
	ws, _, _ := st.CreateWorkspace(t.Context(), "W", "", "")
	_, ada, _, _ := joinMember(t, st, ws.ID, "Ada", "")
	_, bob, _, _ := joinMember(t, st, ws.ID, "Bob", "")
	base := ts.URL + "/api/workspaces/" + ws.ID + "/backlog"
	_, it := call(t, "POST", base, ada, `{"title":"Fix login"}`)
	item := base + "/" + strconv.Itoa(int(it["id"].(float64)))

	if status, _ := call(t, "POST", item+"/comments", "", `{"body":"hi"}`); status != http.StatusUnauthorized {
		t.Errorf("unauthenticated comment = %d, want 401", status)
	}
	status, c := call(t, "POST", item+"/comments", ada, `{"body":"Where is the bug?"}`)
	if status != http.StatusCreated || c["body"] != "Where is the bug?" {
		t.Fatalf("add comment = %d %v", status, c)
	}
	if status, _ := call(t, "POST", item+"/comments", ada, `{"body":"  "}`); status != http.StatusBadRequest {
		t.Errorf("empty comment = %d, want 400", status)
	}
	if status, _ := call(t, "POST", base+"/999/comments", ada, `{"body":"x"}`); status != http.StatusNotFound {
		t.Errorf("comment on missing item = %d, want 404", status)
	}
	if _, body := getAs(t, item+"/comments", ada); !strings.Contains(body, "Where is the bug?") {
		t.Errorf("list comments = %s", body)
	}
	commentURL := item + "/comments/" + strconv.Itoa(int(c["id"].(float64)))
	if status, _ := call(t, "DELETE", commentURL, bob, ""); status != http.StatusForbidden {
		t.Errorf("deleting another's comment = %d, want 403", status)
	}
	if status, _ := call(t, "DELETE", commentURL, ada, ""); status != http.StatusOK {
		t.Errorf("deleting own comment = %d, want 200", status)
	}

	pr := "https://github.com/digitalex/unison/pull/4"
	status, got := call(t, "POST", item+"/prs", bob, `{"url":"`+pr+`"}`)
	if prs, _ := got["prs"].([]any); status != http.StatusOK || len(prs) != 1 {
		t.Fatalf("link PR = %d %v", status, got)
	}
	if status, _ := call(t, "POST", item+"/prs", bob, `{"url":"`+pr+`"}`); status != http.StatusConflict {
		t.Errorf("duplicate PR = %d, want 409", status)
	}
	if status, _ := call(t, "POST", item+"/prs", bob, `{"url":"not a url"}`); status != http.StatusBadRequest {
		t.Errorf("invalid PR URL = %d, want 400", status)
	}
	if _, body := getAs(t, item, ada); !strings.Contains(body, `"prs":[{`) || !strings.Contains(body, `"comment_count":0`) {
		t.Errorf("item JSON = %s", body)
	}
	status, got = call(t, "DELETE", item+"/prs?url="+url.QueryEscape(pr), ada, "")
	if prs, _ := got["prs"].([]any); status != http.StatusOK || len(prs) != 0 {
		t.Errorf("unlink PR = %d %v", status, got)
	}
}
