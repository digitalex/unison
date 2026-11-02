package cli

import (
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
