package cli

import (
	"bytes"
	"encoding/json"
	"io"
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

func TestLoginLogout(t *testing.T) {
	st, err := store.Open(storetest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := server.New(st, nil)
	srv.SetAuth(server.AuthConfig{DeviceInterval: time.Second, Creators: []string{"ada@example.com"}})
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	e := &testEnv{t: t, url: ts.URL}
	for _, k := range []string{"UNISON_SERVER", "UNISON_WORKSPACE", "UNISON_TOKEN", "UNISON_CONFIG"} {
		t.Setenv(k, "")
	}
	creds := filepath.Join(t.TempDir(), "unison", "credentials.json")
	t.Setenv("UNISON_CREDENTIALS", creds)
	dir := t.TempDir()

	// The person approving in the browser, signed in on the web.
	ada, _ := st.LoginWithProvider(t.Context(), "github", store.Profile{Subject: "1", Name: "Ada", Email: "ada@example.com", EmailVerified: true})
	browser, _, _ := st.CreateSession(t.Context(), ada.ID, store.SessionBrowser, "test")

	if _, errOut, code := e.run(dir, "workspace", "create", "Locked"); code == 0 || !strings.Contains(errOut, "not signed in") {
		t.Errorf("create before login: exit %d %q, want refused", code, errOut)
	}

	// Approve whatever code `unison login` prints, the way the device page does.
	r, w, _ := os.Pipe()
	done := make(chan string, 1)
	go func() {
		var out, errOut bytes.Buffer
		Run([]string{"--server", ts.URL, "login"}, io.MultiWriter(&out, w), &errOut)
		w.Close()
		done <- out.String() + errOut.String()
	}()
	code := waitForCode(t, r)
	req, _ := http.NewRequest("POST", ts.URL+"/api/auth/device/approve", strings.NewReader(`{"user_code":"`+code+`","approve":true}`))
	req.AddCookie(&http.Cookie{Name: "unison_session", Value: browser})
	req.Header.Set("X-Unison-CSRF", "1")
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 200 {
		t.Fatalf("approve: %v %v", resp, err)
	}
	out := <-done
	if !strings.Contains(out, "Signed in as Ada <ada@example.com>") {
		t.Fatalf("login output:\n%s", out)
	}
	info, err := os.Stat(creds)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("credentials file: %v %v", info, err)
	}
	if dirInfo, _ := os.Stat(filepath.Dir(creds)); dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("credentials dir mode = %v", dirInfo.Mode().Perm())
	}
	var saved credentials
	raw, _ := os.ReadFile(creds)
	json.Unmarshal(raw, &saved)
	token := saved.Servers[serverKey(ts.URL)].Token
	if !strings.HasPrefix(token, store.UserTokenPrefix) {
		t.Fatalf("saved credentials = %s", raw)
	}

	if out := e.ok(dir, "whoami", "--user"); !strings.Contains(out, "Ada (user") || !strings.Contains(out, "github  ada@example.com") {
		t.Errorf("whoami --user = %q", out)
	}
	// The account (on the creators list) may create workspaces; joining and
	// workspace commands are unchanged.
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(dir, "workspace", "create", "Mine", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	e.ok(dir, "join", ts.URL+"/w/"+ws.ID, "--name", "Ada")
	if out := e.ok(dir, "whoami"); !strings.Contains(out, "Ada (participant") {
		t.Errorf("whoami = %q", out)
	}

	e.ok(dir, "logout")
	if _, _, err := st.SessionByToken(t.Context(), token); err == nil {
		t.Error("logout did not revoke the session")
	}
	if _, errOut, code := e.run(dir, "whoami", "--user"); code == 0 || !strings.Contains(errOut, "unison login") {
		t.Errorf("whoami --user after logout: exit %d %q", code, errOut)
	}
	if _, errOut, code := e.run(dir, "logout"); code == 0 || !strings.Contains(errOut, "not signed in") {
		t.Errorf("second logout: exit %d %q", code, errOut)
	}
}

func waitForCode(t *testing.T, r *os.File) string {
	t.Helper()
	re := regexp.MustCompile(`confirm the code ([A-Z]{4}-[A-Z]{4})`)
	var buf []byte
	chunk := make([]byte, 512)
	for {
		n, err := r.Read(chunk)
		buf = append(buf, chunk[:n]...)
		if m := re.FindSubmatch(buf); m != nil {
			go func() { // Keep draining so the CLI never blocks on the pipe.
				for {
					if _, err := r.Read(chunk); err != nil {
						return
					}
				}
			}()
			return string(m[1])
		}
		if err != nil {
			t.Fatalf("login printed no code: %q", buf)
		}
	}
}

func TestServerKey(t *testing.T) {
	for in, want := range map[string]string{
		"https://Unison.Example.com/":      "https://unison.example.com",
		"https://unison.example.com:443/w": "https://unison.example.com",
		"http://localhost:4600":            "http://localhost:4600",
		"http://localhost:80":              "http://localhost",
	} {
		if got := serverKey(in); got != want {
			t.Errorf("serverKey(%q) = %q, want %q", in, got, want)
		}
	}
}
