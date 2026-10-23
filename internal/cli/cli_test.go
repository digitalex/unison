package cli

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digitalex/unison/internal/server"
	"github.com/digitalex/unison/internal/store"
	"github.com/digitalex/unison/internal/store/storetest"
)

// testEnv runs a real server and CLI invocations against it, each in its
// own directory (and so with its own .unison.json identity).
type testEnv struct {
	t   *testing.T
	url string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	st, err := store.Open(storetest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ts := httptest.NewServer(server.New(st, nil))
	t.Cleanup(ts.Close)
	for _, k := range []string{"UNISON_SERVER", "UNISON_WORKSPACE", "UNISON_TOKEN", "UNISON_CONFIG"} {
		t.Setenv(k, "")
	}
	return &testEnv{t: t, url: ts.URL}
}

// run executes the CLI in dir and returns stdout, stderr and the exit code.
func (e *testEnv) run(dir string, args ...string) (string, string, int) {
	e.t.Helper()
	e.t.Chdir(dir)
	var out, errOut bytes.Buffer
	code := Run(append([]string{"--server", e.url}, args...), &out, &errOut)
	return out.String(), errOut.String(), code
}

// ok runs the CLI and fails the test on a non-zero exit.
func (e *testEnv) ok(dir string, args ...string) string {
	e.t.Helper()
	out, errOut, code := e.run(dir, args...)
	if code != 0 {
		e.t.Fatalf("unison %v: exit %d\nstdout: %s\nstderr: %s", args, code, out, errOut)
	}
	return out
}

func TestEndToEnd(t *testing.T) {
	e := newTestEnv(t)
	human, agent := t.TempDir(), t.TempDir()

	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(human, "workspace", "create", "Unison", "--repo", "https://github.com/digitalex/unison", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	invite := e.url + "/w/" + ws.ID
	e.ok(human, "join", invite, "--name", "Alex", "--emoji", "🧑")
	e.ok(agent, "join", invite, "--name", "Ada", "--emoji", "🦊")
	if _, err := os.Stat(filepath.Join(agent, ConfigFile)); err != nil {
		t.Fatalf("join did not save a session: %v", err)
	}
	if out := e.ok(agent, "whoami"); !strings.Contains(out, "Ada") {
		t.Errorf("whoami = %q", out)
	}
	if out := e.ok(agent, "workspace"); !strings.Contains(out, "github.com/digitalex/unison") {
		t.Errorf("workspace = %q, want the repo URL", out)
	}

	e.ok(human, "backlog", "add", "Second", "--body", "later")
	e.ok(human, "backlog", "add", "First", "--position", "1")
	if out := e.ok(agent, "backlog"); strings.Index(out, "First") > strings.Index(out, "Second") {
		t.Errorf("backlog not in priority order:\n%s", out)
	}

	// The agent claims the top item; the human can no longer take it.
	if out := e.ok(agent, "backlog", "claim"); !strings.Contains(out, "First") || !strings.Contains(out, "Ada") {
		t.Errorf("claim = %q", out)
	}
	if _, errOut, code := e.run(human, "backlog", "assign", "2"); code == 0 || !strings.Contains(errOut, "already assigned") {
		t.Errorf("double assign: exit %d, stderr %q", code, errOut)
	}

	// The human's message wakes the parked agent.
	done := make(chan string)
	go func() {
		time.Sleep(300 * time.Millisecond)
		var out, errOut bytes.Buffer
		Run([]string{"--server", e.url, "--workspace", ws.ID, "--token", tokenOf(t, human), "chat", "send", "@Ada", "how", "is", "it", "going?"}, &out, &errOut)
		done <- errOut.String()
	}()
	out := e.ok(agent, "wait", "--mentions", "--timeout", "10")
	if errOut := <-done; errOut != "" {
		t.Fatalf("chat send: %s", errOut)
	}
	if !strings.Contains(out, "@Ada how is it going?") {
		t.Errorf("wait = %q, want the mention", out)
	}

	e.ok(agent, "chat", "send", "Done!", "--reply-to", "1")
	e.ok(agent, "backlog", "close", "2", "--note", "https://github.com/digitalex/unison/pull/1")
	if out := e.ok(human, "chat", "log"); !strings.Contains(out, "↳") || !strings.Contains(out, "Done!") {
		t.Errorf("chat log missing thread reply:\n%s", out)
	}
	if out := e.ok(human, "backlog", "--status", "done"); !strings.Contains(out, "pull/1") {
		t.Errorf("done list = %q", out)
	}

	// The agent's own reply and close must not wake it: with nothing else
	// new, wait times out with exit code 2.
	if _, _, code := e.run(agent, "wait", "--timeout", "1"); code != 2 {
		t.Errorf("idle wait exit = %d, want 2", code)
	}

	// No open items left after claiming the remaining one.
	e.ok(agent, "backlog", "claim")
	if _, _, code := e.run(agent, "backlog", "claim"); code != 3 {
		t.Errorf("claim on empty backlog exit = %d, want 3", code)
	}

	// Closing with a GitHub PR URL in the note linked it to the item.
	if out := e.ok(human, "backlog", "show", "2"); !strings.Contains(out, "PRs       https://github.com/digitalex/unison/pull/1") {
		t.Errorf("closed item lacks its PR link:\n%s", out)
	}

	// A comment on an item assigned to the agent wakes `wait --mentions`.
	go func() {
		time.Sleep(300 * time.Millisecond)
		var out, errOut bytes.Buffer
		Run([]string{"--server", e.url, "--workspace", ws.ID, "--token", tokenOf(t, human), "backlog", "comment", "1", "Is", "this", "still", "needed?"}, &out, &errOut)
		done <- errOut.String()
	}()
	out = e.ok(agent, "wait", "--mentions", "--timeout", "10")
	if errOut := <-done; errOut != "" {
		t.Fatalf("backlog comment: %s", errOut)
	}
	if !strings.Contains(out, `commented on #1 "Second"`) || !strings.Contains(out, "still needed?") {
		t.Errorf("wait = %q, want the comment", out)
	}
	if out := e.ok(agent, "backlog", "comment", "1", "Yes,", "@Alex"); !strings.Contains(out, "Commented on #1 (comment 2)") {
		t.Errorf("comment = %q", out)
	}
	if out := e.ok(human, "backlog", "comments", "1"); !strings.Contains(out, "🧑 Alex: Is this still needed?") || !strings.Contains(out, "🦊 Ada: Yes, @Alex") {
		t.Errorf("comments = %q", out)
	}
	if _, errOut, code := e.run(human, "backlog", "comment", "1", "--delete", "2"); code == 0 || !strings.Contains(errOut, "only the author") {
		t.Errorf("deleting another's comment: exit %d, stderr %q", code, errOut)
	}
	e.ok(agent, "backlog", "comment", "1", "--delete", "2")
	e.ok(agent, "backlog", "pr", "1", "https://github.com/acme/auth/pull/9")
	out = e.ok(agent, "backlog", "pr", "1", "https://github.com/digitalex/unison/pull/12")
	if !strings.Contains(out, "acme/auth/pull/9") || !strings.Contains(out, "unison/pull/12") {
		t.Errorf("pr link = %q, want both PRs", out)
	}
	if _, errOut, code := e.run(agent, "backlog", "pr", "1", "https://github.com/acme/auth/pull/9"); code == 0 || !strings.Contains(errOut, "already linked") {
		t.Errorf("duplicate PR: exit %d, stderr %q", code, errOut)
	}
	if out := e.ok(agent, "backlog", "pr", "1", "--remove", "https://github.com/acme/auth/pull/9"); strings.Contains(out, "acme/auth") {
		t.Errorf("pr --remove = %q", out)
	}
	out = e.ok(human, "backlog", "show", "1")
	if !strings.Contains(out, "unison/pull/12") || !strings.Contains(out, "Comments  1") || !strings.Contains(out, "Is this still needed?") {
		t.Errorf("show = %q, want PRs and comments", out)
	}
	var shown struct {
		Item     store.Item      `json:"item"`
		Comments []store.Comment `json:"comments"`
	}
	if err := json.Unmarshal([]byte(e.ok(human, "backlog", "show", "1", "--json")), &shown); err != nil || len(shown.Item.PRs) != 1 || len(shown.Comments) != 1 {
		t.Errorf("show --json = %+v, %v", shown, err)
	}

	// Metrics: adding one also queues a backlog item to track it.
	out = e.ok(human, "metric", "add", "Build time", "--direction", "decrease", "--unit", "s", "--target", "30", "--description", "CI wall-clock time")
	if !strings.Contains(out, "↓ decrease") || !strings.Contains(out, "Target    30 s") || !strings.Contains(out, "Added backlog item #3") {
		t.Errorf("metric add = %q", out)
	}
	if out := e.ok(agent, "backlog", "show", "3"); !strings.Contains(out, "Track metric: Build time") || !strings.Contains(out, "CI wall-clock time") {
		t.Errorf("tracking item = %q", out)
	}
	var m store.Metric
	if err := json.Unmarshal([]byte(e.ok(agent, "metric", "add", "Coverage", "--unit", "%", "--json")), &m); err != nil {
		t.Fatal(err)
	}
	if m.Direction != store.DirectionIncrease || m.ItemID == nil || *m.ItemID != 4 {
		t.Errorf("metric add --json = %+v", m)
	}
	if _, errOut, code := e.run(agent, "metric", "add", "build TIME"); code == 0 || !strings.Contains(errOut, "already exists") {
		t.Errorf("duplicate metric: exit %d, stderr %q", code, errOut)
	}
	if _, errOut, code := e.run(agent, "metric", "add", "Speed", "--direction", "up"); code == 0 || !strings.Contains(errOut, "direction") {
		t.Errorf("bad direction: exit %d, stderr %q", code, errOut)
	}
	if out := e.ok(agent, "metric", "edit", "1", "--target", "none", "--name", "CI build time"); !strings.Contains(out, "CI build time") || strings.Contains(out, "Target") {
		t.Errorf("metric edit = %q", out)
	}
	if out := e.ok(human, "metric"); !strings.Contains(out, "CI build time") || !strings.Contains(out, "tracked by #3") || !strings.Contains(out, "Coverage") {
		t.Errorf("metric list = %q", out)
	}
	e.ok(human, "metric", "delete", "2")
	if out := e.ok(human, "metric", "show", "1"); !strings.Contains(out, "backlog item #3") {
		t.Errorf("metric show = %q", out)
	}
	if out := e.ok(human, "backlog"); !strings.Contains(out, "Track metric: Coverage") {
		t.Errorf("deleting a metric removed its backlog item:\n%s", out)
	}

	if out := e.ok(agent, "--agent-help"); !strings.Contains(out, ws.ID) || !strings.Contains(out, "unison wait") {
		t.Errorf("agent help is not tailored to the workspace")
	}
}

func tokenOf(t *testing.T, dir string) string {
	t.Helper()
	cfg, err := loadConfig(filepath.Join(dir, ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Token
}

func TestParseInvite(t *testing.T) {
	for _, tc := range []struct{ in, server, ws string }{
		{"http://localhost:4600/w/ws_abc", "http://localhost:4600", "ws_abc"},
		{"http://localhost:4600/w/ws_abc/agent.md", "http://localhost:4600", "ws_abc"},
		{"https://unison.example.com", "https://unison.example.com", ""},
	} {
		server, ws, err := parseInvite(tc.in)
		if err != nil || server != tc.server || ws != tc.ws {
			t.Errorf("parseInvite(%q) = %q, %q, %v; want %q, %q", tc.in, server, ws, err, tc.server, tc.ws)
		}
	}
	if _, _, err := parseInvite("ws_abc"); err == nil {
		t.Error("parseInvite(bare id) succeeded, want error")
	}
}
