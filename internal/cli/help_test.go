package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

func runOfflineCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	for _, k := range []string{"UNISON_SERVER", "UNISON_WORKSPACE", "UNISON_TOKEN", "UNISON_CONFIG"} {
		t.Setenv(k, "")
	}
	t.Setenv("UNISON_CREDENTIALS", dir+"/credentials.json")
	t.Setenv("XDG_CONFIG_HOME", dir+"/xdg")
	var out, errOut bytes.Buffer
	code := Run(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

func TestHelpCommandAndSubcommandFlags(t *testing.T) {
	// Top-level help forms.
	for _, args := range [][]string{
		{},
		{"help"},
		{"--help"},
		{"-h"},
	} {
		out, errOut, code := runOfflineCLI(t, args...)
		if code != 0 || errOut != "" {
			t.Fatalf("unison %v: exit %d, stderr %q", args, code, errOut)
		}
		for _, want := range []string{
			"Workspaces & identity:",
			"Backlog (",
			"Feedback (",
			"feedback delete|rm <id>",
			"artifact update <name> [<path>] [--title T] [--item ID|none]",
			"@Name, @all, @everyone",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("unison %v missing %q in:\n%s", args, want, out)
			}
		}
	}

	// Targeted `unison help <cmd> [<subcmd>]` and `<cmd> [--help|-h|help]`.
	cases := []struct {
		args      []string
		wantInOut []string
		forbidOut []string
	}{
		{
			args:      []string{"help", "backlog"},
			wantInOut: []string{"Backlog (", "backlog [list]", "backlog claim", "backlog comment <id> --delete CID [--legacy]"},
			forbidOut: []string{"Feedback (", "Webhooks ("},
		},
		{
			args:      []string{"help", "feedback"},
			wantInOut: []string{"Feedback (", "feedback [ls]", "feedback add <title>", "feedback delete|rm <id>"},
			forbidOut: []string{"Backlog (", "Recurring tasks ("},
		},
		{
			args:      []string{"help", "feedback", "add"},
			wantInOut: []string{"feedback add <title>", "--severity low|medium|high|critical"},
			forbidOut: []string{"feedback dismiss"},
		},
		{
			args:      []string{"help", "feedback", "delete"},
			wantInOut: []string{"feedback delete|rm <id>"},
		},
		{
			args:      []string{"help", "feedback", "rm"},
			wantInOut: []string{"feedback delete|rm <id>"},
		},
		{
			args:      []string{"backlog", "--help"},
			wantInOut: []string{"Backlog (", "backlog [list]", "backlog claim"},
			forbidOut: []string{"Flags:"},
		},
		{
			args:      []string{"backlog", "-h"},
			wantInOut: []string{"Backlog (", "backlog claim"},
		},
		{
			args:      []string{"backlog", "help"},
			wantInOut: []string{"Backlog (", "backlog claim"},
		},
		{
			args:      []string{"backlog", "help", "list"},
			wantInOut: []string{"backlog [list]", "--sort priority|newest|oldest|updated|title"},
			forbidOut: []string{"backlog delete"},
		},
		{
			args:      []string{"backlog", "list", "--help"},
			wantInOut: []string{"backlog [list]", "Flags:", "-status", "-section", "-sort"},
		},
		{
			args:      []string{"feedback", "add", "--help"},
			wantInOut: []string{"feedback add <title>", "Flags:", "-severity", "-type", "-image", "-page"},
		},
		{
			args: []string{"wait", "--help"},
			wantInOut: []string{
				"wait [--mentions] [--timeout SECONDS]",
				"@Name, @all, @everyone",
				"replies to your",
				"--action",
				"ideas you added",
				"--notify",
				"unblocked main",
				"webhook deliveries",
				"Flags:",
				"-mentions",
				"-timeout",
			},
		},
		{
			args:      []string{"artifact", "update", "--help"},
			wantInOut: []string{"artifact update <name> [<path>] [--title T] [--item ID|none]", "<path> - reads", "--item none unlinks", "Flags:", "-item", "-title"},
		},
		{
			args:      []string{"backlog", "claim", "--help"},
			wantInOut: []string{"backlog claim", "atomically assign & start"},
			forbidOut: []string{"Flags:"},
		},
		{
			args:      []string{"workspace", "--help"},
			wantInOut: []string{"workspace [show]", "workspace set", "workspace repo add"},
		},
		{
			args:      []string{"workspace", "repo", "--help"},
			wantInOut: []string{"workspace repo add", "workspace repo edit", "workspace repo remove"},
		},
		{
			args:      []string{"workspace", "repo", "edit", "--help"},
			wantInOut: []string{"workspace repo edit <id> --url U", "Flags:", "-url"},
		},
		{
			args:      []string{"workspace", "link", "--help"},
			wantInOut: []string{"workspace link add", "workspace link edit", "workspace link remove"},
		},
		{
			args:      []string{"chat", "--help"},
			wantInOut: []string{"chat log", "chat send", "chat read", "chat react"},
		},
		{
			args:      []string{"chat", "read", "--help"},
			wantInOut: []string{"chat read [<message-id>]", "Flags:", "-show"},
		},
		{
			args:      []string{"chat", "react", "--help"},
			wantInOut: []string{"chat react <message-id> <emoji>"},
		},
		{
			args:      []string{"todo", "--help"},
			wantInOut: []string{"TODOs (", "todo [ls]", "todo add", "todo snooze"},
		},
		{
			args:      []string{"todo", "snooze", "--help"},
			wantInOut: []string{"todo snooze <id> (1d | 1w | off)"},
		},
		{
			args:      []string{"section", "--help"},
			wantInOut: []string{"Sections (", "section add", "section move"},
		},
		{
			args:      []string{"idea", "--help"},
			wantInOut: []string{"Ideas (", "idea promote"},
		},
		{
			args:      []string{"schedule", "--help"},
			wantInOut: []string{"Recurring tasks (", "schedule add"},
		},
		{
			args:      []string{"schedule", "add", "--help"},
			wantInOut: []string{"schedule add <title>", "Flags:", "-hourly", "-daily", "-weekly"},
			forbidOut: []string{"-title"},
		},
		{
			args:      []string{"webhook", "--help"},
			wantInOut: []string{"Webhooks (", "webhook deliveries"},
		},
		{
			args:      []string{"webhook", "deliveries", "--help"},
			wantInOut: []string{"webhook deliveries <id> [--limit N]", "Flags:", "-limit"},
		},
		{
			args:      []string{"invite", "--help"},
			wantInOut: []string{"invite email", "invite link", "invites", "invite revoke"},
		},
		{
			args:      []string{"invite", "email", "--help"},
			wantInOut: []string{"invite email <address>"},
		},
		{
			args:      []string{"members", "--help"},
			wantInOut: []string{"members", "members remove <user-id>"},
		},
		{
			args:      []string{"sessions", "revoke", "--help"},
			wantInOut: []string{"sessions revoke <id>|--others", "Flags:", "-others"},
		},
		{
			args:      []string{"agents", "--help"},
			wantInOut: []string{"agents", "agents revoke"},
		},
		{
			args:      []string{"devices", "--help"},
			wantInOut: []string{"devices", "devices remove"},
		},
		{
			args:      []string{"image", "--help"},
			wantInOut: []string{"Images (", "image upload", "image save"},
		},
		{
			args:      []string{"whoami", "--help"},
			wantInOut: []string{"whoami", "whoami --user", "Flags:", "-user"},
		},
		{
			args:      []string{"reviews", "--help"},
			wantInOut: []string{"reviews", "open PRs"},
		},
		{
			args:      []string{"version", "--help"},
			wantInOut: []string{"version", "this binary's version"},
		},
		{
			args:      []string{"peek", "--help"},
			wantInOut: []string{"peek [--mentions]", "Flags:", "-mentions"},
		},
	}

	for _, tc := range cases {
		out, errOut, code := runOfflineCLI(t, tc.args...)
		if code != 0 || errOut != "" {
			t.Errorf("unison %v: exit %d, stderr %q; stdout:\n%s", tc.args, code, errOut, out)
			continue
		}
		for _, want := range tc.wantInOut {
			if !strings.Contains(out, want) {
				t.Errorf("unison %v missing %q in:\n%s", tc.args, want, out)
			}
		}
		for _, forbid := range tc.forbidOut {
			if strings.Contains(out, forbid) {
				t.Errorf("unison %v unexpectedly contained %q in:\n%s", tc.args, forbid, out)
			}
		}
	}

	// Unknown help topics fail with errUsage (exit 1).
	for _, bad := range [][]string{
		{"help", "nosuchcommand"},
		{"help", "backlog", "nosuchsub"},
		{"backlog", "help", "nosuchsub"},
		{"workspace", "help", "repo", "nosuch"},
	} {
		out, errOut, code := runOfflineCLI(t, bad...)
		if code != 1 || out != "" || !strings.Contains(errOut, "usage") || !strings.Contains(errOut, "unknown") {
			t.Errorf("unison %v = exit %d, stdout %q, stderr %q; want exit 1 usage error", bad, code, out, errOut)
		}
	}
}

func expandEntryPaths(cmd string, subs [][]string) [][]string {
	paths := [][]string{{cmd}}
	for _, group := range subs {
		var next [][]string
		for _, prefix := range paths {
			for _, alt := range group {
				next = append(next, append(append([]string{}, prefix...), alt))
			}
		}
		paths = next
	}
	return paths
}

func TestEveryUsageEntrySupportsHelp(t *testing.T) {
	seen := map[string]bool{}
	for _, sec := range parseUsageSections() {
		for _, e := range sec.entries {
			for _, path := range expandEntryPaths(e.cmd, e.subs) {
				key := strings.Join(path, " ")
				if seen[key] {
					continue
				}
				seen[key] = true
				for _, inv := range [][]string{
					append([]string{"help"}, path...),
					append(append([]string{}, path...), "--help"),
					append(append([]string{}, path...), "-h"),
				} {
					out, errOut, code := runOfflineCLI(t, inv...)
					if code != 0 || errOut != "" || !strings.Contains(out, e.cmd) {
						t.Errorf("unison %v = exit %d, stderr %q, stdout %q", inv, code, errOut, out)
					}
				}
			}
		}
	}
}

func TestDoubleDashPreservesLiteralHelpAndGuideDocs(t *testing.T) {
	e := newTestEnv(t)
	alexDir := t.TempDir()

	e.login("Alex", "")
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(alexDir, "workspace", "create", "Docs", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	e.ok(alexDir, "join", e.url+"/w/"+ws.ID, "--name", "Alex", "--emoji", "🧑", "--account")

	// `unison chat send -- --help` and multi-word `unison chat send -- check --help` post literal text.
	send1 := e.ok(alexDir, "chat", "send", "--", "--help")
	if !strings.Contains(send1, "Sent message #1.") {
		t.Fatalf("chat send -- --help = %q", send1)
	}
	send2 := e.ok(alexDir, "chat", "send", "--", "run", "--help")
	if !strings.Contains(send2, "Sent message #2.") {
		t.Fatalf("chat send -- run --help = %q", send2)
	}
	var msgs []store.Message
	if err := json.Unmarshal([]byte(e.ok(alexDir, "chat", "log", "--json")), &msgs); err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].Body != "--help" || msgs[1].Body != "run --help" {
		t.Fatalf("chat messages = %+v; want literal --help bodies", msgs)
	}

	// `unison status -- --help` sets literal status "--help".
	statusOut := e.ok(alexDir, "status", "--", "--help")
	if !strings.Contains(statusOut, "🧑 Alex: --help") {
		t.Fatalf("status -- --help = %q", statusOut)
	}

	// Both `feedback delete` and `feedback rm` work.
	e.ok(alexDir, "feedback", "add", "First report")
	e.ok(alexDir, "feedback", "add", "Second report")
	if out := e.ok(alexDir, "feedback", "delete", "1"); !strings.Contains(out, "Deleted feedback 1.") {
		t.Errorf("feedback delete = %q", out)
	}
	if out := e.ok(alexDir, "feedback", "rm", "2"); !strings.Contains(out, "Deleted feedback 2.") {
		t.Errorf("feedback rm = %q", out)
	}

	// Verify agent guide (--agent-help) includes updated #292 documentation.
	guide := e.ok(alexDir, "--agent-help")
	for _, want := range []string{
		"@everyone",
		"backlog.answered",
		"idea.commented",
		"todo.created",
		"backlog.resumed",
		"`?assignee=` accepts a participant `Name`, numeric `ID`, or `me`",
		"`unison feedback delete\\|rm <id>`",
		"`unison artifact update <name> [<path>] [--title T] [--item ID\\|none]`",
		"`<path>` `-` reads stdin",
		"`--item none`",
	} {
		if !strings.Contains(guide, want) {
			t.Errorf("--agent-help missing %q", want)
		}
	}
}
