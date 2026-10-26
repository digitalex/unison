package cli

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
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

// testEnv runs a real server and CLI invocations against it, each in its
// own directory (and so with its own .unison.json identity).
type testEnv struct {
	t   *testing.T
	url string
	st  *store.Store
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	st, err := store.Open(storetest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := server.New(st, nil)
	srv.SetAuth(server.AuthConfig{DeviceInterval: time.Second})
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	for _, k := range []string{"UNISON_SERVER", "UNISON_WORKSPACE", "UNISON_TOKEN", "UNISON_CONFIG"} {
		t.Setenv(k, "")
	}
	t.Setenv("UNISON_CREDENTIALS", filepath.Join(t.TempDir(), "credentials.json"))
	return &testEnv{t: t, url: ts.URL, st: st}
}

// login signs the CLI in (`unison login`) as a new account named name, with
// a verified email, and makes it the current credentials (each account
// has its own credentials file, as on separate machines).
func (e *testEnv) login(name, email string) *store.User {
	e.t.Helper()
	e.t.Setenv("UNISON_CREDENTIALS", filepath.Join(e.t.TempDir(), "credentials.json"))
	u, err := e.st.LoginWithProvider(e.t.Context(), "github", store.Profile{Subject: "gh-" + name, Name: name, Email: email, EmailVerified: email != ""})
	if err != nil {
		e.t.Fatal(err)
	}
	browser, _, _ := e.st.CreateSession(e.t.Context(), u.ID, store.SessionBrowser, "test")
	if out := deviceLogin(e.t, e.url, browser); !strings.Contains(out, "Signed in as "+name) {
		e.t.Fatalf("login as %s: %s", name, out)
	}
	return u
}

// passFor mints a pass as the signed-in member in dir and returns it.
func (e *testEnv) passFor(dir string) string {
	e.t.Helper()
	m := regexp.MustCompile(`--pass (up_\S+)`).FindStringSubmatch(e.ok(dir, "pass"))
	if m == nil {
		e.t.Fatal("pass printed no pass")
	}
	return m[1]
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

	e.login("Alex", "")
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(human, "workspace", "create", "Unison", "--repo", "https://github.com/digitalex/unison", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	invite := e.url + "/w/" + ws.ID
	e.ok(human, "join", invite, "--name", "Alex", "--emoji", "🧑", "--account")
	e.ok(agent, "join", invite, "--pass", e.passFor(human), "--name", "Ada", "--emoji", "🦊")
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

	// Repositories: a workspace can have several.
	if out := e.ok(human, "workspace", "repo", "add", "https://github.com/digitalex/unison-web", "--label", "frontend"); !strings.Contains(out, "(frontend)") {
		t.Errorf("repo add = %q", out)
	}
	if _, errOut, code := e.run(human, "workspace", "repo", "add", "https://github.com/digitalex/unison-web"); code == 0 || !strings.Contains(errOut, "already connected") {
		t.Errorf("duplicate repo: exit %d, stderr %q", code, errOut)
	}
	e.ok(agent, "workspace", "repo", "edit", "1", "--label", "backend")
	out = e.ok(agent, "workspace", "repos")
	if !strings.Contains(out, "1   https://github.com/digitalex/unison  (backend)") || !strings.Contains(out, "2   https://github.com/digitalex/unison-web  (frontend)") {
		t.Errorf("repos = %q", out)
	}
	if out := e.ok(agent, "workspace"); !strings.Contains(out, "Repos      1   https://github.com/digitalex/unison  (backend)") || !strings.Contains(out, "unison-web") {
		t.Errorf("workspace show = %q", out)
	}
	if out := e.ok(agent, "--agent-help"); !strings.Contains(out, "| Code repository (backend) | https://github.com/digitalex/unison |") || !strings.Contains(out, "| Code repository (frontend) | https://github.com/digitalex/unison-web |") {
		t.Errorf("agent help lacks the repositories:\n%s", out)
	}
	e.ok(human, "workspace", "repo", "remove", "1")
	var after store.Workspace
	json.Unmarshal([]byte(e.ok(human, "workspace", "--json")), &after)
	if len(after.Repos) != 1 || after.RepoURL != "https://github.com/digitalex/unison-web" {
		t.Errorf("after removing the first repo: %+v", after)
	}
	var multi store.Workspace
	json.Unmarshal([]byte(e.ok(human, "workspace", "create", "Multi", "--repo", "https://github.com/acme/api=backend", "--repo", "https://github.com/acme/web", "--json")), &multi)
	if len(multi.Repos) != 2 || multi.Repos[0].Label != "backend" || multi.Repos[1].Label != "" || multi.RepoURL != "https://github.com/acme/api" {
		t.Errorf("create with two repos = %+v", multi)
	}

	// Webhooks: a delivery wakes `wait --mentions` with the instructions.
	out = e.ok(human, "webhook", "add", "GitHub", "--instructions", "Review new PRs", "--signing-secret", "shh")
	if !strings.Contains(out, "Receive URL") || !strings.Contains(out, "/hooks/wh_") || !strings.Contains(out, "application/json") {
		t.Errorf("webhook add = %q", out)
	}
	hookURL := strings.TrimSpace(e.ok(agent, "webhook", "url", "1"))
	if !strings.Contains(out, hookURL) {
		t.Errorf("webhook url %q is not the one printed by add", hookURL)
	}
	if out := e.ok(agent, "webhook"); strings.Contains(out, "wh_") || !strings.Contains(out, "[enabled, signed]") {
		t.Errorf("webhook list = %q", out)
	}
	post := func(event string, body []byte) {
		mac := hmac.New(sha256.New, []byte("shh"))
		mac.Write(body)
		req, _ := http.NewRequest("POST", hookURL, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitHub-Event", event)
		req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusAccepted {
			done <- fmt.Sprintf("POST %s: %v %v", event, err, resp.Status)
			return
		}
		resp.Body.Close()
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		post("ping", []byte(`{"zen":"Design for failure."}`))
		post("pull_request", []byte(`{"action":"opened","pull_request":{"number":14,"title":"Add search"}}`))
		done <- ""
	}()
	out = e.ok(agent, "wait", "--mentions", "--timeout", "10")
	if errOut := <-done; errOut != "" {
		t.Fatal(errOut)
	}
	want := `🪝 Webhook "GitHub" (#1) received github: pull_request opened #14 "Add search" — delivery 2. Instructions: Review new PRs. Details: unison webhook delivery 2`
	if strings.TrimSpace(out) != want {
		t.Errorf("wait = %q\nwant  %q", out, want)
	}
	if out := e.ok(agent, "webhook", "deliveries", "1"); !strings.Contains(out, "github: ping") || !strings.Contains(out, "pull_request opened #14") {
		t.Errorf("deliveries = %q", out)
	}
	if out := e.ok(agent, "webhook", "delivery", "2"); !strings.Contains(out, "X-GitHub-Event: pull_request") || !strings.Contains(out, `  "action": "opened",`) {
		t.Errorf("delivery = %q", out)
	}
	rotated := strings.TrimSpace(e.ok(human, "webhook", "rotate", "1"))
	if !strings.Contains(rotated, "/hooks/wh_") || strings.Contains(rotated, hookURL) {
		t.Errorf("rotate = %q", rotated)
	}
	if out := e.ok(human, "webhook", "edit", "1", "--disable", "--signing-secret", "none"); !strings.Contains(out, "[disabled]") {
		t.Errorf("webhook edit = %q", out)
	}
	e.ok(human, "webhook", "delete", "1")
	if _, errOut, code := e.run(agent, "webhook", "delivery", "2"); code == 0 || !strings.Contains(errOut, "not found") {
		t.Errorf("delivery after delete: exit %d, stderr %q", code, errOut)
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
