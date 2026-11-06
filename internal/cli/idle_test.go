package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digitalex/unison/internal/store"
)

// TestAgentLoggedOut: an agent idle past 24 hours gets the reason and exit
// code 4 from every command, `wait` included, so loops stop; its human is
// unaffected.
func TestAgentLoggedOut(t *testing.T) {
	e := newTestEnv(t)
	human, agent := t.TempDir(), t.TempDir()
	e.login("Alex", "")
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(human, "workspace", "create", "Unison", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	invite := e.url + "/w/" + ws.ID
	e.ok(human, "join", invite, "--name", "Alex", "--account")
	e.ok(agent, "join", invite, "--pass", e.passFor(human), "--name", "Ada")
	e.ok(agent, "chat", "send", "On it")

	later := time.Now().Add(store.AgentIdleTimeout + time.Minute)
	e.st.SetClock(func() time.Time { return later })
	for _, cmd := range [][]string{{"wait", "--timeout", "30"}, {"chat", "send", "still here?"}, {"backlog"}} {
		_, errOut, code := e.run(agent, cmd...)
		if code != exitLoggedOut || !strings.Contains(errOut, "logged out after 24 hours without any activity") || !strings.Contains(errOut, "exit code 4") {
			t.Errorf("%v: exit %d, stderr %q", cmd, code, errOut)
		}
	}
	if out := e.ok(human, "chat", "log"); !strings.Contains(out, "On it") {
		t.Errorf("human after 24h = %q", out)
	}
	if out := e.ok(human, "who"); strings.Contains(out, "Ada") {
		t.Errorf("who should hide logged-out agent Ada, got %q", out)
	}
	if out := e.ok(human, "who", "--all"); !strings.Contains(out, "⚪") || !strings.Contains(out, "Ada") || !strings.Contains(out, "logged out (inactive) 20") || strings.Contains(out, "credential expires") {
		t.Errorf("who --all = %q", out)
	}
	if out := e.ok(human, "agents"); !strings.Contains(out, "logged out (inactive) 20") {
		t.Errorf("agents after idle logout = %q", out)
	}
}

// TestWaitStopsWithoutValidSession verifies that `unison wait` exits with
// exitLoggedOut (4) instead of 1 when there is no configured session/workspace
// or when the token is rejected with 401/403, so agent wait loops stop instead
// of spinning.
func TestWaitStopsWithoutValidSession(t *testing.T) {
	e := newTestEnv(t)
	emptyDir := t.TempDir()

	// No token / not joined in emptyDir -> exit code 4.
	_, errOut, code := e.run(emptyDir, "wait", "--timeout", "1")
	if code != exitLoggedOut || !strings.Contains(errOut, "exit code 4") {
		t.Fatalf("wait without session: exit %d, stderr %q", code, errOut)
	}

	// Token present but no workspace configured -> exit code 4.
	_, errOut, code = e.run(emptyDir, "--token", "ut_bogus", "wait", "--timeout", "1")
	if code != exitLoggedOut || !strings.Contains(errOut, "exit code 4") {
		t.Fatalf("wait without workspace: exit %d, stderr %q", code, errOut)
	}

	// Invalid token (401 sign_in) with workspace configured -> exit code 4.
	_, errOut, code = e.run(emptyDir, "--token", "ut_bogus", "--workspace", "ws_1234", "wait", "--timeout", "1")
	if code != exitLoggedOut || !strings.Contains(errOut, "exit code 4") {
		t.Fatalf("wait with invalid token: exit %d, stderr %q", code, errOut)
	}
}

// TestFindConfigFallsBackToPWDWhenCWDInodeStale verifies that when the open
// "." directory inode has been removed and recreated at the same path (so
// os.Getwd() fails with ENOENT while $PWD still names the recreated path),
// findConfig() still locates .unison.json via $PWD.
func TestFindConfigFallsBackToPWDWhenCWDInodeStale(t *testing.T) {
	e := newTestEnv(t)
	human := t.TempDir()
	agentParent := t.TempDir()
	agentDir := filepath.Join(agentParent, "worktree")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}

	e.login("Alex", "")
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(human, "workspace", "create", "Unison", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	invite := e.url + "/w/" + ws.ID
	e.ok(human, "join", invite, "--name", "Alex", "--account")
	e.ok(agentDir, "join", invite, "--pass", e.passFor(human), "--name", "Ada")

	cfgBytes, err := os.ReadFile(filepath.Join(agentDir, ConfigFile))
	if err != nil {
		t.Fatal(err)
	}

	// Move into agentDir, then remove and recreate agentDir at the same path so
	// the process's open "." directory inode is unlinked (stale), while $PWD
	// still points to the recreated directory containing .unison.json.
	t.Chdir(agentDir)
	t.Setenv("PWD", agentDir)
	if err := os.RemoveAll(agentDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, ConfigFile), cfgBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := Run([]string{"--server", e.url, "whoami"}, &out, &errOut)
	if code != 0 || !strings.Contains(out.String(), "Ada") {
		t.Fatalf("whoami with stale CWD inode: exit %d, stdout=%q, stderr=%q", code, out.String(), errOut.String())
	}
}
