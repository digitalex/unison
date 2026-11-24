package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

func TestCLIWorkspaceInstructions(t *testing.T) {
	e := newTestEnv(t)
	human, agent := t.TempDir(), t.TempDir()

	e.login("Alex", "")
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(human, "workspace", "create", "Unison", "--instructions", "## Intro\nHello", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	invite := e.url + "/w/" + ws.ID
	e.ok(human, "join", invite, "--name", "Alex", "--emoji", "🧑", "--account")
	e.ok(agent, "join", invite, "--pass", e.passFor(human), "--name", "Ada", "--emoji", "🦊")

	// Read full instructions and headings.
	if out := e.ok(agent, "workspace", "instructions"); !strings.Contains(out, "Hello") {
		t.Fatalf("instructions output = %q", out)
	}
	if out := e.ok(agent, "workspace", "instructions", "--headings"); strings.TrimSpace(out) != "Intro" {
		t.Fatalf("--headings = %q", out)
	}

	// Agent cannot modify instructions.
	if _, _, code := e.run(agent, "workspace", "instructions", "set", "--text", "hacked"); code == 0 {
		t.Fatal("agent should not be able to set instructions")
	}

	// Human updates via stdin (--file -).
	prevStdin := instructionsStdin
	instructionsStdin = strings.NewReader("## Build\nRun go test.\n## Style\nKeep it simple.\n")
	t.Cleanup(func() { instructionsStdin = prevStdin })
	e.ok(human, "workspace", "instructions", "set", "--file", "-")
	if out := e.ok(agent, "workspace", "instructions", "--section", "style"); !strings.Contains(out, "Keep it simple.") || strings.Contains(out, "Run go test.") {
		t.Fatalf("--section style = %q", out)
	}

	// Human syncs from a repo-relative file.
	if err := os.MkdirAll(filepath.Join(human, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(human, ".unison"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(human, ".unison", "instructions.md"), []byte("## Synced\nFrom git repo.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.ok(human, "workspace", "instructions", "sync", "--path", ".unison/instructions.md")
	if out := e.ok(agent, "workspace", "instructions"); !strings.Contains(out, "From git repo.") {
		t.Fatalf("after sync = %q", out)
	}

	// Human clears instructions.
	e.ok(human, "workspace", "instructions", "set", "--clear")
	if out := e.ok(agent, "workspace", "instructions"); !strings.Contains(out, "no workspace instructions") {
		t.Fatalf("after clear = %q", out)
	}
}
