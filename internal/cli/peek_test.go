package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

// TestPeek: `unison peek` says whether `unison wait` would wake now (true,
// exit 0; false, exit 2) and leaves wait's cursor and the read marker alone.
func TestPeek(t *testing.T) {
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
	peek := func(want bool, args ...string) string {
		t.Helper()
		out, errOut, code := e.run(agent, append([]string{"peek"}, args...)...)
		wantCode := 0
		if !want {
			wantCode = 2
		}
		if code != wantCode || errOut != "" || (len(args) == 0 || args[0] != "--json") && out != map[bool]string{true: "true\n", false: "false\n"}[want] {
			t.Errorf("peek %v = %q (stderr %q), exit %d; want %v, exit %d", args, out, errOut, code, want, wantCode)
		}
		return out
	}
	config := func() string {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(agent, ConfigFile))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}

	peek(false)
	e.ok(human, "chat", "send", "Morning, team")
	before, read := config(), e.ok(agent, "chat", "read", "--show")
	peek(true)
	peek(false, "--mentions")
	peek(true)
	if after := config(); after != before {
		t.Errorf("peek changed %s:\n%s\nwas:\n%s", ConfigFile, after, before)
	}
	if after := e.ok(agent, "chat", "read", "--show"); after != read {
		t.Errorf("peek moved the read marker: %q, was %q", after, read)
	}

	// wait hands over what peek saw, and then nothing is waiting.
	if out := e.ok(agent, "wait", "--timeout", "5"); !strings.Contains(out, "Morning, team") {
		t.Errorf("wait after peek = %q", out)
	}
	peek(false)

	// From wait's saved cursor: a new open item is work to pick up, even
	// with --mentions; a plain message isn't.
	e.ok(human, "backlog", "add", "Ship it")
	e.ok(human, "chat", "send", "Lunch?")
	peek(true, "--mentions")
	var resp struct {
		Pending   bool  `json:"pending"`
		Count     int   `json:"count"`
		LatestSeq int64 `json:"latest_seq"`
	}
	if err := json.Unmarshal([]byte(peek(true, "--json")), &resp); err != nil || !resp.Pending || resp.Count != 2 || resp.LatestSeq == 0 {
		t.Errorf("peek --json = %+v, %v", resp, err)
	}
	if out := e.ok(agent, "wait", "--timeout", "5"); !strings.Contains(out, "Ship it") || !strings.Contains(out, "Lunch?") {
		t.Errorf("wait = %q", out)
	}
	peek(false)
	if err := json.Unmarshal([]byte(peek(false, "--json")), &resp); err != nil || resp.Pending || resp.Count != 0 {
		t.Errorf("peek --json with nothing waiting = %+v, %v", resp, err)
	}

	if _, errOut, code := e.run(agent, "peek", "now"); code != 1 || !strings.Contains(errOut, "peek [--mentions]") {
		t.Errorf("peek now: exit %d, stderr %q", code, errOut)
	}
}
