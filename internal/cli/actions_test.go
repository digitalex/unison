package cli

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

// TestActionsCLI: an agent asks its member to act with `chat send
// --action`; the member lists it with `actions` and marks it `done` (and
// undoes that); the chat log shows who it asks and who is done.
func TestActionsCLI(t *testing.T) {
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

	if out := e.ok(human, "actions"); !strings.Contains(out, "Nothing needs you") {
		t.Errorf("no actions = %q", out)
	}
	out := e.ok(agent, "chat", "send", "@Alex PR #7 is ready for your review", "--action")
	if !strings.Contains(out, "[needs @Alex]") {
		t.Errorf("send = %q", out)
	}
	id := regexp.MustCompile(`message #(\d+)`).FindStringSubmatch(out)[1]
	if _, errOut, code := e.run(agent, "chat", "send", "anyone?", "--action"); code == 0 || !strings.Contains(errOut, "@mention") {
		t.Errorf("--action without a mention: exit %d %q", code, errOut)
	}
	if out := e.ok(human, "actions"); !strings.Contains(out, "🦊 Ada [needs @Alex]: @Alex PR #7 is ready for your review") {
		t.Errorf("actions = %q", out)
	}
	if _, errOut, code := e.run(agent, "done", id); code == 0 || !strings.Contains(errOut, "doesn't ask you") {
		t.Errorf("the asker marking it done: exit %d %q", code, errOut)
	}
	if out := e.ok(human, "done", id); !strings.Contains(out, "Marked message #"+id+" done") {
		t.Errorf("done = %q", out)
	}
	if out := e.ok(human, "actions"); !strings.Contains(out, "Nothing needs you") {
		t.Errorf("after done = %q", out)
	}
	if out := e.ok(human, "chat", "log"); !strings.Contains(out, "[needs @Alex ✓]") {
		t.Errorf("chat log after done = %q", out)
	}
	e.ok(human, "done", id, "--undo")
	if out := e.ok(human, "actions"); !strings.Contains(out, "PR #7") {
		t.Errorf("after undo = %q", out)
	}
	// Replying in the thread marks it done too.
	e.ok(human, "chat", "send", "Looking now", "--reply-to", id)
	if out := e.ok(human, "actions"); !strings.Contains(out, "Nothing needs you") {
		t.Errorf("after replying = %q", out)
	}
	if _, errOut, code := e.run(human, "inbox"); code == 0 || !strings.Contains(errOut, "unknown") {
		t.Errorf("inbox is gone: exit %d %q", code, errOut)
	}
}
