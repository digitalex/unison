package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

func TestCLIProjectDefaults(t *testing.T) {
	e := newTestEnv(t)
	dir := t.TempDir()
	defaultsDir := t.TempDir()
	t.Setenv(defaultsDirEnv, defaultsDir)

	if err := os.WriteFile(filepath.Join(defaultsDir, "instructions.md"), []byte("## Default rules\nAlways write tests.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(defaultsDir, "settings.json"), []byte(`{"instructions_path":".unison/instructions.md","canvas_url":"https://isocan.io/p/default"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(defaultsDir, "routines.json"), []byte(`[{"title":"Nightly check","cadence":"daily","body":"Check tests"}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	// `unison defaults path` and `unison defaults show`
	if out := strings.TrimSpace(e.ok(dir, "defaults", "path")); out != defaultsDir {
		t.Fatalf("defaults path = %q, want %q", out, defaultsDir)
	}
	if out := e.ok(dir, "defaults", "show"); !strings.Contains(out, "instructions.md") || !strings.Contains(out, "Nightly check") {
		t.Fatalf("defaults show = %q", out)
	}

	e.login("Alex", "")

	// Creating a workspace applies all local defaults.
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(dir, "workspace", "create", "WithDefaults", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	if !ws.HasInstructions || ws.InstructionsPath != ".unison/instructions.md" || ws.CanvasURL != "https://isocan.io/p/default" {
		t.Fatalf("defaults not applied to workspace: %+v", ws)
	}
	schedules, err := e.st.ListSchedules(t.Context(), ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(schedules) != 1 || schedules[0].Title != "Nightly check" {
		t.Fatalf("default routines not created: %+v", schedules)
	}

	// --no-defaults skips all local defaults.
	var wsNoDef store.Workspace
	if err := json.Unmarshal([]byte(e.ok(dir, "workspace", "create", "NoDefaults", "--no-defaults", "--json")), &wsNoDef); err != nil {
		t.Fatal(err)
	}
	if wsNoDef.HasInstructions || wsNoDef.InstructionsPath != "" || wsNoDef.CanvasURL != "" {
		t.Fatalf("--no-defaults still applied defaults: %+v", wsNoDef)
	}
	schedules, err = e.st.ListSchedules(t.Context(), wsNoDef.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(schedules) != 0 {
		t.Fatalf("--no-defaults created routines: %+v", schedules)
	}
}
