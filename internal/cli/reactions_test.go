package cli

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

// TestChatReact: `chat react` toggles a reaction (emoji or name) and
// `chat log` shows the counts.
func TestChatReact(t *testing.T) {
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
	id := regexp.MustCompile(`#(\d+)`).FindStringSubmatch(e.ok(human, "chat", "send", "Shipped the landing page"))[1]

	if out := e.ok(agent, "chat", "react", id, "👍"); !strings.Contains(out, "Reacted 👍 to message #"+id+".  · 👍 1") {
		t.Errorf("react = %q", out)
	}
	e.ok(human, "chat", "react", id, "+1")
	e.ok(human, "chat", "react", id, "heart")
	if out := e.ok(human, "chat", "log"); !strings.Contains(out, "Shipped the landing page  · 👍 2 ❤️ 1") {
		t.Errorf("chat log =\n%s", out)
	}
	if out := e.ok(agent, "chat", "react", id, "thumbsup"); !strings.Contains(out, "Took back 👍 on message #"+id+".  · 👍 1 ❤️ 1") {
		t.Errorf("toggle off = %q", out)
	}
	// Taking back the last reactions leaves no summary (not a stale one).
	e.ok(human, "chat", "react", id, "heart")
	if out := e.ok(human, "chat", "react", id, "+1"); out != "Took back 👍 on message #"+id+".\n" {
		t.Errorf("last toggle off = %q", out)
	}
	if _, errOut, code := e.run(agent, "chat", "react", id, "🎉"); code == 0 || !strings.Contains(errOut, "reactions are") {
		t.Errorf("🎉: exit %d %q", code, errOut)
	}
	if _, errOut, code := e.run(agent, "chat", "react", "999", "👍"); code == 0 || !strings.Contains(errOut, "not found") {
		t.Errorf("missing message: exit %d %q", code, errOut)
	}
}
