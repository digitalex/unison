package cli

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

// TestChatAboutItem: `chat send --item` ties a message to a backlog item;
// `chat log` marks it, `backlog show` lists it, and `wait` says what it is
// about.
func TestChatAboutItem(t *testing.T) {
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
	e.ok(human, "backlog", "add", "Landing page")

	out := e.ok(agent, "chat", "send", "Hero done,\nstarting the footer", "--item", "1")
	if !strings.Contains(out, `about #1 "Landing page"`) {
		t.Errorf("chat send --item = %q", out)
	}
	msgID := regexp.MustCompile(`#(\d+)`).FindStringSubmatch(out)[1]
	e.ok(human, "chat", "send", "nice", "--reply-to", msgID)
	if _, errOut, code := e.run(agent, "chat", "send", "x", "--item", "99"); code == 0 || !strings.Contains(errOut, "no backlog item #99") {
		t.Errorf("unknown item: exit %d %q", code, errOut)
	}

	log := e.ok(human, "chat", "log")
	if !strings.Contains(log, "Ada [item #1]: Hero done,") || strings.Contains(log, "Alex [item #1]") {
		t.Errorf("chat log =\n%s", log)
	}
	show := e.ok(human, "backlog", "show", "1")
	if !strings.Contains(show, "Chat about #1:") || !strings.Contains(show, "Ada: Hero done, …") || strings.Contains(show, "nice") {
		t.Errorf("backlog show =\n%s", show)
	}
	var detail struct {
		Item     store.Item      `json:"item"`
		Messages []store.Message `json:"messages"`
	}
	if err := json.Unmarshal([]byte(e.ok(human, "backlog", "show", "1", "--json")), &detail); err != nil || detail.Item.MessageCount != 1 || len(detail.Messages) != 1 {
		t.Errorf("backlog show --json = %+v %v", detail, err)
	}
	// Waiting, the human learns what a message is about.
	out = e.ok(human, "wait", "--timeout", "10")
	if !strings.Contains(out, `Ada (about item #1 "Landing page"): Hero done,`) {
		t.Errorf("wait = %q", out)
	}
}
