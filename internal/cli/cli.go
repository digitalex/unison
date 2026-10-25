// Package cli implements the `unison` command: the server (`unison serve`)
// and a CLI that maps 1:1 onto the HTTP API.
package cli

import (
	"bytes"
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
	"sort"
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
  workspace create <name> [--repo URL[=label]]... [--canvas URL]
  workspace set [--name N] [--repo URL] [--canvas URL]   (--repo: the first repo)
  workspace repos                    the workspace's code repositories
  workspace repo add <url> [--label L]
  workspace repo edit <id> [--label L] [--url U]
  workspace repo remove <id>
  join <workspace-url> [--name N] [--emoji E] [--pass P | --account]
                                     register; saves .unison.json here. --pass
                                     (agents): the pass from your invite, making you
                                     an agent of the member who sent it. --account
                                     (humans, after login) links you to your account
                                     and reuses your participant if you have one
  whoami                             your identity in this workspace
  status <text>                      set your status line ("" clears it)
  who                                participants and who is active

Accounts (optional for now; workspaces still work by joining):
  login                              sign in with GitHub or Google via your browser
                                     (device code); saves a token per server
  logout                             sign out (revokes the token)
  whoami --user                      your account on this server
  claim                              link the participant joined here (.unison.json)
                                     to your account, e.g. old Alex, Alexmobile...
  members                            the workspace's members (accounts) and their
                                     participants
  members remove <user-id>           remove a member (any member may; their
                                     participants stay, for history; their agents
                                     stop working)
  pass [--agent]                     mint a single-use agent pass (15 minutes) and
                                     print the invite prompt to paste into an agent

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
  backlog close <id> [--note N]      mark done (GitHub PR URLs in the note are
                                     also linked to the item as PRs)
  backlog reopen <id>
  backlog delete <id>
  backlog comments <id>              an item's comments
  backlog comment <id> <text>        comment on an item (@Name mentions)
  backlog comment <id> --delete CID  delete one of your comments
  backlog pr <id> <url>              link a pull request (several are allowed)
  backlog pr <id> --remove <url>     unlink a pull request

Webhooks (e.g. from GitHub; each delivery wakes agents with the webhook's
plain-text instructions):
  webhook [list]
  webhook show <id>
  webhook add <name> --instructions "..." [--signing-secret S]   prints the receive URL
  webhook edit <id> [--name N] [--instructions I] [--enable|--disable] [--signing-secret S|none]
  webhook url <id>                   the receive URL (keep it secret)
  webhook rotate <id>                issue a new receive URL; the old one stops working
  webhook delete <id>                (also deletes its deliveries)
  webhook deliveries <id>            recent deliveries
  webhook delivery <delivery-id>     headers and payload

Real time:
  wait [--mentions] [--timeout SECONDS]   block until something concerns you

Global flags (anywhere): --json  --server URL  --workspace ID  --token T
Environment: UNISON_SERVER, UNISON_WORKSPACE, UNISON_TOKEN, UNISON_CONFIG,
  UNISON_CREDENTIALS (login tokens; default in your user config dir),
  UNISON_NO_BROWSER (login: don't open a browser)
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
		return a.whoami(rest)
	case "login":
		return a.login(rest)
	case "logout":
		return a.logout(rest)
	case "claim":
		return a.claim(rest)
	case "pass":
		return a.pass(rest)
	case "members", "member":
		return a.members(rest)
	case "status":
		return a.status(rest)
	case "who":
		return a.who()
	case "chat":
		return a.chat(rest)
	case "backlog":
		return a.backlog(rest)
	case "webhook", "webhooks":
		return a.webhook(rest)
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
	srv.SetAuth(server.AuthConfigFromEnv())
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
			repos := ws.RepoURL
			if len(ws.Repos) > 1 {
				repos += fmt.Sprintf(" (+%d more)", len(ws.Repos)-1)
			}
			a.printf("%s  %s  %s\n", ws.ID, ws.Name, repos)
		}
		return nil
	case "create":
		return a.workspaceCreate(args)
	case "set":
		return a.workspaceSet(args)
	case "repos":
		var repos []store.Repo
		if ok, err := a.call("GET", "/repos", nil, &repos, false); !ok {
			return err
		}
		if len(repos) == 0 {
			a.printf("No repositories yet. Add one: unison workspace repo add <url> --label <name>\n")
		}
		for _, r := range repos {
			a.printf("%s\n", repoLine(r))
		}
		return nil
	case "repo":
		return a.workspaceRepo(args)
	}
	return fmt.Errorf("%w: unknown workspace subcommand %q", errUsage, sub)
}

func (a *app) printWorkspace(ws *store.Workspace) {
	a.printf("Workspace  %s (%s)\n", ws.Name, ws.ID)
	a.printf("Web UI     %s/w/%s\n", strings.TrimRight(a.server, "/"), ws.ID)
	if len(ws.Repos) == 0 {
		a.printf("Repo       (not set)\n")
	}
	for i, r := range ws.Repos {
		heading := "Repos     "
		if i > 0 {
			heading = "          "
		}
		a.printf("%s %s\n", heading, repoLine(r))
	}
	a.printf("Canvas     %s\n", firstNonEmpty(ws.CanvasURL, "(not set)"))
}

func repoLine(r store.Repo) string {
	if r.Label == "" {
		return fmt.Sprintf("%-3d %s", r.ID, r.URL)
	}
	return fmt.Sprintf("%-3d %s  (%s)", r.ID, r.URL, r.Label)
}

// parseRepoFlag splits "URL=label" (the label is optional).
func parseRepoFlag(v string) store.RepoInput {
	if i := strings.LastIndex(v, "="); i > 0 && !strings.ContainsAny(v[i+1:], "/:") {
		return store.RepoInput{URL: v[:i], Label: v[i+1:]}
	}
	return store.RepoInput{URL: v}
}

func (a *app) workspaceRepo(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: workspace repo add|edit|remove", errUsage)
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("workspace repo "+sub, flag.ContinueOnError)
	patch := map[string]string{}
	fs.Func("label", "short name, e.g. backend", func(v string) error { patch["label"] = v; return nil })
	if sub == "edit" {
		fs.Func("url", "new repository URL", func(v string) error { patch["url"] = v; return nil })
	}
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	var r store.Repo
	switch {
	case sub == "add" && len(pos) == 1:
		patch["url"] = pos[0]
		if ok, err := a.call("POST", "/repos", patch, &r, true); !ok {
			return err
		}
		a.printf("Added repository %s\n", repoLine(r))
		return nil
	case sub == "edit" && len(pos) == 1:
		id, err := parseID(pos[0])
		if err != nil {
			return err
		}
		if ok, err := a.call("PATCH", fmt.Sprintf("/repos/%d", id), patch, &r, true); !ok {
			return err
		}
		a.printf("Updated repository %s\n", repoLine(r))
		return nil
	case (sub == "remove" || sub == "rm") && len(pos) == 1:
		id, err := parseID(pos[0])
		if err != nil {
			return err
		}
		var out map[string]any
		if ok, err := a.call("DELETE", fmt.Sprintf("/repos/%d", id), nil, &out, true); !ok {
			return err
		}
		a.printf("Removed repository %d.\n", id)
		return nil
	}
	return fmt.Errorf("%w: workspace repo add <url> [--label L] | edit <id> [--label L] [--url U] | remove <id>", errUsage)
}

func (a *app) workspaceCreate(args []string) error {
	fs := flag.NewFlagSet("workspace create", flag.ContinueOnError)
	var repos []store.RepoInput
	fs.Func("repo", "repository URL, optionally URL=label (repeatable)", func(v string) error {
		repos = append(repos, parseRepoFlag(v))
		return nil
	})
	canvas := fs.String("canvas", "", "isocan canvas URL")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return fmt.Errorf("%w: workspace create <name>", errUsage)
	}
	var raw json.RawMessage
	body := map[string]any{"name": strings.Join(pos, " "), "repos": repos, "canvas_url": *canvas}
	// Servers may limit creation to signed-in accounts (UNISON_CREATORS).
	c := newClient(a.server, firstNonEmpty(a.userToken(), a.token))
	if err := c.do("POST", "/api/workspaces", body, &raw); err != nil {
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
	account := fs.Bool("account", false, "link the participant to your account (from `unison login`); humans only")
	pass := fs.String("pass", "", "agents: the single-use pass from your invite")
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
		Existing    bool              `json:"existing,omitempty"`
	}
	// Joins are anonymous unless --account: agents often run in a shell where
	// their human has run `unison login`, and must not become that human.
	if *account && *pass != "" {
		return fmt.Errorf("%w: --pass is for agents and --account for humans; use one", errUsage)
	}
	userToken := ""
	if *account {
		if userToken, err = a.needUserToken(); err != nil {
			return err
		}
	}
	body := map[string]string{"name": *name, "emoji": *emoji}
	if *pass != "" {
		body["pass"] = *pass
	}
	if err := newClient(a.server, userToken).do("POST", path, body, &resp); err != nil {
		return err
	}
	c := newClient(a.server, "")
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
	switch {
	case resp.Participant.Sponsor != nil:
		a.printf("Joined as %s %s (participant %d), agent of %s. Session saved to %s — keep it out of git.\n",
			resp.Participant.Emoji, resp.Participant.Name, resp.Participant.ID, resp.Participant.Sponsor.DisplayName, cfg.path)
		a.printf("Your credential lapses after %d days without use; if it stops working, ask %s for a new invite.\n",
			int(store.AgentCredentialTTL.Hours()/24), resp.Participant.Sponsor.DisplayName)
	case resp.Existing:
		a.printf("Welcome back: you are %s %s (participant %d), linked to your account. Session saved to %s — keep it out of git.\n",
			resp.Participant.Emoji, resp.Participant.Name, resp.Participant.ID, cfg.path)
	case *account:
		a.printf("Joined as %s %s (participant %d), linked to your account. Session saved to %s — keep it out of git.\n",
			resp.Participant.Emoji, resp.Participant.Name, resp.Participant.ID, cfg.path)
	default:
		a.printf("Joined as %s %s (participant %d). Session saved to %s — keep it out of git.\n",
			resp.Participant.Emoji, resp.Participant.Name, resp.Participant.ID, cfg.path)
	}
	a.printf("Next: `unison --agent-help` for the protocol, `unison workspace` to orient.\n")
	return nil
}

func (a *app) whoami(args []string) error {
	fs := flag.NewFlagSet("whoami", flag.ContinueOnError)
	user := fs.Bool("user", false, "show your account (from `unison login`) instead")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	if *user {
		return a.whoamiUser()
	}
	var p struct {
		store.Participant
		User *struct {
			ID          int64  `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"user"`
	}
	if ok, err := a.call("GET", "/me", nil, &p, true); !ok {
		return err
	}
	a.printf("%s %s (participant %d) in workspace %s\n", p.Emoji, p.Name, p.ID, p.WorkspaceID)
	if p.Status != "" {
		a.printf("Status: %s\n", p.Status)
	}
	if p.User != nil {
		a.printf("Account: %s (user %d)\n", firstNonEmpty(p.User.DisplayName, "(no name)"), p.User.ID)
	}
	if p.Sponsor != nil {
		a.printf("Agent of %s; credential expires %s unless used (each use extends it %d days).\n",
			p.Sponsor.DisplayName, time.UnixMilli(p.TokenExpiresAt).Format("2006-01-02 15:04"), int(store.AgentCredentialTTL.Hours()/24))
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
		if p.Sponsor != nil {
			line += " · agent of " + p.Sponsor.DisplayName
		}
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
		return a.backlogShow(args)
	case "comments":
		return a.backlogComments(args)
	case "comment":
		return a.backlogComment(args)
	case "pr":
		return a.backlogPR(args)
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
	for i, pr := range it.PRs {
		heading := "PRs      "
		if i > 0 {
			heading = "         "
		}
		a.printf("%s %s\n", heading, pr.URL)
	}
	if it.CommentCount > 0 {
		a.printf("Comments  %d (unison backlog comments %d)\n", it.CommentCount, it.ID)
	}
	if it.Body != "" {
		a.printf("\n%s\n", it.Body)
	}
	return nil
}

// backlogShow prints an item with its PR links and comments.
func (a *app) backlogShow(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%w: backlog show <id>", errUsage)
	}
	id, err := parseID(args[0])
	if err != nil {
		return err
	}
	if a.jsonOut {
		var it, comments json.RawMessage
		path, err := a.wsPath(fmt.Sprintf("/backlog/%d", id))
		if err != nil {
			return err
		}
		if err := a.client().do("GET", path, nil, &it); err != nil {
			return err
		}
		if err := a.client().do("GET", path+"/comments", nil, &comments); err != nil {
			return err
		}
		return a.printJSON(json.RawMessage(fmt.Sprintf(`{"item":%s,"comments":%s}`, it, comments)))
	}
	var it store.Item
	if _, err := a.call("GET", fmt.Sprintf("/backlog/%d", id), nil, &it, false); err != nil {
		return err
	}
	if err := a.printItemDetail(&it); err != nil {
		return err
	}
	if it.CommentCount == 0 {
		return nil
	}
	a.printf("\n")
	return a.printComments(id)
}

func (a *app) printComments(id int64) error {
	var list []store.Comment
	if _, err := a.call("GET", fmt.Sprintf("/backlog/%d/comments", id), nil, &list, false); err != nil {
		return err
	}
	people, err := a.participants()
	if err != nil {
		return err
	}
	if len(list) == 0 {
		a.printf("No comments on #%d yet.\n", id)
	}
	for _, c := range list {
		when := time.UnixMilli(c.CreatedAt).Format("Jan 2 15:04")
		a.printf("[comment %d, %s] %s: %s\n", c.ID, when, label(people, c.AuthorID), strings.ReplaceAll(c.Body, "\n", "\n      "))
	}
	return nil
}

func (a *app) backlogComments(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%w: backlog comments <id>", errUsage)
	}
	id, err := parseID(args[0])
	if err != nil {
		return err
	}
	if a.jsonOut {
		_, err := a.call("GET", fmt.Sprintf("/backlog/%d/comments", id), nil, nil, false)
		return err
	}
	return a.printComments(id)
}

func (a *app) backlogComment(args []string) error {
	fs := flag.NewFlagSet("backlog comment", flag.ContinueOnError)
	del := fs.String("delete", "", "comment id to delete (your own)")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 || (*del == "" && len(pos) < 2) {
		return fmt.Errorf("%w: backlog comment <id> <text> | backlog comment <id> --delete <comment-id>", errUsage)
	}
	id, err := parseID(pos[0])
	if err != nil {
		return err
	}
	if *del != "" {
		cid, err := parseID(*del)
		if err != nil {
			return err
		}
		var out map[string]any
		if ok, err := a.call("DELETE", fmt.Sprintf("/backlog/%d/comments/%d", id, cid), nil, &out, true); !ok {
			return err
		}
		a.printf("Deleted comment %d on #%d.\n", cid, id)
		return nil
	}
	var c store.Comment
	if ok, err := a.call("POST", fmt.Sprintf("/backlog/%d/comments", id), map[string]string{"body": strings.Join(pos[1:], " ")}, &c, true); !ok {
		return err
	}
	a.printf("Commented on #%d (comment %d).\n", id, c.ID)
	return nil
}

func (a *app) backlogPR(args []string) error {
	fs := flag.NewFlagSet("backlog pr", flag.ContinueOnError)
	remove := fs.String("remove", "", "PR URL to unlink")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	switch {
	case *remove != "" && len(pos) == 1:
		return a.itemCommand(pos, "DELETE", "/prs", map[string]string{"url": *remove}, true)
	case *remove == "" && len(pos) == 2:
		return a.itemCommand(pos[:1], "POST", "/prs", map[string]string{"url": pos[1]}, true)
	}
	return fmt.Errorf("%w: backlog pr <id> <url> | backlog pr <id> --remove <url>", errUsage)
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

// ---- webhooks ----

// webhookSecret is the create/rotate/url response.
type webhookSecret struct {
	store.Webhook
	Token string `json:"token"`
	URL   string `json:"url"`
}

func (a *app) webhook(args []string) error {
	sub := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "list", "ls":
		var list []store.Webhook
		if ok, err := a.call("GET", "/webhooks", nil, &list, false); !ok {
			return err
		}
		if len(list) == 0 {
			a.printf("No webhooks yet. Add one: unison webhook add <name> --instructions \"...\"\n")
		}
		for _, h := range list {
			a.printf("%s\n", webhookLine(&h))
		}
		return nil
	case "show":
		return a.webhookCommand(args, "GET", "", nil, false)
	case "add":
		return a.webhookAdd(args)
	case "edit":
		return a.webhookEdit(args)
	case "url":
		return a.webhookSecretCommand(args, "GET", "/secret", "")
	case "rotate":
		return a.webhookSecretCommand(args, "POST", "/rotate", "New receive URL (the old one no longer works):")
	case "delete", "rm":
		id, err := oneID(args, "webhook delete <id>")
		if err != nil {
			return err
		}
		var out map[string]any
		if ok, err := a.call("DELETE", fmt.Sprintf("/webhooks/%d", id), nil, &out, true); !ok {
			return err
		}
		a.printf("Deleted webhook %d and its deliveries.\n", id)
		return nil
	case "deliveries":
		id, err := oneID(args, "webhook deliveries <id>")
		if err != nil {
			return err
		}
		var list []store.Delivery
		if ok, err := a.call("GET", fmt.Sprintf("/webhooks/%d/deliveries", id), nil, &list, false); !ok {
			return err
		}
		if len(list) == 0 {
			a.printf("No deliveries yet.\n")
		}
		for _, d := range list {
			a.printf("%-5d %s  %s\n", d.ID, time.UnixMilli(d.ReceivedAt).Format("Jan 2 15:04:05"), d.Summary)
		}
		return nil
	case "delivery":
		id, err := oneID(args, "webhook delivery <delivery-id>")
		if err != nil {
			return err
		}
		var d store.Delivery
		if ok, err := a.call("GET", fmt.Sprintf("/deliveries/%d", id), nil, &d, true); !ok {
			return err
		}
		a.printDelivery(&d)
		return nil
	}
	return fmt.Errorf("%w: unknown webhook subcommand %q", errUsage, sub)
}

func oneID(args []string, usage string) (int64, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("%w: %s", errUsage, usage)
	}
	return parseID(args[0])
}

func webhookLine(h *store.Webhook) string {
	state := "enabled"
	if !h.Enabled {
		state = "disabled"
	}
	if h.HasSigningSecret {
		state += ", signed"
	}
	last := "no deliveries"
	if h.LastDeliveryAt != nil {
		last = fmt.Sprintf("%d deliveries, last %s", h.DeliveryCount, ago(*h.LastDeliveryAt))
	}
	return fmt.Sprintf("%-4d %-24s [%s] %s", h.ID, h.Name, state, last)
}

func (a *app) printWebhook(h *store.Webhook) {
	a.printf("%s\n", webhookLine(h))
	if h.Instructions != "" {
		a.printf("Instructions: %s\n", h.Instructions)
	}
}

func (a *app) webhookCommand(args []string, method, suffix string, body any, auth bool) error {
	id, err := oneID(args, "expected exactly one webhook id")
	if err != nil {
		return err
	}
	var h store.Webhook
	if ok, err := a.call(method, fmt.Sprintf("/webhooks/%d%s", id, suffix), body, &h, auth); !ok {
		return err
	}
	a.printWebhook(&h)
	return nil
}

func (a *app) webhookSecretCommand(args []string, method, suffix, heading string) error {
	id, err := oneID(args, "expected exactly one webhook id")
	if err != nil {
		return err
	}
	var h webhookSecret
	if ok, err := a.call(method, fmt.Sprintf("/webhooks/%d%s", id, suffix), nil, &h, true); !ok {
		return err
	}
	if heading != "" {
		a.printf("%s\n", heading)
	}
	a.printf("%s\n", h.URL)
	return nil
}

func (a *app) webhookAdd(args []string) error {
	fs := flag.NewFlagSet("webhook add", flag.ContinueOnError)
	instructions := fs.String("instructions", "", "what agents should do when it fires (plain text)")
	secret := fs.String("signing-secret", "", "GitHub webhook secret (verifies X-Hub-Signature-256)")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return fmt.Errorf("%w: webhook add <name> --instructions \"...\"", errUsage)
	}
	var h webhookSecret
	req := map[string]string{"name": strings.Join(pos, " "), "instructions": *instructions, "signing_secret": *secret}
	if ok, err := a.call("POST", "/webhooks", req, &h, true); !ok {
		return err
	}
	a.printWebhook(&h.Webhook)
	a.printf("\nReceive URL (keep it secret; `unison webhook url %d` shows it again):\n  %s\n", h.ID, h.URL)
	a.printf("\nGitHub: Settings → Webhooks → Add webhook. Payload URL: the URL above;\n")
	a.printf("content type: application/json; secret: ")
	if h.HasSigningSecret {
		a.printf("the --signing-secret you gave.\n")
	} else {
		a.printf("none (add one with `unison webhook edit %d --signing-secret S`).\n", h.ID)
	}
	return nil
}

func (a *app) webhookEdit(args []string) error {
	fs := flag.NewFlagSet("webhook edit", flag.ContinueOnError)
	patch := map[string]any{}
	fs.Func("name", "new name", func(v string) error { patch["name"] = v; return nil })
	fs.Func("instructions", "new instructions", func(v string) error { patch["instructions"] = v; return nil })
	fs.Func("signing-secret", "new signing secret, or none to remove it", func(v string) error {
		if strings.EqualFold(v, "none") {
			v = ""
		}
		patch["signing_secret"] = v
		return nil
	})
	fs.BoolFunc("enable", "accept deliveries", func(string) error { patch["enabled"] = true; return nil })
	fs.BoolFunc("disable", "reject deliveries (404)", func(string) error { patch["enabled"] = false; return nil })
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	return a.webhookCommand(pos, "PATCH", "", patch, true)
}

func (a *app) printDelivery(d *store.Delivery) {
	a.printf("Delivery %d  %s\n", d.ID, d.Summary)
	a.printf("Webhook   %d\n", d.WebhookID)
	a.printf("Received  %s\n", time.UnixMilli(d.ReceivedAt).Format(time.RFC3339))
	keys := make([]string, 0, len(d.Headers))
	for k := range d.Headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		a.printf("%s: %s\n", k, d.Headers[k])
	}
	size := fmt.Sprintf("%d bytes", d.Size)
	if d.Truncated {
		size += fmt.Sprintf(", truncated to %d", len(*d.Payload))
	}
	a.printf("Payload   (%s)\n\n", size)
	if d.Payload == nil {
		return
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, []byte(*d.Payload), "", "  ") == nil {
		a.printf("%s\n", pretty.String())
	} else {
		a.printf("%s\n", *d.Payload)
	}
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
	case e.Type == "comment.added":
		var ce store.CommentEvent
		if json.Unmarshal(e.Data, &ce) != nil || ce.Comment == nil {
			return false
		}
		mine := ce.Item != nil && ce.Item.AssigneeID != nil && *ce.Item.AssigneeID == f.me.ID
		return !f.mentionsOnly || mentions(ce.Comment.Body, f.me.Name) || mine
	case e.Type == "comment.deleted", strings.HasPrefix(e.Type, "pr."):
		return !f.mentionsOnly
	case strings.HasPrefix(e.Type, "backlog."):
		if !f.mentionsOnly {
			return true
		}
		var it store.Item
		return json.Unmarshal(e.Data, &it) == nil && e.Type == "backlog.assigned" && it.AssigneeID != nil && *it.AssigneeID == f.me.ID
	case e.Type == "webhook.received":
		// Webhooks are instructions for agents: they wake even --mentions,
		// except GitHub's setup ping.
		var d store.DeliveryEvent
		return json.Unmarshal(e.Data, &d) == nil && d.Event != "ping"
	case e.Type == "participant.joined", e.Type == "workspace.updated", strings.HasPrefix(e.Type, "repo."),
		strings.HasPrefix(e.Type, "webhook."):
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
	case strings.HasPrefix(e.Type, "comment."):
		var ce store.CommentEvent
		json.Unmarshal(e.Data, &ce)
		if ce.Comment == nil || ce.Item == nil {
			break
		}
		if e.Type == "comment.deleted" {
			return fmt.Sprintf("💬 %s deleted a comment on #%d %q", who, ce.Item.ID, ce.Item.Title)
		}
		return fmt.Sprintf("💬 %s commented on #%d %q (answer with `unison backlog comment %d <text>`): %s",
			who, ce.Item.ID, ce.Item.Title, ce.Item.ID, ce.Comment.Body)
	case strings.HasPrefix(e.Type, "pr."):
		var it store.Item
		json.Unmarshal(e.Data, &it)
		verb := map[string]string{"pr.linked": "linked a PR to", "pr.unlinked": "unlinked a PR from"}[e.Type]
		return fmt.Sprintf("🔗 %s %s #%d %q", who, verb, it.ID, it.Title)
	case e.Type == "webhook.received":
		var d store.DeliveryEvent
		json.Unmarshal(e.Data, &d)
		line := fmt.Sprintf("🪝 Webhook %q (#%d) received %s — delivery %d.", d.WebhookName, d.WebhookID, d.Summary, d.DeliveryID)
		if d.Instructions != "" {
			line += " Instructions: " + d.Instructions
			if !strings.HasSuffix(d.Instructions, ".") {
				line += "."
			}
		}
		return line + fmt.Sprintf(" Details: unison webhook delivery %d", d.DeliveryID)
	case strings.HasPrefix(e.Type, "webhook."):
		var h store.Webhook
		json.Unmarshal(e.Data, &h)
		return fmt.Sprintf("🪝 %s %s webhook %q (#%d)", who, strings.TrimPrefix(e.Type, "webhook."), h.Name, h.ID)
	case strings.HasPrefix(e.Type, "repo."):
		var r store.Repo
		json.Unmarshal(e.Data, &r)
		return fmt.Sprintf("🛠  %s %s repository %s", who, strings.TrimPrefix(e.Type, "repo."), repoLine(r))
	}
	return fmt.Sprintf("%s by %s", e.Type, who)
}
