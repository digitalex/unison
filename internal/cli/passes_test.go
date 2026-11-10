package cli

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/digitalex/unison/internal/server"
	"github.com/digitalex/unison/internal/store"
	"github.com/digitalex/unison/internal/store/storetest"
)

// TestAgentPass: a signed-in member mints a pass, an agent joins with it,
// works as "agent of" the member, and stops (with a clear message) when the
// member is removed.
func TestAgentPass(t *testing.T) {
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

	owner, _ := st.LoginWithProvider(t.Context(), "github", store.Profile{Subject: "owner", Name: "Owner"})
	ws, _, _ := st.CreateWorkspaceFor(t.Context(), owner.ID, "Unison", "", "")
	invite := ts.URL + "/w/" + ws.ID
	_, link, _, _ := st.CreateLinkInvite(t.Context(), ws.ID, owner.ID)
	alex, _ := st.LoginWithProvider(t.Context(), "github", store.Profile{Subject: "1", Name: "Alex"})
	browser, _, _ := st.CreateSession(t.Context(), alex.ID, store.SessionBrowser, "test")
	if _, errOut, code := e.run(human, "--workspace", ws.ID, "pass"); code == 0 || !strings.Contains(errOut, "unison login") {
		t.Errorf("pass before login: exit %d %q", code, errOut)
	}
	deviceLogin(t, ts.URL, browser)
	if _, errOut, code := e.run(human, "--workspace", ws.ID, "pass", "--agent"); code == 0 || !strings.Contains(errOut, "not a member") {
		t.Errorf("pass before joining: exit %d %q", code, errOut)
	}
	// Alex joins with the owner's invite link (implies --account).
	e.ok(human, "join", invite+"/join?invite="+link, "--name", "Alex")

	out := e.ok(human, "pass", "--agent")
	m := regexp.MustCompile(`unison join (\S+) --pass (up_\S+)`).FindStringSubmatch(out)
	if m == nil || m[1] != invite || !strings.Contains(out, "agent of Alex") {
		t.Fatalf("pass output:\n%s", out)
	}
	if _, errOut, code := e.run(agent, "join", invite, "--pass", m[2], "--account"); code == 0 || !strings.Contains(errOut, "use one") {
		t.Errorf("--pass with --account: exit %d %q", code, errOut)
	}
	if out := e.ok(agent, "join", m[1], "--pass", m[2], "--name", "Quill", "--emoji", "🪶"); !strings.Contains(out, "agent of Alex") || !strings.Contains(out, "24 hours") {
		t.Errorf("join --pass = %q", out)
	}
	if out := e.ok(agent, "whoami"); !strings.Contains(out, "Agent of Alex; credential expires 20") || !strings.Contains(out, "24h without activity") {
		t.Errorf("agent whoami = %q", out)
	}
	if _, errOut, code := e.run(t.TempDir(), "join", invite, "--pass", m[2]); code == 0 || !strings.Contains(errOut, "already used") {
		t.Errorf("reused pass: exit %d %q", code, errOut)
	}
	e.ok(agent, "chat", "send", "Hello from Quill")
	if out := e.ok(human, "who"); !strings.Contains(out, "🪶 Quill · agent of Alex") || !strings.Contains(out, "credential expires ") {
		t.Errorf("who = %q", out)
	}

	// Workspace deletion is for the owner only (#296): not from the agent's
	// session, even with Alex signed in on this machine, nor with the
	// agent's token passed explicitly; nor by Alex, a member but not owner.
	var agentCfg Config
	raw, _ := os.ReadFile(filepath.Join(agent, ConfigFile))
	if err := json.Unmarshal(raw, &agentCfg); err != nil || !agentCfg.Agent {
		t.Errorf("agent session = %s (%v), want agent: true", raw, err)
	}
	if _, errOut, code := e.run(agent, "workspace", "delete", "--confirm", "Unison"); code == 0 || !strings.Contains(errOut, "agents can't delete") {
		t.Errorf("delete from the agent's session: exit %d %q", code, errOut)
	}
	if _, errOut, code := e.run(t.TempDir(), "--workspace", ws.ID, "--token", agentCfg.Token, "workspace", "delete", "--confirm", "Unison"); code == 0 || !strings.Contains(errOut, "agents can't delete") {
		t.Errorf("delete with the agent's --token: exit %d %q", code, errOut)
	}
	t.Setenv("UNISON_TOKEN", agentCfg.Token)
	if _, errOut, code := e.run(t.TempDir(), "--workspace", ws.ID, "workspace", "delete", "--confirm", "Unison"); code == 0 || !strings.Contains(errOut, "agents can't delete") {
		t.Errorf("delete with the agent's UNISON_TOKEN: exit %d %q", code, errOut)
	}
	t.Setenv("UNISON_TOKEN", "")
	if _, errOut, code := e.run(human, "workspace", "delete", "--confirm", "Unison"); code == 0 || !strings.Contains(errOut, "only the workspace's owner") {
		t.Errorf("delete by a member who isn't the owner: exit %d %q", code, errOut)
	}

	// Alex leaves the workspace's members: Quill stops working.
	bob, _ := st.LoginWithProvider(t.Context(), "github", store.Profile{Subject: "2", Name: "Bob"})
	st.JoinAsUser(t.Context(), ws.ID, bob.ID, "Bob", "")
	if _, err := st.RemoveMember(t.Context(), ws.ID, owner.ID, alex.ID); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code := e.run(agent, "chat", "send", "anyone?"); code == 0 || !strings.Contains(errOut, "Alex, who invited this agent, is no longer a member") {
		t.Errorf("agent after its sponsor left: exit %d %q", code, errOut)
	}
	// So does Alex's own participant: workspaces are private to members.
	if _, errOut, code := e.run(human, "chat", "send", "still here"); code == 0 || !strings.Contains(errOut, "no longer a member") {
		t.Errorf("removed member: exit %d %q", code, errOut)
	}

	// The owner deletes it.
	ownerBrowser, _, _ := st.CreateSession(t.Context(), owner.ID, store.SessionBrowser, "test")
	deviceLogin(t, ts.URL, ownerBrowser)
	if out := e.ok(t.TempDir(), "workspace", "delete", ws.ID, "--confirm", "Unison"); !strings.Contains(out, "Deleted workspace Unison") {
		t.Errorf("owner delete = %q", out)
	}
}
