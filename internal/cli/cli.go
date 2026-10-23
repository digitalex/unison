// Package cli implements the `unison` command: the server (`unison serve`)
// and a CLI that maps 1:1 onto the HTTP API.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/digitalex/unison/internal/guide"
	"github.com/digitalex/unison/internal/server"
	"github.com/digitalex/unison/internal/store"
	"github.com/digitalex/unison/internal/webui"
)

// DefaultServer is the server URL used when none is configured.
const DefaultServer = "http://localhost:4600"

// errUsage marks errors caused by bad command-line usage.
var errUsage = errors.New("usage")

// exitError carries a specific exit code.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

const usage = `unison — the isomorphic project workspace. Humans use the web UI; agents
use this CLI. Both do exactly the same things.

Agents: run ` + "`unison --agent-help`" + ` first — it is the whole protocol.

Server:
  serve [--addr 127.0.0.1:4600] [--db ~/.unison/unison.db]
                                     --db also takes a postgres:// DSN; --addr
                                     defaults to :$PORT when PORT is set

Workspaces & identity:
  workspace [show]                   show the current workspace
  workspace list                     list workspaces on the server
  workspace create <name> [--repo URL] [--canvas URL]
  workspace set [--name N] [--repo URL] [--canvas URL]
  join <workspace-url> [--name N] [--emoji E]   register; saves .unison.json here
  whoami                             your identity in this workspace
  status <text>                      set your status line ("" clears it)
  who                                participants and who is active

Chat:
  chat log [--limit N]               recent messages, with threads
  chat send <text> [--reply-to ID]   post a message (or reply in a thread)

Backlog (#1 is the highest priority):
  backlog [list] [--status open|assigned|done|all]
  backlog show <id>
  backlog add <title> [--body B] [--position N]
  backlog edit <id> [--title T] [--body B]
  backlog move <id> <position>
  backlog claim                      atomically assign yourself the top open item
  backlog assign <id> [--to NAME]    take (or renew) a 12-hour lease on an item
  backlog release <id>               give an item back to the queue
  backlog close <id> [--note N]      mark done (note: e.g. the PR URL)
  backlog reopen <id>
  backlog delete <id>

Metrics (what the workspace is optimizing; adding one also adds a backlog
item to make sure it gets tracked):
  metric [list]
  metric show <id>
  metric add <name> [--description D] [--direction increase|decrease] [--unit U] [--target T]
  metric edit <id> [--name N] [--description D] [--direction increase|decrease] [--unit U] [--target T|none]
  metric delete <id>                 (keeps its tracking backlog item)

Real time:
  wait [--mentions] [--timeout SECONDS]   block until something concerns you

Global flags (anywhere): --json  --server URL  --workspace ID  --token T
Environment: UNISON_SERVER, UNISON_WORKSPACE, UNISON_TOKEN, UNISON_CONFIG
`

type app struct {
	out     io.Writer
	cfg     *Config
	jsonOut bool
	server  string
	ws      string
	token   string
}

// Run executes the unison command with args (excluding the program name)
// and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	err := run(args, stdout)
	if err == nil {
		return 0
	}
	var ee *exitError
	if errors.As(err, &ee) {
		if ee.msg != "" {
			fmt.Fprintln(stderr, ee.msg)
		}
		return ee.code
	}
	fmt.Fprintln(stderr, "unison:", err)
	if errors.Is(err, errUsage) {
		fmt.Fprintln(stderr, "Run `unison help` for usage.")
	}
	return 1
}

func run(args []string, stdout io.Writer) error {
	a := &app{out: stdout}
	var configPath string
	args, err := extractGlobals(args, map[string]*string{
		"server": &a.server, "workspace": &a.ws, "token": &a.token, "config": &configPath,
	}, map[string]*bool{"json": &a.jsonOut})
	if err != nil {
		return err
	}
	if configPath == "" {
		configPath = findConfig()
	}
	if a.cfg, err = loadConfig(configPath); err != nil {
		return err
	}
	a.server = firstNonEmpty(a.server, os.Getenv("UNISON_SERVER"), a.cfg.Server, DefaultServer)
	a.ws = firstNonEmpty(a.ws, os.Getenv("UNISON_WORKSPACE"), a.cfg.Workspace)
	a.token = firstNonEmpty(a.token, os.Getenv("UNISON_TOKEN"), a.cfg.Token)

	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return nil
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return nil
	case "--agent-help", "agent-help":
		return a.agentHelp()
	case "serve":
		return serve(rest)
	case "workspace", "ws":
		return a.workspace(rest)
	case "join":
		return a.join(rest)
	case "whoami":
		return a.whoami()
	case "status":
		return a.status(rest)
	case "who":
		return a.who()
	case "chat":
		return a.chat(rest)
	case "backlog":
		return a.backlog(rest)
	case "metric", "metrics":
		return a.metric(rest)
	case "wait":
		return a.wait(rest)
	}
	return fmt.Errorf("%w: unknown command %q", errUsage, cmd)
}

// extractGlobals removes global flags (in --name value, --name=value or
// --bool form) from anywhere in args.
func extractGlobals(args []string, strs map[string]*string, bools map[string]*bool) ([]string, error) {
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if !strings.HasPrefix(arg, "-") {
			rest = append(rest, arg)
			continue
		}
		if p, ok := bools[name]; ok {
			*p = !hasValue || value == "true"
			continue
		}
		p, ok := strs[name]
		if !ok {
			rest = append(rest, arg)
			continue
		}
		if !hasValue {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("%w: --%s needs a value", errUsage, name)
			}
			i++
			value = args[i]
		}
		*p = value
	}
	return rest, nil
}

// parseFlags parses fs from args, allowing flags and positional arguments to
// be interleaved, and returns the positional arguments.
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, fmt.Errorf("%w: %v", errUsage, err)
		}
		args = fs.Args()
		if len(args) == 0 {
			return positional, nil
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func (a *app) client() *client { return newClient(a.server, a.token) }

// wsPath returns the API path for the current workspace.
func (a *app) wsPath(suffix string) (string, error) {
	if a.ws == "" {
		return "", errors.New("no workspace selected: run `unison join <workspace-url>` (or pass --workspace)")
	}
	return "/api/workspaces/" + a.ws + suffix, nil
}

func (a *app) needToken() error {
	if a.token == "" {
		return errors.New("you have not joined this workspace: run `unison join <workspace-url> --name <YourName>`")
	}
	return nil
}

// call performs a request against the current workspace. In --json mode it
// prints the raw response and returns ok=false so callers skip human output.
func (a *app) call(method, suffix string, body, out any, auth bool) (ok bool, err error) {
	path, err := a.wsPath(suffix)
	if err != nil {
		return false, err
	}
	if auth {
		if err := a.needToken(); err != nil {
			return false, err
		}
	}
	var raw json.RawMessage
	if err := a.client().do(method, path, body, &raw); err != nil {
		return false, err
	}
	if a.jsonOut {
		return false, a.printJSON(raw)
	}
	return true, json.Unmarshal(raw, out)
}

func (a *app) printJSON(raw json.RawMessage) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	enc := json.NewEncoder(a.out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func (a *app) printf(format string, args ...any) { fmt.Fprintf(a.out, format, args...) }

// ---- serve ----

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	home, _ := os.UserHomeDir()
	addr := fs.String("addr", firstNonEmpty(os.Getenv("UNISON_ADDR"), portAddr(os.Getenv("PORT")), "127.0.0.1:4600"), "listen address")
	dbPath := fs.String("db", firstNonEmpty(os.Getenv("UNISON_DB"), filepath.Join(home, ".unison", "unison.db")), "SQLite database path, or a Postgres DSN (postgres://...)")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	dbLabel := *dbPath
	if store.IsPostgresDSN(*dbPath) {
		dbLabel = "postgres" // Don't log credentials.
	} else if err := os.MkdirAll(filepath.Dir(*dbPath), 0o755); err != nil {
		return err
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	srv := server.New(st, webui.FS())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go srv.RunLeaseSweeper(ctx, 30*time.Second)
	hs := &http.Server{Addr: *addr, Handler: srv}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		hs.Shutdown(shutdown)
	}()
	host := *addr
	if strings.HasPrefix(host, ":") {
		host = "localhost" + host
	}
	log.Printf("Unison is serving http://%s (database %s)", host, dbLabel)
	log.Printf("Agents start at http://%s/agent.md", host)
	if err := hs.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// portAddr returns ":PORT" when the PORT variable is set (as on Cloud Run and
// similar platforms), so the server listens on all interfaces there. An
// explicit --addr or UNISON_ADDR takes precedence.
func portAddr(port string) string {
	if port == "" {
		return ""
	}
	return ":" + port
}

// ---- agent help ----

func (a *app) agentHelp() error {
	var ws *store.Workspace
	if a.ws != "" {
		var w store.Workspace
		if err := a.client().do("GET", "/api/workspaces/"+a.ws, nil, &w); err == nil {
			ws = &w
		}
	}
	fmt.Fprint(a.out, guide.Doorway(a.server, ws))
	return nil
}

// ---- workspaces & identity ----

func (a *app) workspace(args []string) error {
	sub := "show"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "show":
		var ws store.Workspace
		if ok, err := a.call("GET", "", nil, &ws, false); !ok {
			return err
		}
		a.printWorkspace(&ws)
		return nil
	case "list":
		var raw json.RawMessage
		if err := a.client().do("GET", "/api/workspaces", nil, &raw); err != nil {
			return err
		}
		if a.jsonOut {
			return a.printJSON(raw)
		}
		var list []store.Workspace
		if err := json.Unmarshal(raw, &list); err != nil {
			return err
		}
		if len(list) == 0 {
			a.printf("No workspaces yet. Create one: unison workspace create <name> --repo <url>\n")
		}
		for _, ws := range list {
			a.printf("%s  %s  %s\n", ws.ID, ws.Name, ws.RepoURL)
		}
		return nil
	case "create":
		return a.workspaceCreate(args)
	case "set":
		return a.workspaceSet(args)
	}
	return fmt.Errorf("%w: unknown workspace subcommand %q", errUsage, sub)
}

func (a *app) printWorkspace(ws *store.Workspace) {
	a.printf("Workspace  %s (%s)\n", ws.Name, ws.ID)
	a.printf("Web UI     %s/w/%s\n", strings.TrimRight(a.server, "/"), ws.ID)
	a.printf("Repo       %s\n", firstNonEmpty(ws.RepoURL, "(not set)"))
	a.printf("Canvas     %s\n", firstNonEmpty(ws.CanvasURL, "(not set)"))
}

func (a *app) workspaceCreate(args []string) error {
	fs := flag.NewFlagSet("workspace create", flag.ContinueOnError)
	repo := fs.String("repo", "", "repository URL")
	canvas := fs.String("canvas", "", "isocan canvas URL")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return fmt.Errorf("%w: workspace create <name>", errUsage)
	}
	var raw json.RawMessage
	body := map[string]string{"name": strings.Join(pos, " "), "repo_url": *repo, "canvas_url": *canvas}
	if err := a.client().do("POST", "/api/workspaces", body, &raw); err != nil {
		return err
	}
	if a.jsonOut {
		return a.printJSON(raw)
	}
	var ws store.Workspace
	if err := json.Unmarshal(raw, &ws); err != nil {
		return err
	}
	a.printWorkspace(&ws)
	a.printf("\nJoin it: unison join %s/w/%s --name <YourName>\n", strings.TrimRight(a.server, "/"), ws.ID)
	return nil
}

func (a *app) workspaceSet(args []string) error {
	fs := flag.NewFlagSet("workspace set", flag.ContinueOnError)
	var patch store.WorkspacePatch
	fs.Func("name", "workspace name", func(v string) error { patch.Name = &v; return nil })
	fs.Func("repo", "repository URL", func(v string) error { patch.RepoURL = &v; return nil })
	fs.Func("canvas", "isocan canvas URL", func(v string) error { patch.CanvasURL = &v; return nil })
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	var ws store.Workspace
	if ok, err := a.call("PATCH", "", patch, &ws, true); !ok {
		return err
	}
	a.printWorkspace(&ws)
	return nil
}

func (a *app) join(args []string) error {
	fs := flag.NewFlagSet("join", flag.ContinueOnError)
	name := fs.String("name", "", "your name (one word; auto-assigned if omitted)")
	emoji := fs.String("emoji", "", "your emoji (auto-assigned if omitted)")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		server, ws, err := parseInvite(pos[0])
		if err != nil {
			return err
		}
		a.server = server
		if ws != "" {
			a.ws = ws
		}
	}
	path, err := a.wsPath("/join")
	if err != nil {
		return err
	}
	var resp struct {
		Participant store.Participant `json:"participant"`
		Token       string            `json:"token"`
	}
	c := newClient(a.server, "")
	if err := c.do("POST", path, map[string]string{"name": *name, "emoji": *emoji}, &resp); err != nil {
		return err
	}
	var events struct {
		Cursor int64 `json:"cursor"`
	}
	if err := c.do("GET", "/api/workspaces/"+a.ws+"/events", nil, &events); err != nil {
		return err
	}
	cfg := &Config{
		Server: a.server, Workspace: a.ws, Token: resp.Token, ParticipantID: resp.Participant.ID,
		Name: resp.Participant.Name, Cursor: events.Cursor, path: firstNonEmpty(os.Getenv("UNISON_CONFIG"), ConfigFile),
	}
	if err := cfg.save(); err != nil {
		return err
	}
	if a.jsonOut {
		raw, _ := json.Marshal(resp)
		return a.printJSON(raw)
	}
	a.printf("Joined as %s %s (participant %d). Session saved to %s — keep it out of git.\n",
		resp.Participant.Emoji, resp.Participant.Name, resp.Participant.ID, cfg.path)
	a.printf("Next: `unison --agent-help` for the protocol, `unison workspace` to orient.\n")
	return nil
}

func (a *app) whoami() error {
	var p store.Participant
	if ok, err := a.call("GET", "/me", nil, &p, true); !ok {
		return err
	}
	a.printf("%s %s (participant %d) in workspace %s\n", p.Emoji, p.Name, p.ID, p.WorkspaceID)
	if p.Status != "" {
		a.printf("Status: %s\n", p.Status)
	}
	return nil
}

func (a *app) status(args []string) error {
	var p store.Participant
	if ok, err := a.call("PATCH", "/me", map[string]string{"status": strings.Join(args, " ")}, &p, true); !ok {
		return err
	}
	a.printf("%s %s: %s\n", p.Emoji, p.Name, firstNonEmpty(p.Status, "(no status)"))
	return nil
}

// activeWindow is how recently a participant must have been seen to count
// as present.
const activeWindow = 2 * time.Minute

func (a *app) who() error {
	var list []store.Participant
	if ok, err := a.call("GET", "/participants", nil, &list, false); !ok {
		return err
	}
	for _, p := range list {
		dot := "⚪"
		if time.Since(time.UnixMilli(p.LastSeenAt)) < activeWindow {
			dot = "🟢"
		}
		line := fmt.Sprintf("%s %s %s", dot, p.Emoji, p.Name)
		if p.Status != "" {
			line += " — " + p.Status
		}
		a.printf("%s  (seen %s)\n", line, ago(p.LastSeenAt))
	}
	return nil
}

// participants returns the workspace's participants keyed by ID.
func (a *app) participants() (map[int64]store.Participant, error) {
	path, err := a.wsPath("/participants")
	if err != nil {
		return nil, err
	}
	var list []store.Participant
	if err := a.client().do("GET", path, nil, &list); err != nil {
		return nil, err
	}
	out := map[int64]store.Participant{}
	for _, p := range list {
		out[p.ID] = p
	}
	return out, nil
}

func label(people map[int64]store.Participant, id int64) string {
	if p, ok := people[id]; ok {
		return p.Emoji + " " + p.Name
	}
	return fmt.Sprintf("participant %d", id)
}

func ago(ms int64) string {
	d := time.Since(time.UnixMilli(ms)).Round(time.Second)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// ---- chat ----

func (a *app) chat(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: chat log | chat send <text>", errUsage)
	}
	switch args[0] {
	case "log":
		return a.chatLog(args[1:])
	case "send":
		return a.chatSend(args[1:])
	}
	return fmt.Errorf("%w: unknown chat subcommand %q", errUsage, args[0])
}

func (a *app) chatLog(args []string) error {
	fs := flag.NewFlagSet("chat log", flag.ContinueOnError)
	limit := fs.Int("limit", 50, "number of recent messages")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	var msgs []store.Message
	if ok, err := a.call("GET", fmt.Sprintf("/messages?limit=%d", *limit), nil, &msgs, false); !ok {
		return err
	}
	people, err := a.participants()
	if err != nil {
		return err
	}
	replies := map[int64][]store.Message{}
	shown := map[int64]bool{}
	for _, m := range msgs {
		shown[m.ID] = true
		if m.ParentID != nil {
			replies[*m.ParentID] = append(replies[*m.ParentID], m)
		}
	}
	for _, m := range msgs {
		if m.ParentID != nil && shown[*m.ParentID] {
			continue // Printed under its thread root.
		}
		a.printMessage(people, m, "")
		for _, r := range replies[m.ID] {
			a.printMessage(people, r, "    ↳ ")
		}
	}
	if len(msgs) == 0 {
		a.printf("No messages yet.\n")
	}
	return nil
}

func (a *app) printMessage(people map[int64]store.Participant, m store.Message, indent string) {
	thread := ""
	if m.ParentID != nil && indent == "" {
		thread = fmt.Sprintf(" (reply in thread #%d)", *m.ParentID)
	}
	when := time.UnixMilli(m.CreatedAt).Format("15:04")
	body := strings.ReplaceAll(m.Body, "\n", "\n"+indent+"      ")
	a.printf("%s[#%d %s] %s%s: %s\n", indent, m.ID, when, label(people, m.AuthorID), thread, body)
}

func (a *app) chatSend(args []string) error {
	fs := flag.NewFlagSet("chat send", flag.ContinueOnError)
	replyTo := fs.String("reply-to", "", "message ID to reply to (threads)")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return fmt.Errorf("%w: chat send <text>", errUsage)
	}
	body := map[string]any{"body": strings.Join(pos, " ")}
	if *replyTo != "" {
		id, err := parseID(*replyTo)
		if err != nil {
			return err
		}
		body["parent_id"] = id
	}
	var m store.Message
	if ok, err := a.call("POST", "/messages", body, &m, true); !ok {
		return err
	}
	a.printf("Sent message #%d.\n", m.ID)
	return nil
}

func parseID(s string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimPrefix(s, "#"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q is not an id", errUsage, s)
	}
	return id, nil
}

// ---- backlog ----

func (a *app) backlog(args []string) error {
	sub := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "list", "ls":
		return a.backlogList(args)
	case "show":
		return a.itemCommand(args, "GET", "", nil, false)
	case "add":
		return a.backlogAdd(args)
	case "edit":
		return a.backlogEdit(args)
	case "move":
		if len(args) != 2 {
			return fmt.Errorf("%w: backlog move <id> <position>", errUsage)
		}
		position, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("%w: position must be a number", errUsage)
		}
		return a.itemCommand(args[:1], "POST", "/move", map[string]int{"position": position}, true)
	case "claim":
		var it store.Item
		ok, err := a.call("POST", "/backlog/claim", nil, &it, true)
		if !ok {
			var ae *apiError
			if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
				return &exitError{code: 3, msg: "No open backlog items to claim. Park on `unison wait` until there is work."}
			}
			return err
		}
		return a.printItemDetail(&it)
	case "assign":
		return a.backlogAssign(args)
	case "release":
		return a.itemCommand(args, "POST", "/release", nil, true)
	case "close":
		fs := flag.NewFlagSet("backlog close", flag.ContinueOnError)
		note := fs.String("note", "", "closing note, e.g. the PR URL")
		pos, err := parseFlags(fs, args)
		if err != nil {
			return err
		}
		return a.itemCommand(pos, "POST", "/close", map[string]string{"note": *note}, true)
	case "reopen":
		return a.itemCommand(args, "POST", "/reopen", nil, true)
	case "delete", "rm":
		if len(args) != 1 {
			return fmt.Errorf("%w: backlog delete <id>", errUsage)
		}
		id, err := parseID(args[0])
		if err != nil {
			return err
		}
		var out map[string]any
		if ok, err := a.call("DELETE", fmt.Sprintf("/backlog/%d", id), nil, &out, true); !ok {
			return err
		}
		a.printf("Deleted #%d.\n", id)
		return nil
	}
	return fmt.Errorf("%w: unknown backlog subcommand %q", errUsage, sub)
}

// itemCommand runs method on /backlog/{id}{suffix} and prints the item.
func (a *app) itemCommand(args []string, method, suffix string, body any, auth bool) error {
	if len(args) != 1 {
		return fmt.Errorf("%w: expected exactly one item id", errUsage)
	}
	id, err := parseID(args[0])
	if err != nil {
		return err
	}
	var it store.Item
	if ok, err := a.call(method, fmt.Sprintf("/backlog/%d%s", id, suffix), body, &it, auth); !ok {
		return err
	}
	return a.printItemDetail(&it)
}

func (a *app) backlogList(args []string) error {
	fs := flag.NewFlagSet("backlog list", flag.ContinueOnError)
	status := fs.String("status", "", "open, assigned, done or all (default: open and assigned)")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	var items []store.Item
	if ok, err := a.call("GET", "/backlog?status="+*status, nil, &items, false); !ok {
		return err
	}
	people, err := a.participants()
	if err != nil {
		return err
	}
	if len(items) == 0 {
		a.printf("The backlog is empty.\n")
	}
	for _, it := range items {
		a.printf("%s\n", itemLine(people, &it))
	}
	return nil
}

func itemLine(people map[int64]store.Participant, it *store.Item) string {
	pos := "  "
	if it.Status != store.StatusDone {
		pos = fmt.Sprintf("%d.", it.Position)
	}
	return fmt.Sprintf("%-4s #%-4d %-40s %s", pos, it.ID, it.Title, itemState(people, it))
}

func itemState(people map[int64]store.Participant, it *store.Item) string {
	switch it.Status {
	case store.StatusAssigned:
		left := max(time.Until(time.UnixMilli(*it.LeaseExpiresAt)).Round(time.Minute), 0)
		return fmt.Sprintf("[assigned → %s, lease %dh%02dm left]", label(people, *it.AssigneeID), int(left.Hours()), int(left.Minutes())%60)
	case store.StatusDone:
		s := "[done"
		if it.AssigneeID != nil {
			s += " by " + label(people, *it.AssigneeID)
		}
		if it.CloseNote != "" {
			s += ": " + it.CloseNote
		}
		return s + "]"
	}
	return "[open]"
}

func (a *app) printItemDetail(it *store.Item) error {
	people, err := a.participants()
	if err != nil {
		return err
	}
	a.printf("#%d  %s\n", it.ID, it.Title)
	if it.Status != store.StatusDone {
		a.printf("Priority  %d\n", it.Position)
	}
	a.printf("State     %s\n", itemState(people, it))
	a.printf("Created   by %s, %s\n", label(people, it.CreatedBy), ago(it.CreatedAt))
	if it.Body != "" {
		a.printf("\n%s\n", it.Body)
	}
	return nil
}

func (a *app) backlogAdd(args []string) error {
	fs := flag.NewFlagSet("backlog add", flag.ContinueOnError)
	body := fs.String("body", "", "description")
	position := fs.Int("position", 0, "priority position (1 = top; default bottom)")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return fmt.Errorf("%w: backlog add <title>", errUsage)
	}
	var it store.Item
	req := map[string]any{"title": strings.Join(pos, " "), "body": *body, "position": *position}
	if ok, err := a.call("POST", "/backlog", req, &it, true); !ok {
		return err
	}
	return a.printItemDetail(&it)
}

func (a *app) backlogEdit(args []string) error {
	fs := flag.NewFlagSet("backlog edit", flag.ContinueOnError)
	var patch store.ItemPatch
	fs.Func("title", "new title", func(v string) error { patch.Title = &v; return nil })
	fs.Func("body", "new description", func(v string) error { patch.Body = &v; return nil })
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	return a.itemCommand(pos, "PATCH", "", patch, true)
}

func (a *app) backlogAssign(args []string) error {
	fs := flag.NewFlagSet("backlog assign", flag.ContinueOnError)
	to := fs.String("to", "", "participant name (default: yourself)")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	body := map[string]int64{}
	if *to != "" {
		people, err := a.participants()
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(*to, "@")
		for _, p := range people {
			if strings.EqualFold(p.Name, name) {
				body["assignee_id"] = p.ID
			}
		}
		if body["assignee_id"] == 0 {
			return fmt.Errorf("no participant named %q in this workspace (see `unison who`)", name)
		}
	}
	return a.itemCommand(pos, "POST", "/assign", body, true)
}

// ---- metrics ----

func (a *app) metric(args []string) error {
	sub := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "list", "ls":
		var list []store.Metric
		if ok, err := a.call("GET", "/metrics", nil, &list, false); !ok {
			return err
		}
		if len(list) == 0 {
			a.printf("No metrics yet. Add one: unison metric add <name> --direction increase|decrease\n")
		}
		for _, m := range list {
			a.printf("%s\n", metricLine(&m))
		}
		return nil
	case "show":
		return a.metricCommand(args, "GET", nil, false)
	case "add":
		return a.metricAdd(args)
	case "edit":
		return a.metricEdit(args)
	case "delete", "rm":
		if len(args) != 1 {
			return fmt.Errorf("%w: metric delete <id>", errUsage)
		}
		id, err := parseID(args[0])
		if err != nil {
			return err
		}
		var out map[string]any
		if ok, err := a.call("DELETE", fmt.Sprintf("/metrics/%d", id), nil, &out, true); !ok {
			return err
		}
		a.printf("Deleted metric %d (its tracking backlog item is kept).\n", id)
		return nil
	}
	return fmt.Errorf("%w: unknown metric subcommand %q", errUsage, sub)
}

// metricCommand runs method on /metrics/{id} and prints the metric.
func (a *app) metricCommand(args []string, method string, body any, auth bool) error {
	if len(args) != 1 {
		return fmt.Errorf("%w: expected exactly one metric id", errUsage)
	}
	id, err := parseID(args[0])
	if err != nil {
		return err
	}
	var m store.Metric
	if ok, err := a.call(method, fmt.Sprintf("/metrics/%d", id), body, &m, auth); !ok {
		return err
	}
	a.printMetricDetail(&m)
	return nil
}

func directionArrow(direction string) string {
	if direction == store.DirectionDecrease {
		return "↓"
	}
	return "↑"
}

func metricLine(m *store.Metric) string {
	line := fmt.Sprintf("%-4d %s %-30s", m.ID, directionArrow(m.Direction), m.Name)
	if t := m.FormatTarget(); t != "" {
		line += "  target " + t
	} else if m.Unit != "" {
		line += "  (" + m.Unit + ")"
	}
	if m.ItemID != nil {
		line += fmt.Sprintf("  tracked by #%d", *m.ItemID)
	}
	return line
}

func (a *app) printMetricDetail(m *store.Metric) {
	a.printf("Metric %d  %s\n", m.ID, m.Name)
	a.printf("Goal      %s %s\n", directionArrow(m.Direction), m.Direction)
	if t := m.FormatTarget(); t != "" {
		a.printf("Target    %s\n", t)
	} else if m.Unit != "" {
		a.printf("Unit      %s\n", m.Unit)
	}
	if m.ItemID != nil {
		a.printf("Tracking  backlog item #%d (unison backlog show %d)\n", *m.ItemID, *m.ItemID)
	}
	if m.Description != "" {
		a.printf("\n%s\n", m.Description)
	}
}

// targetFlag parses --target: a number, or "none" to clear it.
func targetFlag(v string) (any, error) {
	if v == "" || strings.EqualFold(v, "none") {
		return nil, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: --target must be a number (or none)", errUsage)
	}
	return f, nil
}

func (a *app) metricAdd(args []string) error {
	fs := flag.NewFlagSet("metric add", flag.ContinueOnError)
	req := map[string]any{}
	fs.Func("description", "what the metric measures", func(v string) error { req["description"] = v; return nil })
	fs.Func("direction", "increase (default) or decrease", func(v string) error { req["direction"] = v; return nil })
	fs.Func("unit", "unit, e.g. ms or %", func(v string) error { req["unit"] = v; return nil })
	fs.Func("target", "target value", func(v string) error {
		t, err := targetFlag(v)
		req["target"] = t
		return err
	})
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return fmt.Errorf("%w: metric add <name>", errUsage)
	}
	req["name"] = strings.Join(pos, " ")
	var m store.Metric
	if ok, err := a.call("POST", "/metrics", req, &m, true); !ok {
		return err
	}
	a.printMetricDetail(&m)
	if m.ItemID != nil {
		a.printf("\nAdded backlog item #%d to make sure this metric is tracked.\n", *m.ItemID)
	}
	return nil
}

func (a *app) metricEdit(args []string) error {
	fs := flag.NewFlagSet("metric edit", flag.ContinueOnError)
	patch := map[string]any{}
	for _, f := range []string{"name", "description", "direction", "unit"} {
		fs.Func(f, "new "+f, func(v string) error { patch[f] = v; return nil })
	}
	fs.Func("target", "new target value, or none to clear it", func(v string) error {
		t, err := targetFlag(v)
		patch["target"] = t
		return err
	})
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	return a.metricCommand(pos, "PATCH", patch, true)
}

// ---- wait ----

func (a *app) wait(args []string) error {
	fs := flag.NewFlagSet("wait", flag.ContinueOnError)
	mentions := fs.Bool("mentions", false, "wake only for @mentions, replies to your threads, and items assigned to you")
	timeout := fs.Int("timeout", 600, "give up after this many seconds (exit code 2)")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	if err := a.needToken(); err != nil {
		return err
	}
	var me store.Participant
	path, err := a.wsPath("/me")
	if err != nil {
		return err
	}
	c := a.client()
	if err := c.do("GET", path, nil, &me); err != nil {
		return err
	}
	cursor := a.cfg.Cursor
	if a.cfg.Workspace != a.ws || cursor == 0 {
		var resp struct {
			Cursor int64 `json:"cursor"`
		}
		if err := c.do("GET", "/api/workspaces/"+a.ws+"/events", nil, &resp); err != nil {
			return err
		}
		cursor = resp.Cursor
	}
	f := &eventFilter{me: &me, mentionsOnly: *mentions, client: c, ws: a.ws}
	deadline := time.Now().Add(time.Duration(*timeout) * time.Second)
	for {
		remaining := int(time.Until(deadline).Seconds())
		if remaining <= 0 {
			return &exitError{code: 2, msg: "Nothing new. Run `unison wait` again to keep listening."}
		}
		var resp struct {
			Events []store.Event `json:"events"`
			Cursor int64         `json:"cursor"`
		}
		q := fmt.Sprintf("/api/workspaces/%s/events?after=%d&wait=%d", a.ws, cursor, min(remaining, 60))
		if err := c.do("GET", q, nil, &resp); err != nil {
			return err
		}
		var relevant []store.Event
		for _, e := range resp.Events {
			if f.relevant(e) {
				relevant = append(relevant, e)
			}
		}
		cursor = resp.Cursor
		if a.cfg.path != "" && a.cfg.Workspace == a.ws {
			a.cfg.Cursor = cursor
			if err := a.cfg.save(); err != nil {
				return err
			}
		}
		if len(relevant) > 0 {
			return a.printEvents(relevant)
		}
	}
}

type eventFilter struct {
	me           *store.Participant
	mentionsOnly bool
	client       *client
	ws           string
	authors      map[int64]int64 // message ID → author ID, fetched lazily.
}

func (f *eventFilter) relevant(e store.Event) bool {
	if e.ActorID == f.me.ID {
		return false
	}
	switch {
	case e.Type == "message.created":
		var m store.Message
		if json.Unmarshal(e.Data, &m) != nil {
			return false
		}
		return !f.mentionsOnly || mentions(m.Body, f.me.Name) || (m.ParentID != nil && f.authorOf(*m.ParentID) == f.me.ID)
	case strings.HasPrefix(e.Type, "backlog."):
		if !f.mentionsOnly {
			return true
		}
		var it store.Item
		return json.Unmarshal(e.Data, &it) == nil && e.Type == "backlog.assigned" && it.AssigneeID != nil && *it.AssigneeID == f.me.ID
	case e.Type == "participant.joined", e.Type == "workspace.updated", strings.HasPrefix(e.Type, "metric."):
		return !f.mentionsOnly
	}
	return false
}

func (f *eventFilter) authorOf(msgID int64) int64 {
	if f.authors == nil {
		f.authors = map[int64]int64{}
		var msgs []store.Message
		if err := f.client.do("GET", "/api/workspaces/"+f.ws+"/messages?limit=1000", nil, &msgs); err == nil {
			for _, m := range msgs {
				f.authors[m.ID] = m.AuthorID
			}
		}
	}
	return f.authors[msgID]
}

func mentions(body, name string) bool {
	re := regexp.MustCompile(`(?i)@(` + regexp.QuoteMeta(name) + `|all|everyone)\b`)
	return re.MatchString(body)
}

func (a *app) printEvents(evs []store.Event) error {
	if a.jsonOut {
		raw, _ := json.Marshal(evs)
		return a.printJSON(raw)
	}
	people, err := a.participants()
	if err != nil {
		return err
	}
	for _, e := range evs {
		a.printf("%s\n", describeEvent(people, e))
	}
	return nil
}

func describeEvent(people map[int64]store.Participant, e store.Event) string {
	who := label(people, e.ActorID)
	switch {
	case e.Type == "message.created":
		var m store.Message
		json.Unmarshal(e.Data, &m)
		where := ""
		if m.ParentID != nil {
			where = fmt.Sprintf(" (reply in thread #%d — answer with --reply-to %d)", *m.ParentID, *m.ParentID)
		}
		return fmt.Sprintf("💬 [#%d] %s%s: %s", m.ID, who, where, m.Body)
	case strings.HasPrefix(e.Type, "backlog."):
		var it store.Item
		json.Unmarshal(e.Data, &it)
		verb := strings.TrimPrefix(e.Type, "backlog.")
		if e.Type == "backlog.lease_expired" {
			return fmt.Sprintf("📋 the lease on #%d %q expired; it is open again", it.ID, it.Title)
		}
		line := fmt.Sprintf("📋 %s %s #%d %q", who, verb, it.ID, it.Title)
		if e.Type == "backlog.assigned" && it.AssigneeID != nil {
			line += " → " + label(people, *it.AssigneeID)
		}
		return line
	case e.Type == "participant.joined":
		var p store.Participant
		json.Unmarshal(e.Data, &p)
		return fmt.Sprintf("👋 %s %s joined", p.Emoji, p.Name)
	case e.Type == "workspace.updated":
		return fmt.Sprintf("🛠  %s updated the workspace settings", who)
	case strings.HasPrefix(e.Type, "metric."):
		var m store.Metric
		json.Unmarshal(e.Data, &m)
		return fmt.Sprintf("📈 %s %s metric %d %q", who, strings.TrimPrefix(e.Type, "metric."), m.ID, m.Name)
	}
	return fmt.Sprintf("%s by %s", e.Type, who)
}
