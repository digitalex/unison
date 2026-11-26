package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

func TestTodoFilterByAssigneeCLI(t *testing.T) {
	e := newTestEnv(t)
	alexDir, blairDir, agentDir := t.TempDir(), t.TempDir(), t.TempDir()

	e.login("Alex", "alex@example.com")
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(alexDir, "workspace", "create", "Platform", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	wsURL := e.url + "/w/" + ws.ID
	e.ok(alexDir, "join", wsURL, "--name", "Alex", "--emoji", "🧑", "--account")

	invRaw := e.ok(alexDir, "invite", "link", "--json")
	var inv struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(invRaw), &inv); err != nil {
		t.Fatal(err)
	}

	e.login("Blair", "blair@example.com")
	e.ok(blairDir, "join", wsURL, "--name", "Blair", "--emoji", "👩‍💻", "--account", "--invite", inv.Token)
	e.ok(agentDir, "join", wsURL, "--pass", e.passFor(blairDir), "--name", "Ada", "--emoji", "🦊")

	e.ok(agentDir, "todo", "add", "Review API schema", "--to", "Alex")
	e.ok(agentDir, "todo", "add", "Provision staging DB", "--to", "Blair")

	var people []store.Participant
	if err := json.Unmarshal([]byte(e.ok(agentDir, "who", "--json")), &people); err != nil {
		t.Fatal(err)
	}
	var alexID int64
	for _, p := range people {
		if p.Name == "Alex" {
			alexID = p.ID
		}
	}
	if alexID == 0 {
		t.Fatal("Alex participant not found")
	}

	// Verify `todo --to Alex`, `todo ls --to @Alex`, and `todo list --to <id>`.
	for _, args := range [][]string{
		{"todo", "--to", "Alex"},
		{"todo", "ls", "--to", "@Alex"},
		{"todo", "list", "--to", strconv.FormatInt(alexID, 10)},
	} {
		out := e.ok(agentDir, args...)
		if !strings.Contains(out, "Review API schema") || strings.Contains(out, "Provision staging DB") {
			t.Errorf("%v =\n%s", args, out)
		}
	}

	blairOut := e.ok(agentDir, "todo", "ls", "--to", "Blair")
	if !strings.Contains(blairOut, "Provision staging DB") || strings.Contains(blairOut, "Review API schema") {
		t.Errorf("todo ls --to Blair =\n%s", blairOut)
	}

	// Reject invalid flag combinations.
	for _, badArgs := range [][]string{
		{"todo", "ls", "--mine", "--to", "Alex"},
		{"todo", "ls", "--all-workspaces", "--to", "Alex"},
		{"todo", "ls", "--to", ""},
		{"todo", "ls", "--to", "@"},
	} {
		if _, errOut, code := e.run(blairDir, badArgs...); code != 1 || !strings.Contains(errOut, "usage") {
			t.Errorf("%v = exit %d, stderr %q; want exit 1 usage error", badArgs, code, errOut)
		}
	}
}

func TestArtifactShowCLI(t *testing.T) {
	e := newTestEnv(t)
	alexDir, agentDir := t.TempDir(), t.TempDir()

	e.login("Alex", "")
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(alexDir, "workspace", "create", "Specs", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	wsURL := e.url + "/w/" + ws.ID
	e.ok(alexDir, "join", wsURL, "--name", "Alex", "--emoji", "🧑", "--account")
	e.ok(agentDir, "join", wsURL, "--pass", e.passFor(alexDir), "--name", "Ada", "--emoji", "🦊")

	e.ok(alexDir, "backlog", "add", "Draft architecture RFC")

	bodyV1 := "# Architecture RFC\n\nInitial proposal.\n"
	srcV1 := filepath.Join(agentDir, "rfc.md")
	if err := os.WriteFile(srcV1, []byte(bodyV1), 0o644); err != nil {
		t.Fatal(err)
	}
	permalink := strings.TrimSpace(e.ok(agentDir, "artifact", "add", srcV1, "--title", "Architecture RFC", "--item", "#1"))

	// 1. Metadata-only show on v1 (not yet updated, no content output).
	showV1 := e.ok(alexDir, "artifact", "show", "rfc.md")
	for _, want := range []string{
		"rfc.md  (v1)",
		"Title     Architecture RFC",
		"Type      text/markdown; charset=utf-8",
		"Size      38 B",
		"SHA-256   ",
		"Item      #1 Draft architecture RFC",
		"Uploaded  by 🦊 Ada",
		"URL       " + permalink,
	} {
		if !strings.Contains(showV1, want) {
			t.Errorf("artifact show rfc.md missing %q in:\n%s", want, showV1)
		}
	}
	if strings.Contains(showV1, "Updated") {
		t.Errorf("v1 artifact show should not print Updated line:\n%s", showV1)
	}
	if strings.Contains(showV1, "Initial proposal.") {
		t.Errorf("artifact show without --content should not print content:\n%s", showV1)
	}

	// 2. Show with --content via viewer path, raw API path, and full URL.
	rawAPIPath := "/api/workspaces/" + ws.ID + "/artifacts/rfc.md"
	for _, ref := range []string{permalink, rawAPIPath, e.url + permalink} {
		withContent := e.ok(alexDir, "artifact", "show", ref, "--content")
		wantSuffix := "URL       " + permalink + "\n\n" + bodyV1
		if !strings.HasSuffix(withContent, wantSuffix) {
			t.Errorf("artifact show %q --content =\n%s\nwant suffix:\n%s", ref, withContent, wantSuffix)
		}
	}

	// 3. --json metadata (with and without --content).
	var metaOnly map[string]any
	if err := json.Unmarshal([]byte(e.ok(alexDir, "artifact", "show", "rfc.md", "--json")), &metaOnly); err != nil {
		t.Fatal(err)
	}
	if _, hasContent := metaOnly["content"]; hasContent {
		t.Errorf("artifact show --json without --content unexpectedly included content: %v", metaOnly)
	}
	var metaWithContent struct {
		store.Artifact
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(e.ok(alexDir, "artifact", "show", permalink, "--content", "--json")), &metaWithContent); err != nil {
		t.Fatal(err)
	}
	if metaWithContent.Name != "rfc.md" || metaWithContent.Version != 1 || metaWithContent.Content != bodyV1 {
		t.Errorf("artifact show --content --json = %+v", metaWithContent)
	}

	// 4. After update by Alex, show prints v2 and Updated line.
	bodyV2 := "# Architecture RFC v2\n\nApproved design."
	srcV2 := filepath.Join(alexDir, "rfc-v2.md")
	if err := os.WriteFile(srcV2, []byte(bodyV2), 0o644); err != nil {
		t.Fatal(err)
	}
	e.ok(alexDir, "artifact", "update", "rfc.md", srcV2)

	showV2 := e.ok(agentDir, "artifact", "show", "--content", "rfc.md")
	for _, want := range []string{
		"rfc.md  (v2)",
		"Uploaded  by 🦊 Ada",
		"Updated   by 🧑 Alex",
		"URL       " + permalink + "\n\n" + bodyV2 + "\n",
	} {
		if !strings.Contains(showV2, want) {
			t.Errorf("artifact show v2 missing %q in:\n%s", want, showV2)
		}
	}

	// 5. Binary artifact with --content omits non-UTF-8/NUL payload cleanly.
	binPath := filepath.Join(agentDir, "pixel.png")
	pngHeader := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00, 0x00, 0x00, 0x0d}
	if err := os.WriteFile(binPath, pngHeader, 0o644); err != nil {
		t.Fatal(err)
	}
	e.ok(agentDir, "artifact", "add", binPath)
	binShow := e.ok(alexDir, "artifact", "show", "pixel.png", "--content")
	if !strings.HasSuffix(binShow, "URL       /w/"+ws.ID+"/artifacts/pixel.png\n") {
		t.Errorf("binary artifact show --content =\n%s", binShow)
	}
}

func TestWebhookDeliveriesLimitCLI(t *testing.T) {
	e := newTestEnv(t)
	alexDir, agentDir := t.TempDir(), t.TempDir()

	e.login("Alex", "")
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(alexDir, "workspace", "create", "Hooks", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	wsURL := e.url + "/w/" + ws.ID
	e.ok(alexDir, "join", wsURL, "--name", "Alex", "--account")
	e.ok(agentDir, "join", wsURL, "--pass", e.passFor(alexDir), "--name", "Ada")

	hookOut := e.ok(alexDir, "webhook", "add", "GitHub", "--instructions", "Check CI")
	hookURL := regexp.MustCompile(`https?://\S+/hooks/wh_\S+`).FindString(hookOut)
	if hookURL == "" {
		t.Fatalf("missing hook URL in %q", hookOut)
	}

	for _, prNum := range []int{10, 11, 12} {
		payload := `{"action":"opened","pull_request":{"number":` + strconv.Itoa(prNum) + `,"title":"PR ` + strconv.Itoa(prNum) + `"}}`
		req, _ := http.NewRequest("POST", hookURL, bytes.NewBufferString(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitHub-Event", "pull_request")
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusAccepted {
			t.Fatalf("POST webhook #%d = %v, %v", prNum, resp, err)
		}
		resp.Body.Close()
	}

	allOut := e.ok(agentDir, "webhook", "deliveries", "1")
	if !strings.Contains(allOut, "#10") || !strings.Contains(allOut, "#11") || !strings.Contains(allOut, "#12") {
		t.Fatalf("webhook deliveries 1 =\n%s", allOut)
	}

	// Flag after <id>: --limit 1 returns only the newest delivery (#12).
	limit1 := e.ok(agentDir, "webhook", "deliveries", "1", "--limit", "1")
	if !strings.Contains(limit1, "#12") || strings.Contains(limit1, "#11") || strings.Contains(limit1, "#10") {
		t.Fatalf("webhook deliveries 1 --limit 1 =\n%s", limit1)
	}

	// Flag before <id> + --json: --limit 2 returns the newest 2 deliveries (#12, #11).
	var deliveries []store.Delivery
	if err := json.Unmarshal([]byte(e.ok(agentDir, "webhook", "deliveries", "--limit", "2", "1", "--json")), &deliveries); err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 2 || !strings.Contains(deliveries[0].Summary, "#12") || !strings.Contains(deliveries[1].Summary, "#11") {
		t.Fatalf("webhook deliveries --limit 2 1 --json = %+v", deliveries)
	}

	// Negative --limit fails with usage error.
	if _, errOut, code := e.run(agentDir, "webhook", "deliveries", "1", "--limit", "-1"); code != 1 || !strings.Contains(errOut, "usage") {
		t.Errorf("negative --limit = exit %d, stderr %q", code, errOut)
	}
}

func TestBacklogCommentDeleteLegacyCLI(t *testing.T) {
	e := newTestEnv(t)
	alexDir := t.TempDir()

	e.login("Alex", "")
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(alexDir, "workspace", "create", "History", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	e.ok(alexDir, "join", e.url+"/w/"+ws.ID, "--name", "Alex", "--emoji", "🧑", "--account")

	// Reject --legacy without --delete or combined with --action.
	e.ok(alexDir, "backlog", "add", "Pick database", "--choice", "SQLite", "--choice", "Postgres")
	for _, badArgs := range [][]string{
		{"backlog", "comment", "1", "Hello", "--legacy"},
		{"backlog", "comment", "1", "--legacy"},
		{"backlog", "comment", "1", "--delete", "1", "--legacy", "--action"},
	} {
		if _, errOut, code := e.run(alexDir, badArgs...); code != 1 || !strings.Contains(errOut, "usage") {
			t.Errorf("%v = exit %d, stderr %q; want usage error", badArgs, code, errOut)
		}
	}

	// Create chat message #1 AND legacy item_comments #1 on item #1 so both share numeric ID 1!
	e.ok(alexDir, "backlog", "comment", "1", "Chat discussion root")
	e.ok(alexDir, "backlog", "answer", "1", "Postgres")

	var threadBefore []store.Message
	if err := json.Unmarshal([]byte(e.ok(alexDir, "backlog", "comments", "1", "--json")), &threadBefore); err != nil {
		t.Fatal(err)
	}
	if len(threadBefore) != 2 {
		t.Fatalf("expected 2 entries on #1, got %+v", threadBefore)
	}
	var sawChat1, sawLegacy1 bool
	for _, m := range threadBefore {
		if m.ID == 1 && !m.Legacy && m.Body == "Chat discussion root" {
			sawChat1 = true
		}
		if m.ID == 1 && m.Legacy && m.Body == "Decision: Postgres" {
			sawLegacy1 = true
		}
	}
	if !sawChat1 || !sawLegacy1 {
		t.Fatalf("expected chat #1 and legacy #1 collision, got %+v", threadBefore)
	}

	// `--delete 1 --legacy` deletes ONLY the legacy item_comments row, preserving chat message #1!
	delOut := e.ok(alexDir, "backlog", "comment", "1", "--delete", "1", "--legacy")
	if !strings.Contains(delOut, "Deleted comment 1 on #1.") {
		t.Fatalf("delete --legacy output = %q", delOut)
	}

	var threadAfter []store.Message
	if err := json.Unmarshal([]byte(e.ok(alexDir, "backlog", "comments", "1", "--json")), &threadAfter); err != nil {
		t.Fatal(err)
	}
	if len(threadAfter) != 1 || threadAfter[0].ID != 1 || threadAfter[0].Legacy || threadAfter[0].Body != "Chat discussion root" {
		t.Fatalf("thread after --legacy delete = %+v; want only chat message #1 remaining", threadAfter)
	}

	// Deleting legacy #1 again returns 404 even though chat message #1 still exists.
	if _, errOut, code := e.run(alexDir, "backlog", "comment", "1", "--delete", "1", "--legacy"); code != 1 || !strings.Contains(errOut, "not found") {
		t.Errorf("second --legacy delete = exit %d, stderr %q; want not found", code, errOut)
	}
}
