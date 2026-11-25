package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

func TestArtifactCLI(t *testing.T) {
	e := newTestEnv(t)
	alexDir, agentDir := t.TempDir(), t.TempDir()
	e.login("Alex", "")
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(alexDir, "workspace", "create", "Docs", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	invite := e.url + "/w/" + ws.ID
	e.ok(alexDir, "join", invite, "--name", "Alex", "--account")
	e.ok(agentDir, "join", invite, "--pass", e.passFor(alexDir), "--name", "Ada")

	var item store.Item
	if err := json.Unmarshal([]byte(e.ok(alexDir, "backlog", "add", "Write design doc", "--json")), &item); err != nil {
		t.Fatal(err)
	}

	// Empty list initially
	if out := e.ok(alexDir, "artifact"); !strings.Contains(out, "No artifacts yet") {
		t.Fatalf("empty artifact ls = %q", out)
	}

	// 1. unison artifact add <path> [--title T] [--item ID]
	mdV1 := "# System Design v1\n\nHello from Ada!\n"
	srcPath := filepath.Join(agentDir, "design-doc.md")
	if err := os.WriteFile(srcPath, []byte(mdV1), 0o644); err != nil {
		t.Fatal(err)
	}
	permalink := strings.TrimSpace(e.ok(agentDir, "artifact", "add", srcPath, "--title", "System Design", "--item", "#1"))
	wantPermalink := "/w/" + ws.ID + "/artifacts/design-doc.md"
	if permalink != wantPermalink {
		t.Fatalf("artifact add printed %q, want %q", permalink, wantPermalink)
	}

	// 2. unison artifact add - --name proto.html (stdin)
	htmlBody := "<!doctype html><h1>Prototype</h1>"
	e.t.Chdir(agentDir)
	var stdinOut, stdinErr bytes.Buffer
	if code := runWithIO([]string{"--server", e.url, "artifact", "add", "-", "--name", "proto.html"}, strings.NewReader(htmlBody), &stdinOut, &stdinErr); code != 0 {
		t.Fatalf("artifact add stdin failed (%d): %s", code, stdinErr.String())
	}
	if strings.TrimSpace(stdinOut.String()) != "/w/"+ws.ID+"/artifacts/proto.html" {
		t.Fatalf("artifact add stdin output = %q", stdinOut.String())
	}

	// Duplicate add fails with conflict suggesting update
	if _, errOut, code := e.run(agentDir, "artifact", "add", srcPath); code != 1 || !strings.Contains(errOut, "update") {
		t.Errorf("duplicate artifact add = %d %q, want conflict mentioning update", code, errOut)
	}

	// 3. unison artifact ls (and --item filter & --json)
	lsOut := e.ok(alexDir, "artifact", "ls")
	if !strings.Contains(lsOut, "design-doc.md") || !strings.Contains(lsOut, "proto.html") || !strings.Contains(lsOut, "[item #1]") {
		t.Fatalf("artifact ls = %q", lsOut)
	}
	itemLs := e.ok(alexDir, "artifact", "ls", "--item", "1")
	if !strings.Contains(itemLs, "design-doc.md") || strings.Contains(itemLs, "proto.html") {
		t.Fatalf("artifact ls --item 1 = %q", itemLs)
	}
	var jsonList []store.Artifact
	if err := json.Unmarshal([]byte(e.ok(alexDir, "artifact", "ls", "--json")), &jsonList); err != nil || len(jsonList) != 2 {
		t.Fatalf("artifact ls --json = %+v, %v", jsonList, err)
	}

	// 4. unison artifact save <file-or-url> <dest> (name, /w/..., /api/..., full URL, and '-' stdout)
	rawAPIPath := "/api/workspaces/" + ws.ID + "/artifacts/design-doc.md"
	for _, ref := range []string{
		"design-doc.md",
		wantPermalink,
		rawAPIPath,
		e.url + wantPermalink,
		e.url + rawAPIPath,
	} {
		dest := filepath.Join(alexDir, "saved.md")
		_ = os.Remove(dest)
		e.ok(alexDir, "artifact", "save", ref, dest)
		got, err := os.ReadFile(dest)
		if err != nil || string(got) != mdV1 {
			t.Fatalf("artifact save %q = %q, %v; want %q", ref, got, err, mdV1)
		}
	}
	if stdoutContent := e.ok(alexDir, "artifact", "save", "proto.html", "-"); stdoutContent != htmlBody {
		t.Fatalf("artifact save - = %q, want %q", stdoutContent, htmlBody)
	}

	// 5. unison artifact update <name> <path> (PUT content replace in-place)
	// Initialize Alex's wait cursor before Ada updates the artifact so we also test wait/describeEvent!
	if _, _, code := e.run(alexDir, "wait", "--timeout", "0"); code != 2 {
		// Drain any prior unread events if present
		_, _, _ = e.run(alexDir, "wait", "--timeout", "1")
	}

	mdV2 := "# System Design v2\n\nUpdated in-place keeping permalink!\n"
	v2Path := filepath.Join(agentDir, "v2.md")
	_ = os.WriteFile(v2Path, []byte(mdV2), 0o644)
	updOut := e.ok(agentDir, "artifact", "update", "design-doc.md", v2Path)
	if !strings.Contains(updOut, wantPermalink) || !strings.Contains(updOut, "v2") {
		t.Fatalf("artifact update content = %q", updOut)
	}
	if got := e.ok(alexDir, "artifact", "save", "design-doc.md", "-"); got != mdV2 {
		t.Fatalf("saved after PUT update = %q, want %q", got, mdV2)
	}

	// Alex's wait wakes and describes artifact.updated
	waitOut := e.ok(alexDir, "wait", "--timeout", "2")
	if !strings.Contains(waitOut, "updated artifact design-doc.md (v2)") || !strings.Contains(waitOut, wantPermalink) {
		t.Fatalf("wait output = %q, want artifact.updated description", waitOut)
	}

	// 6. unison artifact update <name> --title ... --item none (PATCH metadata only)
	var patched store.Artifact
	if err := json.Unmarshal([]byte(e.ok(agentDir, "artifact", "update", "design-doc.md", "--title", "Final Spec", "--item", "none", "--json")), &patched); err != nil {
		t.Fatal(err)
	}
	if patched.Title != "Final Spec" || patched.ItemID != nil || patched.Version != 3 || patched.URL != wantPermalink {
		t.Fatalf("PATCH metadata = %+v", patched)
	}
	if got := e.ok(alexDir, "artifact", "save", "design-doc.md", "-"); got != mdV2 {
		t.Fatalf("content after metadata PATCH = %q, want %q", got, mdV2)
	}

	// 7. unison artifact delete <name>
	e.ok(agentDir, "artifact", "delete", "design-doc.md")
	if _, _, code := e.run(alexDir, "artifact", "save", "design-doc.md", "-"); code != 1 {
		t.Errorf("saving deleted artifact exited %d, want 1", code)
	}
}
