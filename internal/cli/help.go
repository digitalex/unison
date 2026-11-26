package cli

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
)

// helpError signals that the caller asked for command or subcommand help
// (via `unison help ...`, `-h`, or `--help`) and exits with code 0 when valid.
type helpError struct {
	topic string
	fs    *flag.FlagSet
}

func (e *helpError) Error() string {
	if e.topic == "" {
		return "help requested"
	}
	return "help requested: " + e.topic
}

func isHelpFlag(s string) bool {
	return s == "-h" || s == "--help" || s == "-help"
}

// dispatcherHelp handles `<cmd> -h|--help` and `<cmd> help [<sub...>]` at the
// top of subcommand dispatchers.
func dispatcherHelp(cmd string, args []string) error {
	if len(args) == 0 {
		return nil
	}
	if isHelpFlag(args[0]) {
		return &helpError{topic: cmd}
	}
	if args[0] == "help" {
		return helpTopicError(append([]string{cmd}, args[1:]...))
	}
	return nil
}

// positionalArgs checks for -h/--help before "--" on subcommands that take no
// flags, and strips a "--" separator if present.
func positionalArgs(topic string, args []string) ([]string, error) {
	var out []string
	for i, arg := range args {
		if arg == "--" {
			return append(out, args[i+1:]...), nil
		}
		if isHelpFlag(arg) {
			return nil, &helpError{topic: topic}
		}
		out = append(out, arg)
	}
	return out, nil
}

// helpTopicError validates and constructs a *helpError for `unison help <args...>`.
func helpTopicError(args []string) error {
	var filtered []string
	for _, a := range args {
		if isHelpFlag(a) {
			continue
		}
		filtered = append(filtered, a)
	}
	if len(filtered) == 0 || (len(filtered) == 1 && filtered[0] == "help") {
		return &helpError{}
	}
	if len(filtered) == 1 && (filtered[0] == "--agent-help" || filtered[0] == "agent-help") {
		return &helpError{topic: "agent-help"}
	}
	topic := strings.Join(filtered, " ")
	if _, err := lookupHelp(topic); err != nil {
		return err
	}
	return &helpError{topic: topic}
}

func writeHelp(w io.Writer, he *helpError) error {
	if he.topic == "" {
		_, err := fmt.Fprint(w, usage)
		return err
	}
	text, err := lookupHelp(he.topic)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, text); err != nil {
		return err
	}
	if he.fs != nil {
		var flagBuf bytes.Buffer
		he.fs.SetOutput(&flagBuf)
		he.fs.PrintDefaults()
		if flagBuf.Len() > 0 {
			if _, err := fmt.Fprintf(w, "\nFlags:\n%s", flagBuf.String()); err != nil {
				return err
			}
		}
	}
	return nil
}

type usageSection struct {
	header  []string
	entries []usageEntry
}

type usageEntry struct {
	cmd   string
	subs  [][]string
	lines []string
}

func parseUsageSections() []usageSection {
	var sections []usageSection
	var cur *usageSection
	var headerBuf []string

	for _, line := range strings.Split(usage, "\n") {
		if strings.HasPrefix(line, "Exit codes:") {
			break
		}
		if line == "" {
			headerBuf = nil
			continue
		}
		if line[0] != ' ' {
			if strings.HasPrefix(line, "unison —") || strings.HasPrefix(line, "Agents:") {
				continue
			}
			headerBuf = append(headerBuf, line)
			if strings.HasSuffix(line, ":") {
				sections = append(sections, usageSection{header: slices.Clone(headerBuf)})
				cur = &sections[len(sections)-1]
				headerBuf = nil
			}
			continue
		}
		if cur == nil {
			continue
		}
		if strings.HasPrefix(line, "  ") && len(line) > 2 && line[2] != ' ' {
			cmd, subs := parseEntrySynopsis(line)
			cur.entries = append(cur.entries, usageEntry{
				cmd:   cmd,
				subs:  subs,
				lines: []string{line},
			})
			continue
		}
		if strings.HasPrefix(line, "   ") && len(cur.entries) > 0 {
			last := &cur.entries[len(cur.entries)-1]
			last.lines = append(last.lines, line)
		}
	}
	return sections
}

func parseEntrySynopsis(line string) (string, [][]string) {
	syn, _, _ := strings.Cut(line[2:], "  ")
	syn = strings.ReplaceAll(syn, " | ", "|")
	words := strings.Fields(syn)
	if len(words) == 0 {
		return "", nil
	}
	cmd := words[0]
	var subs [][]string
	for idx, w := range words[1:] {
		if strings.HasPrefix(w, "<") || strings.HasPrefix(w, "-") || strings.HasPrefix(w, "(") {
			break
		}
		if strings.HasPrefix(w, "[") {
			if idx == 0 && (w == "[show]" || w == "[ls]" || w == "[list]") {
				subs = append(subs, []string{w[1 : len(w)-1]})
			}
			break
		}
		subs = append(subs, strings.Split(w, "|"))
	}
	return cmd, subs
}

func normalizeHelpTokens(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	tokens := slices.Clone(raw)
	switch tokens[0] {
	case "ws":
		tokens[0] = "workspace"
	case "todos":
		tokens[0] = "todo"
	case "artifacts":
		tokens[0] = "artifact"
	case "sections":
		tokens[0] = "section"
	case "ideas":
		tokens[0] = "idea"
	case "webhooks":
		tokens[0] = "webhook"
	case "schedules":
		tokens[0] = "schedule"
	case "images":
		tokens[0] = "image"
	case "member":
		tokens[0] = "members"
	case "device":
		tokens[0] = "devices"
	case "--version":
		tokens[0] = "version"
	}
	if len(tokens) < 2 {
		return tokens
	}
	cmd, sub := tokens[0], tokens[1]
	switch cmd {
	case "workspace":
		if sub == "rm" {
			tokens[1] = "delete"
		} else if (sub == "repo" || sub == "link") && len(tokens) >= 3 && tokens[2] == "rm" {
			tokens[2] = "remove"
		}
	case "backlog", "idea", "webhook":
		switch sub {
		case "ls":
			tokens[1] = "list"
		case "rm":
			tokens[1] = "delete"
		}
	case "schedule":
		switch sub {
		case "ls":
			tokens[1] = "list"
		case "rm":
			tokens[1] = "remove"
		}
	case "artifact":
		switch sub {
		case "list":
			tokens[1] = "ls"
		case "upload":
			tokens[1] = "add"
		case "replace":
			tokens[1] = "update"
		case "rm":
			tokens[1] = "delete"
		}
	case "todo":
		switch sub {
		case "list":
			tokens[1] = "ls"
		case "rm":
			tokens[1] = "delete"
		}
	case "section":
		switch sub {
		case "list":
			tokens[1] = "ls"
		case "rm", "remove":
			tokens[1] = "delete"
		}
	case "feedback":
		if sub == "list" {
			tokens[1] = "ls"
		}
	case "members":
		if sub == "ls" {
			tokens[1] = "list"
		} else if sub == "rm" {
			tokens[1] = "remove"
		}
	case "devices":
		if sub == "rm" {
			tokens[1] = "remove"
		}
	case "invite":
		if sub == "list" || sub == "ls" {
			return []string{"invites"}
		}
	}
	return tokens
}

func entryMatches(e usageEntry, tokens []string) bool {
	cmd := tokens[0]
	if len(tokens) == 1 {
		if cmd == "invite" {
			return e.cmd == "invite" || e.cmd == "invites"
		}
		return e.cmd == cmd
	}
	if e.cmd != cmd {
		return false
	}
	wantSubs := tokens[1:]
	if len(wantSubs) == 1 && wantSubs[0] == "list" && len(e.subs) == 0 &&
		(cmd == "members" || cmd == "sessions" || cmd == "agents" || cmd == "devices") {
		return true
	}
	if len(e.subs) < len(wantSubs) {
		return false
	}
	for i, want := range wantSubs {
		if !slices.Contains(e.subs[i], want) {
			return false
		}
	}
	return true
}

func lookupHelp(topic string) (string, error) {
	raw := strings.Fields(topic)
	tokens := normalizeHelpTokens(raw)
	if len(tokens) == 0 {
		return usage, nil
	}
	sections := parseUsageSections()
	knownCmd := false
	var outLines []string
	for _, sec := range sections {
		var matched []usageEntry
		allInSection := len(sec.entries) > 0
		for _, e := range sec.entries {
			if e.cmd == tokens[0] || (tokens[0] == "invite" && e.cmd == "invites") {
				knownCmd = true
			}
			if entryMatches(e, tokens) {
				matched = append(matched, e)
			} else {
				allInSection = false
			}
		}
		if len(matched) == 0 {
			continue
		}
		if len(tokens) == 1 && allInSection && len(sec.header) > 0 {
			outLines = append(outLines, sec.header...)
		}
		for _, m := range matched {
			outLines = append(outLines, m.lines...)
		}
	}
	if len(outLines) == 0 {
		if !knownCmd {
			return "", fmt.Errorf("%w: unknown command %q", errUsage, raw[0])
		}
		return "", fmt.Errorf("%w: unknown %s subcommand %q", errUsage, tokens[0], strings.Join(raw[1:], " "))
	}
	return strings.Join(outLines, "\n") + "\n", nil
}
