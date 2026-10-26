package cli

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

// TestInboxCLI: an agent asks its member to act (default recipient); the
// member lists and dismisses; agents have no inbox of their own.
func TestInboxCLI(t *testing.T) {
	e := newTestEnv(t)
	human, agent := t.TempDir(), t.TempDir()
	e.login("Alex Example", "")
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(human, "workspace", "create", "Unison", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	invite := e.url + "/w/" + ws.ID
	e.ok(human, "join", invite, "--name", "Alex", "--account")
	e.ok(agent, "join", invite, "--pass", e.passFor(human), "--name", "Ada", "--emoji", "🦊")
	e.ok(human, "backlog", "add", "Landing page")

	if out := e.ok(human, "inbox"); !strings.Contains(out, "Nothing needs your attention") {
		t.Errorf("empty inbox = %q", out)
	}
	out := e.ok(agent, "inbox", "send", "PR is ready for your review", "--item", "1", "--url", "https://github.com/o/r/pull/7")
	if !strings.Contains(out, "Sent to Alex Example's inbox") {
		t.Errorf("send = %q", out)
	}
	id := regexp.MustCompile(`entry (\d+)`).FindStringSubmatch(out)[1]
	if out := e.ok(human, "inbox"); !strings.Contains(out, "🦊 Ada [item #1]: PR is ready for your review  https://github.com/o/r/pull/7") {
		t.Errorf("inbox = %q", out)
	}
	if _, errOut, code := e.run(agent, "inbox", "send", "x", "--to", "Ada"); code == 0 || !strings.Contains(errOut, "is an agent") {
		t.Errorf("to an agent: exit %d %q", code, errOut)
	}
	if _, errOut, code := e.run(agent, "inbox"); code == 0 || !strings.Contains(errOut, "only members have an inbox") {
		t.Errorf("agent reading: exit %d %q", code, errOut)
	}
	if out := e.ok(human, "inbox", "dismiss", id); !strings.Contains(out, "Dismissed entry "+id) {
		t.Errorf("dismiss = %q", out)
	}
	if out := e.ok(human, "inbox"); !strings.Contains(out, "Nothing needs your attention") {
		t.Errorf("after dismiss = %q", out)
	}
	if out := e.ok(human, "inbox", "--all"); !strings.Contains(out, "(dismissed)") {
		t.Errorf("--all = %q", out)
	}
	e.ok(human, "inbox", "restore", id)
	if out := e.ok(human, "inbox"); !strings.Contains(out, "PR is ready") {
		t.Errorf("after restore = %q", out)
	}
}
