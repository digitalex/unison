package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/digitalex/unison/internal/store"
)

func TestPerWorkspaceItemNumberingCLI(t *testing.T) {
	e := newTestEnv(t)
	now := time.Date(2026, 11, 25, 19, 0, 0, 0, time.UTC)
	e.st.SetClock(func() time.Time { return now })

	wsADir, wsBHuman, wsBAgent := t.TempDir(), t.TempDir(), t.TempDir()
	e.login("Alex", "alex@example.com")

	var wsA, wsB store.Workspace
	if err := json.Unmarshal([]byte(e.ok(wsADir, "workspace", "create", "Alpha", "--json")), &wsA); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(e.ok(wsBHuman, "workspace", "create", "Beta", "--json")), &wsB); err != nil {
		t.Fatal(err)
	}

	e.ok(wsADir, "join", e.url+"/w/"+wsA.ID, "--name", "Alex", "--emoji", "🧑", "--account")
	e.ok(wsBHuman, "join", e.url+"/w/"+wsB.ID, "--name", "Alex", "--emoji", "🧑", "--account")
	e.ok(wsBAgent, "join", e.url+"/w/"+wsB.ID, "--pass", e.passFor(wsBHuman), "--name", "Wren", "--emoji", "🪶")

	// Seed 4 items in Alpha so Beta's global IDs start at 5 while Beta's numbers start at #1.
	for i := 1; i <= 4; i++ {
		e.ok(wsADir, "backlog", "add", "Alpha item")
	}

	// Beta starts at #1!
	var b1 store.Item
	if err := json.Unmarshal([]byte(e.ok(wsBHuman, "backlog", "add", "Beta first", "--json")), &b1); err != nil {
		t.Fatal(err)
	}
	if b1.Number != 1 || b1.ID < 5 {
		t.Fatalf("Beta first: ID=%d Number=%d, want ID>=5 Number=1", b1.ID, b1.Number)
	}
	b1LegacyID := strconv.FormatInt(b1.ID, 10)

	outB2 := e.ok(wsBHuman, "backlog", "add", "Beta second")
	if !strings.Contains(outB2, "#2  Beta second") {
		t.Fatalf("backlog add #2 output = %q", outB2)
	}

	// Deleting #2 prints #2 and next item gets #3 (never reuses #2).
	if delOut := e.ok(wsBHuman, "backlog", "delete", "#2"); !strings.Contains(delOut, "Deleted #2.") {
		t.Fatalf("backlog delete #2 = %q", delOut)
	}
	var b3 store.Item
	if err := json.Unmarshal([]byte(e.ok(wsBHuman, "backlog", "add", "Beta third", "--json")), &b3); err != nil {
		t.Fatal(err)
	}
	if b3.Number != 3 {
		t.Fatalf("after deleting #2, next item Number = %d, want 3", b3.Number)
	}
	b3LegacyID := strconv.FormatInt(b3.ID, 10)

	// `unison backlog` displays #1 and #3 (not global IDs 5 and 7).
	listOut := e.ok(wsBHuman, "backlog")
	if !strings.Contains(listOut, "#1 ") || !strings.Contains(listOut, "#3 ") || strings.Contains(listOut, "#"+b1LegacyID+" ") {
		t.Fatalf("backlog list =\n%s", listOut)
	}

	// Lookup, edit, start, comment, close, reopen work with BOTH #number and legacy global ID!
	if showNum := e.ok(wsBAgent, "backlog", "show", "#1"); !strings.Contains(showNum, "#1  Beta first") {
		t.Fatalf("backlog show #1 = %q", showNum)
	}
	if showLegacy := e.ok(wsBAgent, "backlog", "show", b1LegacyID); !strings.Contains(showLegacy, "#1  Beta first") {
		t.Fatalf("backlog show legacy %s = %q", b1LegacyID, showLegacy)
	}
	if startOut := e.ok(wsBAgent, "backlog", "start", b1LegacyID); !strings.Contains(startOut, "#1  Beta first") {
		t.Fatalf("backlog start legacy = %q", startOut)
	}
	if cmOut := e.ok(wsBAgent, "backlog", "comment", "1", "Investigating @Alex"); !strings.Contains(cmOut, "Commented on #1") {
		t.Fatalf("backlog comment 1 = %q", cmOut)
	}
	if closeOut := e.ok(wsBAgent, "backlog", "close", b3LegacyID, "--note", "Shipped"); !strings.Contains(closeOut, "#3  Beta third") {
		t.Fatalf("backlog close legacy = %q", closeOut)
	}
	if reopenOut := e.ok(wsBAgent, "backlog", "reopen", "3"); !strings.Contains(reopenOut, "#3  Beta third") {
		t.Fatalf("backlog reopen 3 = %q", reopenOut)
	}

	// Scheduled task run prints #4.
	var sc store.Schedule
	if err := json.Unmarshal([]byte(e.ok(wsBHuman, "schedule", "add", "Daily sweep", "--daily", "--json")), &sc); err != nil {
		t.Fatal(err)
	}
	if runOut := e.ok(wsBHuman, "schedule", "run", strconv.FormatInt(sc.ID, 10)); !strings.Contains(runOut, `Added #4 "Daily sweep"`) {
		t.Fatalf("schedule run = %q, want Added #4 \"Daily sweep\"", runOut)
	}

	// Idea promotion prints #5 and idea list/show displays [moved to backlog #5].
	var idea store.Idea
	if err := json.Unmarshal([]byte(e.ok(wsBHuman, "idea", "add", "Offline mode", "--json")), &idea); err != nil {
		t.Fatal(err)
	}
	if promOut := e.ok(wsBHuman, "idea", "promote", strconv.FormatInt(idea.ID, 10)); !strings.Contains(promOut, "as #5.") {
		t.Fatalf("idea promote = %q, want #5", promOut)
	}
	if ideaLs := e.ok(wsBHuman, "idea", "ls", "--status", "promoted"); !strings.Contains(ideaLs, "[moved to backlog #5]") {
		t.Fatalf("idea ls --status promoted = %q", ideaLs)
	}

	// Chat --item accepts #number and legacy global ID, and chat log / activity / wait print #number!
	e.ok(wsBAgent, "chat", "send", "Progress on first item", "--item", "#1")
	e.ok(wsBAgent, "chat", "send", "Progress on third item", "--item", b3LegacyID)
	chatLog := e.ok(wsBHuman, "chat", "log")
	if !strings.Contains(chatLog, "[item #1]") || !strings.Contains(chatLog, "[item #3]") {
		t.Fatalf("chat log =\n%s", chatLog)
	}
	actOut := e.ok(wsBHuman, "activity")
	if !strings.Contains(actOut, `#1 "Beta first"`) || !strings.Contains(actOut, `#3 "Beta third"`) {
		t.Fatalf("activity =\n%s", actOut)
	}

	// TODO --item accepts #number and legacy global ID, and todo ls displays [item #1] / [item #3].
	if tdOut1 := e.ok(wsBAgent, "todo", "add", "Review #1", "--to", "Alex", "--item", "1"); !strings.Contains(tdOut1, "Item      #1 Beta first") {
		t.Fatalf("todo add --item 1 = %q", tdOut1)
	}
	if tdOut2 := e.ok(wsBAgent, "todo", "add", "Review #3", "--to", "Alex", "--item", b3LegacyID); !strings.Contains(tdOut2, "Item      #3 Beta third") {
		t.Fatalf("todo add --item legacy = %q", tdOut2)
	}
	todoLs := e.ok(wsBHuman, "todo", "ls")
	if !strings.Contains(todoLs, "[item #1]") || !strings.Contains(todoLs, "[item #3]") {
		t.Fatalf("todo ls =\n%s", todoLs)
	}

	// Artifact --item accepts #number and legacy global ID, and artifact ls displays [item #1] / [item #3].
	docPath := filepath.Join(t.TempDir(), "design.md")
	if err := os.WriteFile(docPath, []byte("# Design\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.ok(wsBAgent, "artifact", "add", docPath, "--item", "3")
	if artLs := e.ok(wsBHuman, "artifact", "ls", "--item", b3LegacyID); !strings.Contains(artLs, "[item #3]") {
		t.Fatalf("artifact ls --item legacy = %q", artLs)
	}
	e.ok(wsBAgent, "artifact", "update", "design.md", "--item", "1")
	if artLs := e.ok(wsBHuman, "artifact", "ls", "--item", "1"); !strings.Contains(artLs, "[item #1]") {
		t.Fatalf("artifact ls --item 1 = %q", artLs)
	}
}
