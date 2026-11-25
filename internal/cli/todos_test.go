package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/digitalex/unison/internal/store"
)

func TestTodosCLI(t *testing.T) {
	e := newTestEnv(t)
	now := time.Date(2026, 11, 25, 18, 0, 0, 0, time.UTC)
	e.st.SetClock(func() time.Time { return now })

	human1, human2, agent := t.TempDir(), t.TempDir(), t.TempDir()
	e.login("Alex", "alex@example.com")

	var ws1, ws2 store.Workspace
	if err := json.Unmarshal([]byte(e.ok(human1, "workspace", "create", "Frontend", "--repo", "https://github.com/acme/web", "--json")), &ws1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(e.ok(human2, "workspace", "create", "Backend", "--repo", "https://github.com/acme/api", "--json")), &ws2); err != nil {
		t.Fatal(err)
	}

	e.ok(human1, "join", e.url+"/w/"+ws1.ID, "--name", "Alex", "--emoji", "🧑", "--account")
	e.ok(human2, "join", e.url+"/w/"+ws2.ID, "--name", "Alex", "--emoji", "🧑", "--account")
	e.ok(agent, "join", e.url+"/w/"+ws1.ID, "--pass", e.passFor(human1), "--name", "Ada", "--emoji", "🦊")

	// Empty list initially.
	if out := e.ok(human1, "todo"); !strings.Contains(out, "No TODOs.") {
		t.Errorf("initial todo ls = %q", out)
	}

	// Reject assigning a TODO to an agent or missing --to.
	if _, errOut, code := e.run(agent, "todo", "add", "Agent task", "--to", "Ada"); code == 0 || !strings.Contains(errOut, "TODOs are for humans") {
		t.Errorf("assigning TODO to agent: exit %d, stderr %q", code, errOut)
	}
	if _, errOut, code := e.run(agent, "todo", "add", "Missing assignee"); code == 0 || !strings.Contains(errOut, "--to") {
		t.Errorf("missing --to: exit %d, stderr %q", code, errOut)
	}

	// Add backlog item, then agent adds a TODO linked to the item and PR.
	e.ok(agent, "backlog", "add", "Upgrade auth")
	out := e.ok(agent, "todo", "add", "Approve OAuth app", "--to", "Alex", "--body", "Enable callback URL", "--item", "1", "--pr", "https://github.com/acme/web/pull/19")
	if !strings.Contains(out, "Approve OAuth app") || !strings.Contains(out, "Item      #1 Upgrade auth") || !strings.Contains(out, "https://github.com/acme/web/pull/19") {
		t.Fatalf("todo add = %q", out)
	}
	id1 := regexp.MustCompile(`TODO #(\d+)`).FindStringSubmatch(out)[1]

	// `unison wait --mentions` wakes Alex when a TODO is created for them.
	if waitOut := e.ok(human1, "wait", "--mentions", "--timeout", "5"); !strings.Contains(waitOut, `TODO #1 "Approve OAuth app"`) {
		t.Errorf("wait --mentions = %q, want TODO #1", waitOut)
	}

	// Add a second TODO in Backend workspace.
	out2 := e.ok(human2, "todo", "add", "Rotate DB password", "--to", "Alex")
	id2 := regexp.MustCompile(`TODO #(\d+)`).FindStringSubmatch(out2)[1]

	// Cross-workspace `unison todo ls --all-workspaces` shows both!
	allOut := e.ok(human1, "todo", "ls", "--all-workspaces")
	if !strings.Contains(allOut, "Frontend") || !strings.Contains(allOut, "Approve OAuth app") || !strings.Contains(allOut, "Backend") || !strings.Contains(allOut, "Rotate DB password") {
		t.Errorf("todo ls --all-workspaces =\n%s", allOut)
	}

	// Snooze 1d hides from active & --all-workspaces active, then reappears after 24h!
	e.ok(human2, "todo", "snooze", id2, "1d")
	if out := e.ok(human2, "todo", "ls"); !strings.Contains(out, "No TODOs.") {
		t.Errorf("active list while snoozed = %q", out)
	}
	if out := e.ok(human2, "todo", "ls", "--status", "snoozed"); !strings.Contains(out, "Rotate DB password") {
		t.Errorf("snoozed list = %q", out)
	}
	if out := e.ok(human1, "todo", "ls", "--all-workspaces"); strings.Contains(out, "Rotate DB password") {
		t.Errorf("--all-workspaces active included snoozed item:\n%s", out)
	}
	now = now.Add(12 * time.Hour)
	e.ok(agent, "whoami") // keep agent credential active across 24h idle window
	now = now.Add(13 * time.Hour)
	if out := e.ok(human2, "todo", "ls"); !strings.Contains(out, "Rotate DB password") {
		t.Errorf("active list after snooze elapsed = %q", out)
	}
	e.ok(human2, "todo", "snooze", id2, "1w")
	e.ok(human2, "todo", "snooze", id2, "off")
	if out := e.ok(human2, "todo", "ls", "--mine"); !strings.Contains(out, "Rotate DB password") {
		t.Errorf("after unsnooze = %q", out)
	}

	// Agent `todo done` fails without --note and succeeds with --note; human `todo done` works without --note.
	if _, errOut, code := e.run(agent, "todo", "done", id1); code == 0 || !strings.Contains(errOut, "--note") {
		t.Errorf("agent todo done without --note: exit %d, stderr %q", code, errOut)
	}
	if out := e.ok(agent, "todo", "done", id1, "--note", "Confirmed by Ada"); !strings.Contains(out, "Confirmed by Ada") {
		t.Errorf("agent todo done with --note = %q", out)
	}
	e.ok(human1, "todo", "reopen", id1)
	if out := e.ok(human2, "todo", "done", id2); !strings.Contains(out, "[done") {
		t.Errorf("human todo done without --note = %q", out)
	}

	// GitHub pull_request closed webhook auto-closes PR-linked TODO #1 in Frontend!
	hookOut := e.ok(human1, "webhook", "add", "GitHub", "--instructions", "Handle PR merges")
	hookURL := regexp.MustCompile(`https?://\S+/hooks/wh_\S+`).FindString(hookOut)
	prClosed := `{"action":"closed","number":19,"pull_request":{"number":19,"title":"OAuth","merged":true,"html_url":"https://github.com/acme/web/pull/19"}}`
	req, _ := http.NewRequest("POST", hookURL, bytes.NewBufferString(prClosed))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "pull_request")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusAccepted {
		t.Fatalf("webhook POST = %v, %v", resp, err)
	}
	resp.Body.Close()

	if doneOut := e.ok(human1, "todo", "ls", "--status", "done"); !strings.Contains(doneOut, "Auto-closed: PR merged") {
		t.Errorf("done list after PR merge webhook = %q", doneOut)
	}

	// Delete TODO.
	if delOut := e.ok(human1, "todo", "delete", id1); !strings.Contains(delOut, "Deleted TODO #"+id1) {
		t.Errorf("todo delete = %q", delOut)
	}
}
