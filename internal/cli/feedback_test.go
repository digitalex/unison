package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

func TestFeedbackCLI(t *testing.T) {
	e := newTestEnv(t)
	alexDir, agentDir := t.TempDir(), t.TempDir()
	e.login("Alex", "")
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(alexDir, "workspace", "create", "FeedbackTest", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	invite := e.url + "/w/" + ws.ID
	e.ok(alexDir, "join", invite, "--name", "Alex", "--account")
	e.ok(agentDir, "join", invite, "--pass", e.passFor(alexDir), "--name", "Ada")

	// Enable feedback via CLI and verify workspace show includes it.
	if out := e.ok(alexDir, "feedback", "enable"); !strings.Contains(out, "Enabled Send feedback") {
		t.Fatalf("feedback enable = %q", out)
	}
	if out := e.ok(alexDir, "workspace"); !strings.Contains(out, "Feedback   enabled") {
		t.Errorf("workspace show = %q, want Feedback enabled", out)
	}

	// Add feedback with a local PNG file (uploaded automatically via /images).
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")
	shot := filepath.Join(alexDir, "crop.png")
	if err := os.WriteFile(shot, png, 0o644); err != nil {
		t.Fatal(err)
	}

	var fb store.Feedback
	raw := e.ok(alexDir, "feedback", "add", "Dialog spacing cramped", "--type", "bug", "--severity", "high", "--body", "Needs more breathing room", "--image", shot, "--page", "/w/"+ws.ID, "--json")
	if err := json.Unmarshal([]byte(raw), &fb); err != nil {
		t.Fatal(err)
	}
	if fb.Kind != "bug" || fb.Severity != "high" || !strings.HasPrefix(fb.ImageURL, "/api/workspaces/"+ws.ID+"/images/") {
		t.Fatalf("added feedback = %+v", fb)
	}

	// List & show.
	if ls := e.ok(agentDir, "feedback", "ls"); !strings.Contains(ls, "Dialog spacing cramped") || !strings.Contains(ls, "[screenshot]") {
		t.Errorf("feedback ls = %q", ls)
	}
	if detail := e.ok(agentDir, "feedback", "show", "1"); !strings.Contains(detail, "Needs more breathing room") || !strings.Contains(detail, fb.ImageURL) {
		t.Errorf("feedback show = %q", detail)
	}

	// Dismiss & reopen.
	e.ok(agentDir, "feedback", "dismiss", "1")
	if ls := e.ok(agentDir, "feedback", "ls", "--status", "dismissed"); !strings.Contains(ls, "[dismissed]") {
		t.Errorf("dismissed ls = %q", ls)
	}
	e.ok(agentDir, "feedback", "reopen", "1")

	// Promote to backlog -> becomes item #1 with screenshot in Markdown body.
	promoOut := e.ok(agentDir, "feedback", "promote", "1")
	if !strings.Contains(promoOut, "Moved feedback 1 to the backlog as #1.") || !strings.Contains(promoOut, fb.ImageURL) {
		t.Errorf("feedback promote = %q", promoOut)
	}

	// Delete & disable.
	e.ok(alexDir, "feedback", "rm", "1")
	e.ok(alexDir, "feedback", "disable")
	var wsAfter store.Workspace
	if err := json.Unmarshal([]byte(e.ok(alexDir, "workspace", "--json")), &wsAfter); err != nil || wsAfter.FeedbackEnabled {
		t.Errorf("after disable = %+v, %v", wsAfter, err)
	}
}
