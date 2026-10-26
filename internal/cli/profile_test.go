package cli

import (
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/digitalex/unison/internal/server"
	"github.com/digitalex/unison/internal/store"
	"github.com/digitalex/unison/internal/store/storetest"
)

// TestProfileCommands: login, sessions, an agent joins with a pass,
// `agents` lists it, `agents revoke` stops it with a clear message.
func TestProfileCommands(t *testing.T) {
	st, err := store.Open(storetest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := server.New(st, nil)
	srv.SetAuth(server.AuthConfig{DeviceInterval: time.Second})
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	e := &testEnv{t: t, url: ts.URL}
	for _, k := range []string{"UNISON_SERVER", "UNISON_WORKSPACE", "UNISON_TOKEN", "UNISON_CONFIG"} {
		t.Setenv(k, "")
	}
	t.Setenv("UNISON_CREDENTIALS", filepath.Join(t.TempDir(), "credentials.json"))
	human, agent := t.TempDir(), t.TempDir()

	if _, errOut, code := e.run(human, "sessions"); code == 0 || !strings.Contains(errOut, "unison login") {
		t.Errorf("sessions before login: exit %d %q", code, errOut)
	}
	owner, _ := st.LoginWithProvider(t.Context(), "github", store.Profile{Subject: "owner", Name: "Owner"})
	ws, _, _ := st.CreateWorkspaceFor(t.Context(), owner.ID, "Unison", "", "")
	invite := ts.URL + "/w/" + ws.ID
	alex, _ := st.LoginWithProvider(t.Context(), "github", store.Profile{Subject: "1", Name: "Alex", Email: "alex@example.com", EmailVerified: true})
	browser, _, _ := st.CreateSession(t.Context(), alex.ID, store.SessionBrowser, "Chrome on Linux")
	deviceLogin(t, ts.URL, browser)

	out := e.ok(human, "sessions")
	if !strings.Contains(out, "cli      unison on") || !strings.Contains(out, "(this one)") || !strings.Contains(out, "browser  Chrome on Linux") {
		t.Errorf("sessions =\n%s", out)
	}
	if out := e.ok(human, "agents"); !strings.Contains(out, "No agents yet") {
		t.Errorf("agents before any = %q", out)
	}

	if _, _, _, _, err := st.JoinWorkspace(t.Context(), ws.ID, alex.ID, "", "", ""); err == nil {
		t.Fatal("joined a workspace without an invite")
	}
	st.CreateEmailInvite(t.Context(), ws.ID, owner.ID, "alex@example.com")
	e.ok(human, "join", invite, "--name", "Alex", "--account")
	m := regexp.MustCompile(`--pass (up_\S+)`).FindStringSubmatch(e.ok(human, "pass"))
	if m == nil {
		t.Fatal("pass printed no pass")
	}
	e.ok(agent, "join", invite, "--pass", m[1], "--name", "Quill", "--emoji", "🪶")
	out = e.ok(human, "agents")
	id := regexp.MustCompile(`(?m)^(\d+)\s+🪶 Quill in Unison \(` + ws.ID + `\) — seen just now, credential expires `).FindStringSubmatch(out)
	if id == nil {
		t.Fatalf("agents =\n%s", out)
	}
	e.ok(agent, "chat", "send", "working")
	if out := e.ok(human, "agents", "revoke", id[1]); !strings.Contains(out, "Revoked 🪶 Quill") {
		t.Errorf("agents revoke = %q", out)
	}
	if _, errOut, code := e.run(agent, "chat", "send", "still here?"); code == 0 || !strings.Contains(errOut, "Alex revoked this agent credential") {
		t.Errorf("revoked agent: exit %d %q", code, errOut)
	}
	if out := e.ok(human, "agents"); !strings.Contains(out, "revoked 20") {
		t.Errorf("agents after revoke = %q", out)
	}

	// Sign out the browser session from the CLI.
	if out := e.ok(human, "sessions", "revoke", "--others"); !strings.Contains(out, "Signed out 1 other session") {
		t.Errorf("sessions revoke --others = %q", out)
	}
	if _, _, err := st.SessionByToken(t.Context(), browser); err == nil {
		t.Error("the browser session survived")
	}
}

// TestDevicesCommands: `devices` lists the account's push devices (without
// their tokens) and `devices remove` removes one of them, not another
// account's.
func TestDevicesCommands(t *testing.T) {
	e := newTestEnv(t)
	dir := t.TempDir()
	if _, errOut, code := e.run(dir, "devices"); code == 0 || !strings.Contains(errOut, "unison login") {
		t.Errorf("devices before login: exit %d %q", code, errOut)
	}
	bob := e.login("Bob", "")
	ada := e.login("Ada", "")
	if out := e.ok(dir, "devices"); !strings.Contains(out, "No devices") {
		t.Errorf("devices before any = %q", out)
	}
	const token = "a0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf"
	_, app, _ := e.st.CreateSession(t.Context(), ada.ID, store.SessionCLI, "iOS")
	d, err := e.st.RegisterDevice(t.Context(), ada.ID, app.ID, token, "Ada's iPhone")
	if err != nil {
		t.Fatal(err)
	}
	_, bobApp, _ := e.st.CreateSession(t.Context(), bob.ID, store.SessionCLI, "iOS")
	bobs, _ := e.st.RegisterDevice(t.Context(), bob.ID, bobApp.ID, strings.Repeat("b", 64), "Bob's iPhone")

	out := e.ok(dir, "devices")
	if !strings.Contains(out, "Ada's iPhone") || !strings.Contains(out, "never notified") || strings.Contains(out, token) || strings.Contains(out, "Bob") {
		t.Errorf("devices =\n%s", out)
	}
	if _, errOut, code := e.run(dir, "devices", "remove", strconv.FormatInt(bobs.ID, 10)); code == 0 || !strings.Contains(errOut, "not found") {
		t.Errorf("removing bob's device: exit %d %q", code, errOut)
	}
	if _, _, code := e.run(dir, "devices", "remove", "x"); code == 0 {
		t.Error("devices remove x succeeded")
	}
	if out := e.ok(dir, "devices", "remove", strconv.FormatInt(d.ID, 10)); !strings.Contains(out, "Removed device") {
		t.Errorf("devices remove = %q", out)
	}
	if out := e.ok(dir, "devices"); !strings.Contains(out, "No devices") {
		t.Errorf("devices after remove = %q", out)
	}
}
