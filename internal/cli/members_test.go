package cli

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/digitalex/unison/internal/server"
	"github.com/digitalex/unison/internal/store"
	"github.com/digitalex/unison/internal/store/storetest"
)

// TestMembership: an old anonymous participant (Alexmobile), then login,
// join --account (and again: the same participant), claim of the old
// participant, members, removal. An agent joining in the same signed-in
// shell stays anonymous.
func TestMembership(t *testing.T) {
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
	legacy, laptop, desktop, agent := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()

	ws, _, _ := st.CreateWorkspace(t.Context(), "Unison", "", "")
	invite := ts.URL + "/w/" + ws.ID
	e.ok(legacy, "join", invite, "--name", "Alexmobile", "--emoji", "📱")
	if _, errOut, code := e.run(legacy, "claim"); code == 0 || !strings.Contains(errOut, "unison login") {
		t.Errorf("claim before login: exit %d %q", code, errOut)
	}

	alex, _ := st.LoginWithProvider(t.Context(), "github", store.Profile{Subject: "1", Name: "Alex"})
	browser, _, _ := st.CreateSession(t.Context(), alex.ID, store.SessionBrowser, "test")
	if out := deviceLogin(t, ts.URL, browser); !strings.Contains(out, "Signed in as Alex") {
		t.Fatalf("login: %s", out)
	}

	if out := e.ok(laptop, "join", invite, "--name", "Alex", "--emoji", "🦊", "--account"); !strings.Contains(out, "linked to your account") {
		t.Errorf("join --account = %q", out)
	}
	if out := e.ok(desktop, "join", invite, "--name", "AlexHome", "--account"); !strings.Contains(out, "Welcome back: you are 🦊 Alex") {
		t.Errorf("second join --account = %q, want the same participant", out)
	}
	if tokenOf(t, desktop) != tokenOf(t, laptop) {
		t.Error("the second join --account did not reuse the participant's token")
	}
	if out := e.ok(laptop, "whoami"); !strings.Contains(out, "Account: Alex (user "+strconv.FormatInt(alex.ID, 10)+")") {
		t.Errorf("whoami = %q", out)
	}
	// An agent in the same shell (credentials present) joins anonymously.
	e.ok(agent, "join", invite, "--name", "Quill", "--emoji", "🦉")
	if out := e.ok(agent, "whoami"); strings.Contains(out, "Account:") {
		t.Errorf("agent got linked to the human's account: %q", out)
	}

	if out := e.ok(legacy, "claim"); !strings.Contains(out, "📱 Alexmobile is now linked to your account") {
		t.Errorf("claim = %q", out)
	}
	out := e.ok(laptop, "members")
	if !strings.Contains(out, "Alex: 🦊 Alex (participant") || !strings.Contains(out, "📱 Alexmobile") || !strings.Contains(out, "Not linked to a member: 🦉 Quill") {
		t.Errorf("members =\n%s", out)
	}
	var m store.Members
	if err := json.Unmarshal([]byte(e.ok(laptop, "members", "--json")), &m); err != nil || len(m.Members) != 1 || len(m.Members[0].Participants) != 2 {
		t.Errorf("members --json = %+v %v", m, err)
	}

	if out := e.ok(laptop, "members", "remove", strconv.FormatInt(alex.ID, 10)); !strings.Contains(out, "Removed user") {
		t.Errorf("members remove = %q", out)
	}
	if out := e.ok(laptop, "members"); !strings.Contains(out, "No members yet") || !strings.Contains(out, "Alexmobile") {
		t.Errorf("members after removal =\n%s", out)
	}
	if _, errOut, code := e.run(laptop, "members", "remove", "x"); code == 0 || !strings.Contains(errOut, "number") {
		t.Errorf("members remove x: exit %d %q", code, errOut)
	}
}
