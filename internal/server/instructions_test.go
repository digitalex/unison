package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

func doJSON(t *testing.T, method, url, token, body string) (int, []byte) {
	t.Helper()
	var r *strings.Reader
	if body != "" {
		r = strings.NewReader(body)
	} else {
		r = strings.NewReader("")
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf strings.Builder
	buf.Grow(256)
	_, _ = strings.NewReader("").WriteTo(&buf)
	b := make([]byte, 0, 256)
	tmp := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(tmp)
		if n > 0 {
			b = append(b, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return resp.StatusCode, b
}

func TestWorkspaceInstructionsAPIAndDoorway(t *testing.T) {
	ts, st := newServer(t)
	ctx := t.Context()

	u, err := st.LoginWithProvider(ctx, "github", store.Profile{Subject: "gh-inst", Name: "Alex", Email: "alex@example.com", EmailVerified: true})
	if err != nil {
		t.Fatal(err)
	}
	userTok, _, err := st.CreateSession(ctx, u.ID, store.SessionCLI, "test")
	if err != nil {
		t.Fatal(err)
	}
	ws, _, err := st.CreateWorkspaceFor(ctx, u.ID, "Unison", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, humanTok, _, _, err := st.JoinAsUser(ctx, ws.ID, u.ID, "Alex", "🧑")
	if err != nil {
		t.Fatal(err)
	}
	pass, _, err := st.MintPass(ctx, ws.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, agentTok, _, err := st.JoinWithPass(ctx, ws.ID, pass, "Ada", "🦊")
	if err != nil {
		t.Fatal(err)
	}

	// Doorway without instructions omits the section completely.
	code, raw := doJSON(t, "GET", ts.URL+"/w/"+ws.ID+"/agent.md", userTok, "")
	if code != 200 || strings.Contains(string(raw), "## Workspace instructions") {
		t.Fatalf("empty doorway: status=%d, unexpectedly has instructions section", code)
	}

	// Agent cannot PUT instructions (403).
	code, _ = doJSON(t, "PUT", ts.URL+"/api/workspaces/"+ws.ID+"/instructions", agentTok, `{"instructions":"## Bad\nNo"}`)
	if code != 403 {
		t.Fatalf("agent PUT instructions = %d, want 403", code)
	}

	// Human sets short instructions → inlined in doorway.
	code, raw = doJSON(t, "PUT", ts.URL+"/api/workspaces/"+ws.ID+"/instructions", humanTok,
		`{"instructions":"## Rules\nKeep PRs small.\n## Testing\nRun go test.","instructions_path":".unison/instructions.md"}`)
	if code != 200 {
		t.Fatalf("human PUT instructions = %d: %s", code, raw)
	}
	var inst store.WorkspaceInstructions
	if err := json.Unmarshal(raw, &inst); err != nil {
		t.Fatal(err)
	}
	if len(inst.Headings) != 2 || inst.Path != ".unison/instructions.md" {
		t.Fatalf("unexpected PUT response: %+v", inst)
	}

	// GET with ?section= extracts one section; missing section returns 404.
	code, raw = doJSON(t, "GET", ts.URL+"/api/workspaces/"+ws.ID+"/instructions?section=testing", agentTok, "")
	if code != 200 || !strings.Contains(string(raw), "Run go test.") || strings.Contains(string(raw), "Keep PRs small.") {
		t.Fatalf("GET ?section=testing = %d: %s", code, raw)
	}
	code, _ = doJSON(t, "GET", ts.URL+"/api/workspaces/"+ws.ID+"/instructions?section=nope", agentTok, "")
	if code != 404 {
		t.Fatalf("GET ?section=nope = %d, want 404", code)
	}

	// Doorway inlines short instructions.
	code, raw = doJSON(t, "GET", ts.URL+"/w/"+ws.ID+"/agent.md", userTok, "")
	if code != 200 || !strings.Contains(string(raw), "## Workspace instructions") || !strings.Contains(string(raw), "Keep PRs small.") {
		t.Fatalf("inline doorway = %d:\n%s", code, raw)
	}

	// Long instructions (> InstructionsInlineLimit) are indexed by ## headings in doorway.
	longBody := "## Alpha\n" + strings.Repeat("a", store.InstructionsInlineLimit+50) + "\n## Beta\nMore details."
	payload, _ := json.Marshal(map[string]string{"instructions": longBody})
	code, raw = doJSON(t, "PUT", ts.URL+"/api/workspaces/"+ws.ID+"/instructions", humanTok, string(payload))
	if code != 200 {
		t.Fatalf("PUT long instructions = %d: %s", code, raw)
	}
	code, raw = doJSON(t, "GET", ts.URL+"/w/"+ws.ID+"/agent.md", userTok, "")
	doorway := string(raw)
	if code != 200 || !strings.Contains(doorway, "- Alpha") || !strings.Contains(doorway, "- Beta") || strings.Contains(doorway, "More details.") {
		t.Fatalf("indexed doorway = %d:\n%s", code, doorway)
	}
}
