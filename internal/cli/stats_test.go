package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

// `unison workspace stats` needs no login and no workspace, and prints
// counts only (#301).
func TestWorkspaceStats(t *testing.T) {
	e := newTestEnv(t)
	dir := t.TempDir()

	// Signed out, on an empty server.
	if out := e.ok(dir, "workspace", "stats"); !strings.Contains(out, "Workspaces           0\n") {
		t.Errorf("stats on an empty server = %q", out)
	}

	e.login("Alex", "")
	e.ok(dir, "workspace", "create", "Acme Antigravity")
	e.ok(dir, "workspace", "create", "Secret Project")
	e.ok(dir, "logout")

	out := e.ok(dir, "workspace", "stats")
	for _, want := range []string{
		"Workspaces           2\n",
		"Created last 7 days  2\n",
		"Created today        2 (the past 24 hours)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stats = %q, want it to contain %q", out, want)
		}
	}
	if strings.Contains(out, "Acme") || strings.Contains(out, "Secret") {
		t.Errorf("stats names a workspace: %q", out)
	}

	var st store.WorkspaceStats
	if err := json.Unmarshal([]byte(e.ok(dir, "workspace", "stats", "--json")), &st); err != nil {
		t.Fatal(err)
	}
	if want := (store.WorkspaceStats{Total: 2, CreatedLast7Days: 2, CreatedToday: 2}); st != want {
		t.Errorf("stats --json = %+v, want %+v", st, want)
	}

	if _, errOut, code := e.run(dir, "workspace", "stats", "extra"); code == 0 || !strings.Contains(errOut, "takes no arguments") {
		t.Errorf("stats with an argument: exit %d %q", code, errOut)
	}
}
