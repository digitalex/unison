package cli

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

func TestCLIMeDefaults(t *testing.T) {
	e := newTestEnv(t)
	human, agent := t.TempDir(), t.TempDir()

	e.login("Alex", "")

	// Default is empty.
	if out := e.ok(human, "me", "defaults"); !strings.Contains(out, "no integrations") {
		t.Fatalf("initial me defaults = %q", out)
	}

	// Set integrations (ordered & deduplicated).
	out := e.ok(human, "me", "defaults", "set", "--integrations", "github,isocan,github")
	if !strings.Contains(out, "isocan, github") {
		t.Fatalf("after set = %q", out)
	}

	// JSON read.
	var got store.UserSettings
	if err := json.Unmarshal([]byte(e.ok(human, "me", "defaults", "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.NewProjectIntegrations, []string{"isocan", "github"}) {
		t.Fatalf("json defaults = %+v", got)
	}

	// Clear with "".
	if out := e.ok(human, "me", "defaults", "set", "--integrations", ""); !strings.Contains(out, "no integrations") {
		t.Fatalf("after clear = %q", out)
	}

	// Agent session is refused.
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(human, "workspace", "create", "Unison", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	invite := e.url + "/w/" + ws.ID
	e.ok(human, "join", invite, "--name", "Alex", "--emoji", "🧑", "--account")
	e.ok(agent, "join", invite, "--pass", e.passFor(human), "--name", "Ada", "--emoji", "🦊")

	if _, errOut, code := e.run(agent, "me", "defaults"); code == 0 || !strings.Contains(errOut, "agents can't") {
		t.Fatalf("agent me defaults: code=%d, err=%q", code, errOut)
	}
}
