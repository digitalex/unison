package server

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestFeedbackAPI(t *testing.T) {
	ts, st := newServer(t)
	ws, _, _ := st.CreateWorkspace(t.Context(), "W", "", "")
	_, token, _, _ := joinMember(t, st, ws.ID, "Ada", "")
	_, other, _, _ := joinMember(t, st, ws.ID, "Bo", "")
	wsURL := ts.URL + "/api/workspaces/" + ws.ID
	base := wsURL + "/feedback"

	// Toggle feedback_enabled on the workspace.
	if status, got := call(t, "PATCH", wsURL, token, `{"feedback_enabled":true}`); status != http.StatusOK || got["feedback_enabled"] != true {
		t.Fatalf("enable feedback = %d %v", status, got)
	}

	if status, _ := call(t, "POST", base, "", `{"title":"Broken button"}`); status != http.StatusUnauthorized {
		t.Errorf("unauthenticated create = %d, want 401", status)
	}
	if status, _ := call(t, "POST", base, token, `{"title":" "}`); status != http.StatusBadRequest {
		t.Errorf("empty title = %d, want 400", status)
	}

	imgURL := "/api/workspaces/" + ws.ID + "/images/3"
	status, fb := call(t, "POST", base, token, `{"type":"bug","severity":"critical","title":"Cropped footer","body":"Cut off on mobile","image_url":"`+imgURL+`","page_url":"/w/`+ws.ID+`"}`)
	if status != http.StatusCreated || fb["kind"] != "bug" || fb["severity"] != "critical" || fb["status"] != "open" {
		t.Fatalf("create = %d %v", status, fb)
	}

	one := base + "/" + strconv.Itoa(int(fb["id"].(float64)))
	if status, got := call(t, "PATCH", one, other, `{"severity":"high","title":"Mobile footer clipped"}`); status != http.StatusOK || got["severity"] != "high" || got["title"] != "Mobile footer clipped" {
		t.Fatalf("patch = %d %v", status, got)
	}

	// Dismiss and reopen.
	if status, got := call(t, "POST", one+"/dismiss", other, ""); status != http.StatusOK || got["status"] != "dismissed" {
		t.Fatalf("dismiss = %d %v", status, got)
	}
	if status, got := call(t, "POST", one+"/reopen", token, ""); status != http.StatusOK || got["status"] != "open" {
		t.Fatalf("reopen = %d %v", status, got)
	}

	// Promote to backlog.
	status, out := call(t, "POST", one+"/promote", other, `{"position":1}`)
	if status != http.StatusOK {
		t.Fatalf("promote = %d %v", status, out)
	}
	item, _ := out["item"].(map[string]any)
	promoted, _ := out["feedback"].(map[string]any)
	if item == nil || promoted == nil || promoted["status"] != "promoted" || promoted["promoted_item_id"] != item["id"] ||
		!strings.Contains(item["body"].(string), imgURL) {
		t.Fatalf("promote = %v", out)
	}
	if status, _ := call(t, "POST", one+"/promote", other, ""); status != http.StatusConflict {
		t.Errorf("promote twice = %d, want 409", status)
	}

	if status, _ := call(t, "DELETE", one, token, ""); status != http.StatusOK {
		t.Errorf("delete = %d", status)
	}
	if status, _ := getAs(t, one, token); status != http.StatusNotFound {
		t.Errorf("get deleted = %d, want 404", status)
	}
}
