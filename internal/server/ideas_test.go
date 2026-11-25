package server

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// TestIdeasAPI (#917): the idea endpoints, their auth, and Move to backlog.
func TestIdeasAPI(t *testing.T) {
	ts, st := newServer(t)
	ws, _, _ := st.CreateWorkspace(t.Context(), "W", "", "")
	_, token, _, _ := joinMember(t, st, ws.ID, "Ada", "")
	_, other, _, _ := joinMember(t, st, ws.ID, "Bo", "")
	base := ts.URL + "/api/workspaces/" + ws.ID + "/ideas"

	if status, _ := call(t, "POST", base, "", `{"title":"Dark mode"}`); status != http.StatusUnauthorized {
		t.Errorf("unauthenticated add = %d, want 401", status)
	}
	if status, _ := call(t, "POST", base, token, `{"title":" "}`); status != http.StatusBadRequest {
		t.Errorf("no title = %d, want 400", status)
	}
	status, idea := call(t, "POST", base, token, `{"title":"Dark mode","body":"Users ask for it"}`)
	if status != http.StatusCreated || idea["status"] != "open" || idea["comment_count"] != 0.0 {
		t.Fatalf("add = %d %v", status, idea)
	}
	one := base + "/" + strconv.Itoa(int(idea["id"].(float64)))
	if status, got := call(t, "PATCH", one, other, `{"body":"Everyone asks for it"}`); status != http.StatusOK || got["body"] != "Everyone asks for it" || got["title"] != "Dark mode" {
		t.Errorf("edit = %d %v", status, got)
	}
	status, c := call(t, "POST", one+"/comments", other, `{"body":"Sidebar first"}`)
	if status != http.StatusCreated || c["body"] != "Sidebar first" {
		t.Fatalf("comment = %d %v", status, c)
	}
	cid := strconv.Itoa(int(c["id"].(float64)))
	if status, _ := call(t, "DELETE", one+"/comments/"+cid, token, ""); status != http.StatusForbidden {
		t.Errorf("deleting another's comment = %d, want 403", status)
	}
	if _, body := getAs(t, one+"/comments", token); !strings.Contains(body, "Sidebar first") {
		t.Errorf("comments = %s", body)
	}
	if _, body := getAs(t, base, token); !strings.Contains(body, `"comment_count":1`) {
		t.Errorf("list = %s", body)
	}

	status, out := call(t, "POST", one+"/promote", token, `{"position":1}`)
	if status != http.StatusOK {
		t.Fatalf("promote = %d %v", status, out)
	}
	item, _ := out["item"].(map[string]any)
	promoted, _ := out["idea"].(map[string]any)
	if item == nil || promoted == nil || promoted["status"] != "promoted" || promoted["promoted_item_id"] != item["id"] ||
		!strings.Contains(item["body"].(string), "Sidebar first") {
		t.Fatalf("promote = %v", out)
	}
	if _, body := getAs(t, ts.URL+"/api/workspaces/"+ws.ID+"/backlog", token); !strings.Contains(body, `"title":"Dark mode"`) {
		t.Errorf("backlog lacks the promoted idea: %s", body)
	}
	if status, _ := call(t, "POST", one+"/promote", token, ""); status != http.StatusConflict {
		t.Errorf("promote twice = %d, want 409", status)
	}
	if _, body := getAs(t, base, token); body != "[]\n" {
		t.Errorf("open ideas = %s, want none", body)
	}
	if _, body := getAs(t, base+"?status=all", token); !strings.Contains(body, `"status":"promoted"`) {
		t.Errorf("all ideas = %s", body)
	}

	if status, _ := call(t, "DELETE", one+"/comments/"+cid, other, ""); status != http.StatusOK {
		t.Errorf("delete own comment = %d", status)
	}
	if status, _ := call(t, "DELETE", one, token, ""); status != http.StatusOK {
		t.Errorf("delete = %d", status)
	}
	if status, _ := getAs(t, one, token); status != http.StatusNotFound {
		t.Errorf("get deleted = %d, want 404", status)
	}
}
