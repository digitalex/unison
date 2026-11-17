package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

// fakeIsocan puts an `isocan` on PATH that records its arguments in a file
// and prints stdout (or fails, when stdout is empty), and returns that file.
func fakeIsocan(t *testing.T, stdout string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake isocan is a shell script")
	}
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + args + "'\n"
	if stdout == "" {
		script += "echo 'no daemon' >&2\nexit 3\n"
	} else {
		script += "cat <<'JSON'\n" + stdout + "\nJSON\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "isocan"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return args
}

// TestWorkspaceCreateCanvas: `workspace create --create-canvas` makes a
// canvas with the isocan CLI and attaches its isocan.io address.
func TestWorkspaceCreateCanvas(t *testing.T) {
	e := newTestEnv(t)
	dir := t.TempDir()
	e.login("Alex", "")

	args := fakeIsocan(t, `{"canvasId": "prj_Ab3-x_9"}`)
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(dir, "workspace", "create", "Orbit", "Two", "--create-canvas", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	if ws.Name != "Orbit Two" || ws.CanvasURL != "https://isocan.io/p/prj_Ab3-x_9" {
		t.Errorf("workspace = %+v", ws)
	}
	if got, _ := os.ReadFile(args); string(got) != "--json\ncanvas\nnew\nOrbit Two\n" {
		t.Errorf("isocan was run with %q", got)
	}

	// A URL, should a later isocan print one, is used as is.
	fakeIsocan(t, `{"canvasId": "prj_1", "url": "https://isocan.io/p/prj_2"}`)
	if out := e.ok(dir, "workspace", "create", "Orbit", "--create-canvas"); !strings.Contains(out, "Canvas     https://isocan.io/p/prj_2\n") {
		t.Errorf("create = %q", out)
	}

	before := len(e.ok(dir, "workspace", "list"))
	for name, tc := range map[string]struct{ stdout, want string }{
		"an address that isn't a canvas on isocan.io": {`{"url": "https://evil.example/p/prj_1"}`, "didn't print a canvas id"},
		"output that isn't JSON":                      {`created canvas prj_1`, "didn't print a canvas id"},
		"a failing isocan":                            {``, "`isocan canvas new` failed (exit status 3): no daemon"},
	} {
		fakeIsocan(t, tc.stdout)
		if _, errOut, code := e.run(dir, "workspace", "create", "Nope", "--create-canvas"); code != 1 || !strings.Contains(errOut, tc.want) {
			t.Errorf("%s: exit %d, stderr %q", name, code, errOut)
		}
	}
	t.Setenv("PATH", t.TempDir())
	if _, errOut, code := e.run(dir, "workspace", "create", "Nope", "--create-canvas"); code != 1 || !strings.Contains(errOut, "needs the isocan CLI") {
		t.Errorf("without isocan: exit %d, stderr %q", code, errOut)
	}
	if _, errOut, code := e.run(dir, "workspace", "create", "Nope", "--create-canvas", "--canvas", "https://isocan.io/p/prj_3"); code != 1 || !strings.Contains(errOut, "alternatives") {
		t.Errorf("--canvas with --create-canvas: exit %d, stderr %q", code, errOut)
	}
	if after := len(e.ok(dir, "workspace", "list")); after != before {
		t.Errorf("a failed --create-canvas still created a workspace")
	}
}
