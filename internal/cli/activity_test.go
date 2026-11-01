package cli

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

func TestActivityCLI(t *testing.T) {
	e := newTestEnv(t)
	alexDir, bobDir, carolDir, agentDir := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()

	e.login("Alex", "")
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(alexDir, "workspace", "create", "Unison", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	if out := e.ok(alexDir, "--workspace", ws.ID, "activity"); !strings.Contains(out, "No activity yet.") {
		t.Fatalf("empty activity = %q, want No activity yet.", out)
	}

	invite := e.url + "/w/" + ws.ID
	e.ok(alexDir, "join", invite, "--name", "Alex", "--emoji", "🧑", "--account")
	link := regexp.MustCompile(`(?m)^(http\S+/join#invite=ui_\S+)$`).FindStringSubmatch(e.ok(alexDir, "invite", "link"))
	if link == nil {
		t.Fatal("invite link printed no URL")
	}

	bob := e.login("Bob", "")
	e.ok(bobDir, "join", link[1], "--name", "Bob", "--emoji", "🐻")

	carol := e.login("Carol", "")
	e.ok(carolDir, "join", link[1], "--name", "Carol", "--emoji", "🦉")

	e.login("Alex", "")
	e.ok(agentDir, "join", invite, "--pass", e.passFor(alexDir), "--name", "Ada", "--emoji", "🦊")

	// Exercise backlog lifecycle + recurring task + member leaving/removal.
	e.ok(alexDir, "backlog", "add", "Ship CLI feed")
	e.ok(alexDir, "backlog", "assign", "1", "--to", "Ada")
	e.ok(agentDir, "backlog", "start", "1")
	e.ok(agentDir, "backlog", "close", "1", "--note", "done")
	e.ok(alexDir, "backlog", "reopen", "1")
	e.ok(alexDir, "backlog", "assign", "1", "--to", "Alex")
	e.ok(alexDir, "backlog", "add", "Docs polish", "--to", "Ada")

	e.ok(alexDir, "schedule", "add", "Daily log check", "--daily")
	e.ok(alexDir, "schedule", "run", "1")

	// Bob leaves (removes himself); Alex removes Carol.
	e.login("Bob", "")
	e.ok(bobDir, "members", "remove", strconv.FormatInt(bob.ID, 10))
	e.login("Alex", "")
	e.ok(alexDir, "members", "remove", strconv.FormatInt(carol.ID, 10))

	out := e.ok(agentDir, "activity")
	for _, want := range []string{
		"👋 🧑 Alex joined",
		"👋 🐻 Bob joined",
		"👋 🦉 Carol joined",
		"👋 🦊 Ada joined · agent of Alex",
		`📋 🧑 Alex added #1 "Ship CLI feed"`,
		`📋 🧑 Alex assigned #1 "Ship CLI feed" → 🦊 Ada`,
		`📋 🦊 Ada started #1 "Ship CLI feed"`,
		`📋 🦊 Ada closed #1 "Ship CLI feed"`,
		`📋 🧑 Alex reopened #1 "Ship CLI feed"`,
		`📋 🧑 Alex assigned #1 "Ship CLI feed" to themselves`,
		`📋 🧑 Alex added #2 "Docs polish" → 🦊 Ada`,
		`📋 🔁 Recurring task added #3 "Daily log check"`,
		"👋 Bob left the workspace",
		"👋 🧑 Alex removed Carol",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("activity missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "unison backlog claim") {
		t.Errorf("activity should not print wait's claim prompt:\n%s", out)
	}

	// --limit N returns only the latest N events, oldest first.
	limited := strings.Split(strings.TrimSpace(e.ok(agentDir, "activity", "--limit", "2")), "\n")
	if len(limited) != 2 || !strings.Contains(limited[0], "Bob left the workspace") || !strings.Contains(limited[1], "Alex removed Carol") {
		t.Errorf("activity --limit 2 = %q", limited)
	}

	// --json returns raw events.
	var evs []store.Event
	if err := json.Unmarshal([]byte(e.ok(agentDir, "activity", "--limit", "3", "--json")), &evs); err != nil || len(evs) != 3 {
		t.Errorf("activity --json = %d events, err %v", len(evs), err)
	}

	// Usage validation and help docs.
	for _, bad := range [][]string{{"activity", "extra"}, {"activity", "--limit", "0"}, {"activity", "--limit", "-5"}} {
		if _, _, code := e.run(agentDir, bad...); code == 0 {
			t.Errorf("unison %v succeeded, want non-zero exit", bad)
		}
	}
	if help := e.ok(agentDir, "help"); !strings.Contains(help, "activity [--limit N]") {
		t.Errorf("help lacks activity command:\n%s", help)
	}
	if guide := e.ok(agentDir, "--agent-help"); !strings.Contains(guide, "`unison activity [--limit N]`") {
		t.Errorf("agent-help lacks activity command:\n%s", guide)
	}
}
