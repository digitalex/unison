package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digitalex/unison/internal/store"
)

// `unison name` shows and changes who you are in the workspace, keeps the
// saved session's name in step, and a parked `unison wait` answers to the
// new name (#303).
func TestName(t *testing.T) {
	e := newTestEnv(t)
	human, agent := t.TempDir(), t.TempDir()
	e.login("Alex", "")
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(human, "workspace", "create", "Unison", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	url := e.url + "/w/" + ws.ID
	e.ok(human, "join", url, "--name", "Alex", "--emoji", "🦊", "--account")
	e.ok(agent, "join", url, "--pass", e.passFor(human), "--name", "Quill", "--emoji", "🪶")
	savedName := func() string {
		t.Helper()
		cfg, err := loadConfig(filepath.Join(agent, ConfigFile))
		if err != nil {
			t.Fatal(err)
		}
		return cfg.Name
	}

	if out := e.ok(agent, "name"); out != "You are 🪶 Quill\n" {
		t.Errorf("name = %q", out)
	}
	e.ok(agent, "status", "working on #1")

	if out := e.ok(agent, "name", "Sparrow"); out != "You are 🪶 Sparrow\n" {
		t.Errorf("name Sparrow = %q", out)
	}
	if got := savedName(); got != "Sparrow" {
		t.Errorf("saved session name = %q, want Sparrow", got)
	}
	// The status survives a rename; so does the name when only the emoji changes.
	if out := e.ok(agent, "whoami"); !strings.Contains(out, "🪶 Sparrow") || !strings.Contains(out, "Status: working on #1") {
		t.Errorf("whoami after the rename = %q", out)
	}
	if out := e.ok(agent, "name", "--emoji", "🐦"); out != "You are 🐦 Sparrow\n" {
		t.Errorf("name --emoji = %q", out)
	}
	var p store.Participant
	if err := json.Unmarshal([]byte(e.ok(agent, "name", "Wren", "--emoji", "🐤", "--json")), &p); err != nil || p.Name != "Wren" || p.Emoji != "🐤" || p.Status != "working on #1" {
		t.Errorf("name --json = %+v, %v", p, err)
	}
	if got := savedName(); got != "Wren" {
		t.Errorf("saved session name after --json = %q, want Wren", got)
	}
	if out := e.ok(human, "who"); !strings.Contains(out, "🐤 Wren") || strings.Contains(out, "Quill") {
		t.Errorf("who = %q", out)
	}

	// The rules of joining apply.
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"name", "alex"}, "already taken"},
		{[]string{"name", "a@b"}, "cannot contain"},
		{[]string{"name", "two", "words"}, "one word"},
	} {
		if _, errOut, code := e.run(agent, tc.args...); code == 0 || !strings.Contains(errOut, tc.want) {
			t.Errorf("unison %v: exit %d %q, want %q", tc.args, code, errOut, tc.want)
		}
	}
	if got := savedName(); got != "Wren" {
		t.Errorf("saved session name after refusals = %q, want Wren", got)
	}

	// Renamed elsewhere (the web UI) while parked: wait wakes for the new
	// name and writes it, not the old one, back with its cursor.
	other := t.TempDir()
	e.ok(other, "join", url, "--pass", e.passFor(human), "--name", "Finch")
	token := func(dir string) string {
		t.Helper()
		cfg, err := loadConfig(filepath.Join(dir, ConfigFile))
		if err != nil || cfg.Token == "" {
			t.Fatalf("no session in %s: %v", dir, err)
		}
		return cfg.Token
	}
	wren, finch := newClient(e.url, token(agent)), newClient(e.url, token(other))
	api := "/api/workspaces/" + ws.ID
	done := make(chan error, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		if err := wren.do("PATCH", api+"/me", map[string]string{"name": "Robin"}, nil); err != nil {
			done <- err
			return
		}
		done <- finch.do("POST", api+"/messages", map[string]string{"body": "@Robin are you there?"}, nil)
	}()
	out := e.ok(agent, "wait", "--mentions", "--timeout", "10")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "@Robin are you there?") {
		t.Errorf("wait after a rename elsewhere = %q", out)
	}
	if got := savedName(); got != "Robin" {
		t.Errorf("saved session name after wait = %q, want Robin", got)
	}
}
