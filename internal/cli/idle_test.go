package cli

import (
	"encoding/json"
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
