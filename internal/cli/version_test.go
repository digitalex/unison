package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestVersionCLI(t *testing.T) {
	e := newTestEnv(t)
	dir := t.TempDir()

	old := Version
	Version = "v1.2.3"
	defer func() { Version = old }()

	for _, cmd := range []string{"version", "--version"} {
		if out := e.ok(dir, cmd); strings.TrimSpace(out) != "unison v1.2.3" {
			t.Fatalf("%s = %q", cmd, out)
		}
	}
	var got struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(e.ok(dir, "version", "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != "v1.2.3" {
		t.Fatalf("json version = %q", got.Version)
	}

	Version = ""
	if v := buildVersion(); v == "" {
		t.Fatal("unstamped build has an empty version")
	}
}

func TestIsVersionOlder(t *testing.T) {
	cases := []struct {
		client, server string
		want           bool
	}{
		{"v1.0.0", "v1.0.1", true},
		{"v1.0.1", "v1.0.0", false},
		{"v1.0.0", "v1.0.0", false},
		{"v0.0.0-20261110004557-9a2215b61fd0", "v0.0.0-20261115152810-95f808b", true},
		{"v0.0.0-20261115152810-95f808b", "v0.0.0-20261110004557-9a2215b61fd0", false},
		{"v0.0.0-20261115152810-95f808b", "v0.1.0", true},
		{"dev", "v1.0.0", false},
		{"dev-abcdef123456", "v1.0.0", false},
		{"v1.0.0", "dev", false},
		{"v1.0.0", "dev-abcdef123456", false},
	}
	for _, tc := range cases {
		if got := isVersionOlder(tc.client, tc.server); got != tc.want {
			t.Errorf("isVersionOlder(%q, %q) = %v, want %v", tc.client, tc.server, got, tc.want)
		}
	}
}

func TestVersionMismatchWarningAndWait(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	e := newTestEnv(t)
	e.srv.SetVersion("v1.5.0")

	old := Version
	Version = "v1.4.0"
	defer func() { Version = old }()

	dir := t.TempDir()
	// unison version shows both CLI and server versions when --server is passed.
	out := e.ok(dir, "version", "--server", e.ts.URL)
	if !strings.Contains(out, "unison v1.4.0") || !strings.Contains(out, "server v1.5.0") {
		t.Fatalf("version output = %q", out)
	}

	// Normal API command warns once per day on stderr.
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"workspace", "stats", "--server", e.ts.URL}, &stdout, &stderr); code != 0 {
		t.Fatalf("stats 1 failed: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "older than the server (v1.5.0)") {
		t.Fatalf("expected outdated warning on stderr, got %q", stderr.String())
	}

	// Second normal command on the same day stays quiet.
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"workspace", "stats", "--server", e.ts.URL}, &stdout, &stderr); code != 0 {
		t.Fatalf("stats 2 failed: %s", stderr.String())
	}
	if stderr.String() != "" {
		t.Fatalf("expected no repeat warning on same day, got %q", stderr.String())
	}

	// unison wait warns at the start even if already warned today.
	e.login("Alex", "alex@example.com")
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(e.ok(dir, "workspace", "create", "test-ws", "--server", e.ts.URL, "--json")), &created); err != nil {
		t.Fatal(err)
	}
	var passOut struct {
		Pass string `json:"pass"`
	}
	if err := json.Unmarshal([]byte(e.ok(dir, "pass", "--server", e.ts.URL, "--workspace", created.ID, "--json")), &passOut); err != nil {
		t.Fatal(err)
	}
	e.ok(dir, "join", e.ts.URL+"/w/"+created.ID, "--pass", passOut.Pass, "--name", "Bot")

	stdout.Reset()
	stderr.Reset()
	_ = Run([]string{"--config", dir + "/.unison.json", "wait", "--timeout", "0"}, &stdout, &stderr)
	if !strings.Contains(stderr.String(), "older than the server (v1.5.0)") {
		t.Fatalf("expected wait to warn at start, got %q", stderr.String())
	}
}
