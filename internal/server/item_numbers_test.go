package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/digitalex/unison/internal/store"
)

func TestPerWorkspaceItemNumberingAPI(t *testing.T) {
	ts, st := newServer(t)
	ctx := t.Context()
	now := time.Date(2026, 11, 25, 17, 0, 0, 0, time.UTC)
	st.SetClock(func() time.Time { return now })

	alexUser, _ := st.LoginWithProvider(ctx, "github", store.Profile{Subject: "alex", Name: "Alex"})
	wsA, _, _ := st.CreateWorkspaceFor(ctx, alexUser.ID, "Alpha", "", "")
	wsB, _, _ := st.CreateWorkspaceFor(ctx, alexUser.ID, "Beta", "https://github.com/acme/beta", "")
	_, tokA, _, _, _ := st.JoinAsUser(ctx, wsA.ID, alexUser.ID, "Alex", "🧑")
	alexB, tokB, _, _, _ := st.JoinAsUser(ctx, wsB.ID, alexUser.ID, "Alex", "🧑")

	apiA := ts.URL + "/api/workspaces/" + wsA.ID
	apiB := ts.URL + "/api/workspaces/" + wsB.ID

	// Seed 4 items in Alpha so Beta's global IDs start at 5 while its numbers start at #1.
	for i := 1; i <= 4; i++ {
		status, it := call(t, "POST", apiA+"/backlog", tokA, `{"title":"Alpha task"}`)
		if status != http.StatusCreated || it["number"] != float64(i) {
			t.Fatalf("POST Alpha item %d = %d %v", i, status, it)
		}
	}

	// Beta item #1 has number=1, id=5.
	status, b1 := call(t, "POST", apiB+"/backlog", tokB, `{"title":"Beta one","choices":["Yes","No"]}`)
	if status != http.StatusCreated || b1["number"] != 1.0 || b1["id"].(float64) < 5 {
		t.Fatalf("POST Beta #1 = %d %v", status, b1)
	}
	b1GlobalID := int(b1["id"].(float64))

	status, b2 := call(t, "POST", apiB+"/backlog", tokB, `{"title":"Beta two"}`)
	if status != http.StatusCreated || b2["number"] != 2.0 {
		t.Fatalf("POST Beta #2 = %d %v", status, b2)
	}
	b2GlobalID := int(b2["id"].(float64))

	// Deleting #2 returns both id and number, and next created item is #3 (never reuses #2).
	status, delResp := call(t, "DELETE", apiB+"/backlog/2", tokB, "")
	if status != http.StatusOK || delResp["number"] != 2.0 || int(delResp["id"].(float64)) != b2GlobalID {
		t.Fatalf("DELETE /backlog/2 = %d %v", status, delResp)
	}

	status, b3 := call(t, "POST", apiB+"/backlog", tokB, `{"title":"Beta three"}`)
	if status != http.StatusCreated || b3["number"] != 3.0 {
		t.Fatalf("POST Beta #3 after delete = %d %v", status, b3)
	}
	b3GlobalID := int(b3["id"].(float64))

	// Backlog endpoints resolve by per-workspace number (#1) AND legacy global ID (b1GlobalID / b3GlobalID).
	if stCode, got := call(t, "GET", apiB+"/backlog/1", tokB, ""); stCode != http.StatusOK || int(got["id"].(float64)) != b1GlobalID {
		t.Fatalf("GET /backlog/1 = %d %v", stCode, got)
	}
	if stCode, got := call(t, "GET", apiB+"/backlog/"+strconv.Itoa(b1GlobalID), tokB, ""); stCode != http.StatusOK || got["number"] != 1.0 {
		t.Fatalf("GET /backlog/<legacy-id> = %d %v", stCode, got)
	}
	if stCode, got := call(t, "PATCH", apiB+"/backlog/1", tokB, `{"title":"Beta one edited"}`); stCode != http.StatusOK || got["title"] != "Beta one edited" {
		t.Fatalf("PATCH /backlog/1 = %d %v", stCode, got)
	}
	if stCode, _ := call(t, "POST", apiB+"/backlog/"+strconv.Itoa(b3GlobalID)+"/move", tokB, `{"position":1}`); stCode != http.StatusOK {
		t.Fatalf("POST /move by legacy id = %d", stCode)
	}
	if stCode, _ := call(t, "POST", apiB+"/backlog/1/start", tokB, `{}`); stCode != http.StatusOK {
		t.Fatalf("POST /backlog/1/start = %d", stCode)
	}
	if stCode, _ := call(t, "POST", apiB+"/backlog/"+strconv.Itoa(b1GlobalID)+"/stop", tokB, `{}`); stCode != http.StatusOK {
		t.Fatalf("POST /stop by legacy id = %d", stCode)
	}
	if stCode, _ := call(t, "POST", apiB+"/backlog/1/assign", tokB, `{"assignee_id":`+itoa(alexB.ID)+`}`); stCode != http.StatusOK {
		t.Fatalf("POST /backlog/1/assign = %d", stCode)
	}
	if stCode, _ := call(t, "POST", apiB+"/backlog/1/release", tokB, `{}`); stCode != http.StatusOK {
		t.Fatalf("POST /backlog/1/release = %d", stCode)
	}
	if stCode, ans := call(t, "POST", apiB+"/backlog/1/answer", tokB, `{"choice":"Yes"}`); stCode != http.StatusOK || ans["answered_choice"] != "Yes" {
		t.Fatalf("POST /backlog/1/answer = %d %v", stCode, ans)
	}
	if stCode, _ := call(t, "POST", apiB+"/backlog/"+strconv.Itoa(b3GlobalID)+"/close", tokB, `{"note":"https://github.com/acme/beta/pull/7"}`); stCode != http.StatusOK {
		t.Fatalf("POST /close by legacy id = %d", stCode)
	}
	if stCode, _ := call(t, "POST", apiB+"/backlog/3/reopen", tokB, `{}`); stCode != http.StatusOK {
		t.Fatalf("POST /backlog/3/reopen = %d", stCode)
	}
	if stCode, cm := call(t, "POST", apiB+"/backlog/3/comments", tokB, `{"body":"Looks good"}`); stCode != http.StatusCreated || cm["item_number"] != 3.0 {
		t.Fatalf("POST /backlog/3/comments = %d %v", stCode, cm)
	}
	if stCode, comments := callList(t, apiB+"/backlog/"+strconv.Itoa(b3GlobalID)+"/comments", tokB); stCode != http.StatusOK || len(comments) != 1 {
		t.Fatalf("GET /comments by legacy id = %d %v", stCode, comments)
	}

	// POST /messages with item_id as per-workspace number (#3) and legacy global ID (b1GlobalID).
	stCode, msgByNum := call(t, "POST", apiB+"/messages", tokB, `{"body":"Update on #3","item_id":3}`)
	if stCode != http.StatusCreated || msgByNum["item"].(map[string]any)["number"] != 3.0 {
		t.Fatalf("POST /messages item_id=3 = %d %v", stCode, msgByNum)
	}
	stCode, msgByLegacy := call(t, "POST", apiB+"/messages", tokB, `{"body":"Update on #1","item_id":`+strconv.Itoa(b1GlobalID)+`}`)
	if stCode != http.StatusCreated || msgByLegacy["item"].(map[string]any)["number"] != 1.0 {
		t.Fatalf("POST /messages legacy item_id = %d %v", stCode, msgByLegacy)
	}
	if stCode, msgs := callList(t, apiB+"/backlog/3/messages", tokB); stCode != http.StatusOK || len(msgs) != 2 {
		t.Fatalf("GET /backlog/3/messages = %d %v", stCode, msgs)
	}

	// POST /todos with item_id as per-workspace number (#3) and legacy global ID.
	stCode, tdByNum := call(t, "POST", apiB+"/todos", tokB, `{"title":"Sign off #3","assignee":"Alex","item_id":3}`)
	if stCode != http.StatusCreated || tdByNum["item_number"] != 3.0 {
		t.Fatalf("POST /todos item_id=3 = %d %v", stCode, tdByNum)
	}
	stCode, tdByLegacy := call(t, "POST", apiB+"/todos", tokB, `{"title":"Sign off #1","assignee":"Alex","item_id":`+strconv.Itoa(b1GlobalID)+`}`)
	if stCode != http.StatusCreated || tdByLegacy["item_number"] != 1.0 {
		t.Fatalf("POST /todos legacy item_id = %d %v", stCode, tdByLegacy)
	}

	// Artifacts POST / PUT / PATCH / GET with item_id as #number and legacy global ID.
	uploadReq := func(method, url string, body []byte) map[string]any {
		t.Helper()
		req, err := http.NewRequest(method, url, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+tokB)
		req.Header.Set("Content-Type", "text/markdown")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			t.Fatalf("%s %s = %d", method, url, resp.StatusCode)
		}
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	art := uploadReq("POST", apiB+"/artifacts?name=plan.md&item_id=3", []byte("# Plan v1"))
	if art["item_number"] != 3.0 || int(art["item_id"].(float64)) != b3GlobalID {
		t.Fatalf("POST /artifacts?item_id=3 = %v", art)
	}
	if stCode, list := callList(t, apiB+"/artifacts?item_id=3", tokB); stCode != http.StatusOK || len(list) != 1 {
		t.Fatalf("GET /artifacts?item_id=3 = %d %v", stCode, list)
	}
	if stCode, list := callList(t, apiB+"/artifacts?item_id="+strconv.Itoa(b3GlobalID), tokB); stCode != http.StatusOK || len(list) != 1 {
		t.Fatalf("GET /artifacts?item_id=<legacy> = %d %v", stCode, list)
	}
	artPut := uploadReq("PUT", apiB+"/artifacts/plan.md?item_id="+strconv.Itoa(b1GlobalID), []byte("# Plan v2"))
	if artPut["item_number"] != 1.0 {
		t.Fatalf("PUT /artifacts/plan.md?item_id=<legacy> = %v", artPut)
	}
	stCode, artPatch := call(t, "PATCH", apiB+"/artifacts/plan.md", tokB, `{"item_id":3}`)
	if stCode != http.StatusOK || artPatch["item_number"] != 3.0 {
		t.Fatalf("PATCH /artifacts/plan.md item_id=3 = %d %v", stCode, artPatch)
	}

	// Ideas promote returns item.number and idea.promoted_item_number.
	stCode, idea := call(t, "POST", apiB+"/ideas", tokB, `{"title":"Ship dark mode"}`)
	if stCode != http.StatusCreated {
		t.Fatalf("POST /ideas = %d %v", stCode, idea)
	}
	ideaID := strconv.Itoa(int(idea["id"].(float64)))
	stCode, prom := call(t, "POST", apiB+"/ideas/"+ideaID+"/promote", tokB, `{}`)
	if stCode != http.StatusOK || prom["item"].(map[string]any)["number"] != 4.0 || prom["idea"].(map[string]any)["promoted_item_number"] != 4.0 {
		t.Fatalf("POST /ideas/%s/promote = %d %v", ideaID, stCode, prom)
	}
}
