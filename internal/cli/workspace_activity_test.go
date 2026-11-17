package cli

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

// TestWorkspaceListActivity: `unison workspace list` marks a workspace where
// somebody else has been busy with [activity #<seq>], and --json carries
// activity_seq (#300).
func TestWorkspaceListActivity(t *testing.T) {
	e := newTestEnv(t)
	human, agent := t.TempDir(), t.TempDir()
	e.login("Alex", "")
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(human, "workspace", "create", "Unison", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	url := e.url + "/w/" + ws.ID
	e.ok(human, "join", url, "--name", "Alex", "--emoji", "🦊", "--account")
	e.ok(human, "chat", "send", "anyone here?")

	// Only Alex has done anything: no marker.
	if out := e.ok(human, "workspace", "list"); !strings.Contains(out, ws.ID+"  Unison") || strings.Contains(out, "[activity") {
		t.Errorf("list with only his own activity = %q", out)
	}

	// His agent joins and answers: that is activity to catch up on.
	e.ok(agent, "join", url, "--pass", e.passFor(human), "--name", "Quill", "--emoji", "🪶")
	e.ok(agent, "chat", "send", "here")
	marker := regexp.MustCompile(`(?m)^` + ws.ID + `  Unison  .*\[activity #(\d+)\]$`)
	out := e.ok(human, "workspace", "list")
	first := marker.FindStringSubmatch(out)
	if first == nil {
		t.Fatalf("list after the agent's message = %q, want an [activity #n] marker", out)
	}
	var list []store.Workspace
	if err := json.Unmarshal([]byte(e.ok(human, "workspace", "list", "--json")), &list); err != nil || len(list) != 1 || list[0].ActivitySeq == 0 {
		t.Fatalf("list --json = %+v, %v", list, err)
	}

	// More of Alex's own doing leaves the number where it was; the agent's
	// next message moves it on.
	e.ok(human, "chat", "send", "thanks")
	if again := marker.FindStringSubmatch(e.ok(human, "workspace", "list")); again == nil || again[1] != first[1] {
		t.Errorf("after his own message the marker went from %v to %v", first, again)
	}
	e.ok(agent, "chat", "send", "anything else?")
	if later := marker.FindStringSubmatch(e.ok(human, "workspace", "list")); later == nil || later[1] == first[1] {
		t.Errorf("after the agent's second message the marker stayed at %v (%v)", first, later)
	}
}
