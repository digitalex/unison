package cli

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

func TestSectionsAndChoicesCLI(t *testing.T) {
	e := newTestEnv(t)
	human, agent := t.TempDir(), t.TempDir()
	e.login("Alex", "alex@example.com")

	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(human, "workspace", "create", "Core", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	e.ok(human, "join", e.url+"/w/"+ws.ID, "--name", "Alex", "--emoji", "🧑", "--account")
	e.ok(agent, "join", e.url+"/w/"+ws.ID, "--pass", e.passFor(human), "--name", "Ada", "--emoji", "🦊")

	// Empty sections initially.
	if out := e.ok(human, "section"); !strings.Contains(out, "No custom sections configured") {
		t.Fatalf("initial section ls = %q", out)
	}

	// Create, reorder, and edit sections.
	e.ok(human, "section", "add", "Triage", "--icon", "inbox")
	e.ok(human, "section", "add", "Decisions", "--icon", "scale", "--notify")
	e.ok(human, "section", "move", "decisions", "1")
	editOut := e.ok(human, "section", "edit", "triage", "--name", "Bug Triage", "--icon", "bug")
	if !strings.Contains(editOut, "Bug Triage") || !strings.Contains(editOut, "icon: bug") {
		t.Fatalf("section edit = %q", editOut)
	}

	secList := e.ok(human, "section", "ls")
	if strings.Index(secList, "Decisions") > strings.Index(secList, "Bug Triage") {
		t.Fatalf("expected Decisions before Bug Triage after move:\n%s", secList)
	}

	// Park an item in triage; main `unison backlog` stays empty and `backlog claim` exits 3!
	trOut := e.ok(agent, "backlog", "add", "Flaky websocket test", "--section", "triage")
	trID := regexp.MustCompile(`#(\d+)`).FindStringSubmatch(trOut)[1]
	if out := e.ok(agent, "backlog"); !strings.Contains(out, "The backlog is empty.") {
		t.Fatalf("main backlog should ignore triage item, got: %q", out)
	}
	if _, _, code := e.run(agent, "backlog", "claim"); code != 3 {
		t.Fatalf("backlog claim with only custom section item: exit %d, want 3", code)
	}
	if out := e.ok(agent, "backlog", "--section", "triage"); !strings.Contains(out, "Flaky websocket test") {
		t.Fatalf("backlog --section triage = %q", out)
	}

	// Prime human's `wait` cursor before agent adds a notify-section decision item.
	e.run(human, "wait", "--mentions", "--timeout", "0")

	// Agent creates two decision items in `decisions` (notify=true) assigned to Alex.
	dec1Out := e.ok(agent, "backlog", "add", "Keep keep-alive ping?",
		"--section", "decisions", "--to", "Alex",
		"--choice", "20s", "--choice", "30s")
	dec1ID := regexp.MustCompile(`#(\d+)`).FindStringSubmatch(dec1Out)[1]

	dec2Out := e.ok(agent, "backlog", "add", "Choose SQLite driver",
		"--section", "decisions", "--to", "Alex",
		"--choice", "modernc", "--choice", "mattn",
		"--close-on-answer")
	dec2ID := regexp.MustCompile(`#(\d+)`).FindStringSubmatch(dec2Out)[1]

	// `wait --mentions` wakes Alex for the assigned decision items.
	if waitOut := e.ok(human, "wait", "--mentions", "--timeout", "5"); !strings.Contains(waitOut, "Keep keep-alive ping?") {
		t.Errorf("human wait --mentions = %q", waitOut)
	}

	// Both notify-section items surface in `todo ls` and `todo ls --all-workspaces`!
	todoOut := e.ok(human, "todo", "ls", "--mine")
	if !strings.Contains(todoOut, "Keep keep-alive ping?") || !strings.Contains(todoOut, "Choose SQLite driver") || !strings.Contains(todoOut, "[section: Decisions]") {
		t.Fatalf("todo ls --mine =\n%s", todoOut)
	}
	allWsOut := e.ok(human, "todo", "ls", "--all-workspaces")
	if !strings.Contains(allWsOut, "Choose SQLite driver") {
		t.Fatalf("todo ls --all-workspaces =\n%s", allWsOut)
	}

	// Prime agent's `wait` cursor so we can verify `backlog answer` wakes the item creator!
	e.run(agent, "wait", "--mentions", "--timeout", "0")

	// Answer dec1 (close_on_answer=false): records choice while keeping item open.
	ans1Out := e.ok(human, "backlog", "answer", dec1ID, "20s")
	if !strings.Contains(ans1Out, "State     [open") || !strings.Contains(ans1Out, "Decision  20s") {
		t.Fatalf("backlog answer dec1 =\n%s", ans1Out)
	}

	// Answer dec2 (close_on_answer=true) using 1-based choice index: resolves and closes item!
	ans2Out := e.ok(human, "backlog", "answer", dec2ID, "1")
	if !strings.Contains(ans2Out, "State     [done") || !strings.Contains(ans2Out, "Decision: modernc") {
		t.Fatalf("backlog answer dec2 =\n%s", ans2Out)
	}

	// Agent's `wait --mentions` wakes on the human's decision!
	if agentWait := e.ok(agent, "wait", "--mentions", "--timeout", "5"); !strings.Contains(agentWait, "answered") {
		t.Errorf("agent wait --mentions on answer = %q", agentWait)
	}

	// Edit dec1 to clear choices and move back to main Backlog; delete triage section and verify trID moved to main Backlog.
	editItemOut := e.ok(human, "backlog", "edit", dec1ID, "--clear-choices", "--section", "none")
	if strings.Contains(editItemOut, "Choices") || strings.Contains(editItemOut, "Section") {
		t.Fatalf("expected cleared choices and section=none:\n%s", editItemOut)
	}
	e.ok(human, "section", "delete", "triage")
	mainOut := e.ok(human, "backlog")
	if !strings.Contains(mainOut, "#"+trID) || !strings.Contains(mainOut, "#"+dec1ID) {
		t.Fatalf("main backlog after section delete =\n%s", mainOut)
	}
}
