package cli

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/server"
	"github.com/digitalex/unison/internal/store"
	"github.com/digitalex/unison/internal/store/storetest"
)

// TestPrivateWorkspaceFlow: Alex logs in and creates a workspace, makes an
// invite link; Bob joins with it; Carol joins by email invite; an agent
// joins with a pass; a participant from before accounts is refused with
// the explanation; members, invites and removal work from the CLI.
func TestPrivateWorkspaceFlow(t *testing.T) {
	e := newTestEnv(t)
	alexDir, alexDesk, bobDir, carolDir, agent, legacyDir := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()

	if _, errOut, code := e.run(alexDir, "workspace", "create", "Nope"); code == 0 || !strings.Contains(errOut, "unison login") {
		t.Errorf("create before login: exit %d %q", code, errOut)
	}
	alex := e.login("Alex", "")
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(alexDir, "workspace", "create", "Unison", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	url := e.url + "/w/" + ws.ID
	if out := e.ok(alexDir, "workspace", "list"); !strings.Contains(out, ws.ID+"  Unison") {
		t.Errorf("workspace list = %q", out)
	}
	if _, errOut, code := e.run(alexDir, "join", url, "--name", "Alex"); code == 0 || !strings.Contains(errOut, "joining needs an invite") {
		t.Errorf("join without --account/--pass: exit %d %q", code, errOut)
	}
	e.ok(alexDir, "join", url, "--name", "Alex", "--emoji", "🦊", "--account")
	if out := e.ok(alexDesk, "join", url, "--name", "AlexHome", "--account"); !strings.Contains(out, "Welcome back: you are 🦊 Alex") {
		t.Errorf("second join --account = %q, want the same participant", out)
	}

	link := regexp.MustCompile(`(?m)^(http\S+/join\?invite=ui_\S+)$`).FindStringSubmatch(e.ok(alexDir, "invite", "link", "--expires", "2d"))
	if link == nil {
		t.Fatal("invite link printed no URL")
	}
	e.ok(alexDir, "invite", "email", "carol@example.com")
	if out := e.ok(alexDir, "invites"); !strings.Contains(out, "email carol@example.com") || !strings.Contains(out, "link, used 0 time(s)") {
		t.Errorf("invites = %q", out)
	}

	bob := e.login("Bob", "")
	if _, errOut, code := e.run(bobDir, "join", url, "--account"); code == 0 || !strings.Contains(errOut, "not a member") {
		t.Errorf("Bob without an invite: exit %d %q", code, errOut)
	}
	if out := e.ok(bobDir, "join", link[1], "--name", "Bob"); !strings.Contains(out, "linked to your account") {
		t.Errorf("join by invite link = %q", out)
	}
	e.login("Carol", "carol@example.com")
	e.ok(carolDir, "join", url, "--name", "Carol", "--account")

	// The agent joins with Bob's pass (Bob is signed in in this shell: no
	// matter, a plain join never uses the account).
	e.login("Bob", "")
	e.ok(agent, "join", url, "--pass", e.passFor(bobDir), "--name", "Quill", "--emoji", "🪶")
	e.ok(agent, "chat", "send", "hello")

	// A participant from before accounts: refused, with the explanation.
	_, legacyTok, _, _ := e.st.Join(t.Context(), ws.ID, "Old", "")
	cfg := `{"server":"` + e.url + `","workspace":"` + ws.ID + `","token":"` + legacyTok + `"}`
	if err := os.WriteFile(filepath.Join(legacyDir, ConfigFile), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code := e.run(legacyDir, "chat", "send", "anyone?"); code == 0 || !strings.Contains(errOut, "predates accounts") {
		t.Errorf("legacy participant: exit %d %q", code, errOut)
	}

	out := e.ok(bobDir, "members")
	for _, want := range []string{"Alex: 🦊 Alex", "Bob: ", "Carol: ", "Not linked to a member:", "Old"} {
		if !strings.Contains(out, want) {
			t.Errorf("members lacks %q:\n%s", want, out)
		}
	}
	// Any member removes any member; the removed one is then refused.
	e.ok(bobDir, "members", "remove", strconv.FormatInt(alex.ID, 10))
	if _, errOut, code := e.run(alexDir, "chat", "send", "still here?"); code == 0 || !strings.Contains(errOut, "no longer a member") {
		t.Errorf("removed member: exit %d %q", code, errOut)
	}
	inv := regexp.MustCompile(`(?m)^(\d+)\s+link`).FindStringSubmatch(e.ok(bobDir, "invites"))
	if inv == nil {
		t.Fatal("no link invite listed")
	}
	if out := e.ok(bobDir, "invite", "revoke", inv[1]); !strings.Contains(out, "Revoked invite") {
		t.Errorf("invite revoke = %q", out)
	}
	_ = bob
}

// TestOpenModeCLI: with privacy off (UNISON_AUTH=off, local development)
// the CLI works without accounts: create, list and join with just a name.
func TestOpenModeCLI(t *testing.T) {
	st, err := store.Open(storetest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := server.New(st, nil)
	srv.SetAuth(server.AuthConfig{Open: true})
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	for _, k := range []string{"UNISON_SERVER", "UNISON_WORKSPACE", "UNISON_TOKEN", "UNISON_CONFIG"} {
		t.Setenv(k, "")
	}
	t.Setenv("UNISON_CREDENTIALS", filepath.Join(t.TempDir(), "credentials.json"))
	e := &testEnv{t: t, url: ts.URL, st: st}
	dir := t.TempDir()

	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(dir, "--server", ts.URL, "workspace", "create", "Local", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	if out := e.ok(dir, "join", ts.URL+"/w/"+ws.ID, "--name", "Ada", "--emoji", "🦊"); !strings.Contains(out, "Joined as 🦊 Ada") {
		t.Errorf("open-mode join = %q", out)
	}
	e.ok(dir, "chat", "send", "hello")
	if out := e.ok(dir, "chat", "log"); !strings.Contains(out, "hello") {
		t.Errorf("chat = %q", out)
	}
}
