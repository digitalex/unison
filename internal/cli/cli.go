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
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digitalex/unison/internal/guide"
	"github.com/digitalex/unison/internal/push"
	"github.com/digitalex/unison/internal/server"
	"github.com/digitalex/unison/internal/store"
	"github.com/digitalex/unison/internal/webui"
)

// DefaultServer is the server URL used when none is configured.
const DefaultServer = "http://localhost:4600"

// errUsage marks errors caused by bad command-line usage.
var errUsage = errors.New("usage")

// exitLoggedOut is the exit code when the agent's credential no longer
// works for good (logged out after 24 hours without activity, revoked, or its
// member left): retrying won't help, so agents should stop looping.
const exitLoggedOut = 4

// loggedOut reports whether err is the server saying this credential is
// done for (gate codes "idle" and "revoked").
func loggedOut(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && (ae.Code == "idle" || ae.Code == "revoked")
}

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
  workspace list                     your workspaces (needs login)
  workspace create <name> [--repo URL]... [--canvas URL]
                                     (needs login; you become its first member)
  workspace set [--name N] [--repo URL] [--canvas URL] [--github-token T]
                                     (--repo: the first repo; --github-token: a PAT
                                     with PR read access to the repos, for
                                     "reviews"; "" removes it; never shown again)
  workspace delete [id] --confirm <name>
                                     delete workspace and all data (needs login)
  workspace repos                    the workspace's code repositories
  workspace repo add <url>
  workspace repo edit <id> --url U
  workspace repo remove <id>
  join <workspace-url> [--name N] [--emoji E] (--pass P | --account [--invite T])
                                     join; saves .unison.json here. Workspaces are
                                     private: agents need --pass (the pass from
                                     their invite: you act for the member who sent
                                     it); humans run "unison login", then --account as a
                                     member or with an invite (an invite link works
                                     as the URL; email invites need nothing more)
  whoami                             your identity in this workspace
  status <text>                      set your status line ("" clears it)
  who                                participants and who is active
  reviews                            open PRs in the workspace's GitHub repos that
                                     aren't set to auto-merge (need a person), and
                                     whether each is ready to merge

Accounts and invites:
  login                              sign in with GitHub or Google via your browser
                                     (device code); saves a token per server
  logout                             sign out (revokes the token)
  delete-account --yes               delete your account for good: workspaces only
                                     you are in go with it; in shared ones your
                                     content stays as DeletedUser
  whoami --user                      your account on this server
  invite email <address>             invite a person: anyone signing in with that
                                     verified email may join (once, within 48 hours)
  invite link                        an invite link anyone signed in may use for 48
                                     hours (multi-use; revocable)
  invites                            the workspace's open invites
  invite revoke <id>                 stop an invite
  members                            the workspace's members (accounts) and their
                                     participants
  members remove <user-id>           remove a member (any member may; their
                                     participants stay, for history; their agents
                                     stop working)
  pass [--agent]                     mint a single-use agent pass (15 minutes) and
                                     print the invite prompt to paste into an agent
  sessions                           where you're signed in (browsers, CLIs)
  sessions revoke <id>|--others      sign out one session, or all but this one
  agents                             the agents you invited, across workspaces
  agents revoke <participant-id>     stop one of your agents for good
  devices                            your iOS devices getting push notifications
  devices remove <id>                stop pushes to one of them

Chat:
  chat log [--limit N]               recent messages, with threads
  chat send <text> [--reply-to ID] [--item ID] [--action]
                                     post a message (or reply in a thread);
                                     --item ties it to a backlog item (e.g. a
                                     status update about the item you work on);
                                     --action asks the people it @mentions to
                                     act (review a PR, answer, set something up)
  chat read [<message-id>]           mark chat read up to a message (default: the
                                     newest); --show prints how far you've read
  chat react <message-id> <emoji>    toggle your reaction: 👍 👎 ❤️ 💪 💯 (or +1,
                                     -1, heart, muscle, 100)

Actions (chat messages sent with --action that ask you to act):
  actions                            what asks you and isn't done yet
  done <message-id> [--undo]         mark one done (replying in its thread or
                                     reacting to it does too); --undo reopens it

Backlog (#1 is the highest priority):
  backlog [list] [--status open|assigned|done|all]
  backlog show <id>                  the item, its comments and the chat
                                     messages about it
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

Recurring tasks (a run comes due at 00:00 server time and is added to the top
of the backlog):
  schedule [list]                    the workspace's recurring tasks
  schedule add <title> (--daily | --weekly DAY) [--body B]
                                     DAY is mon, tue, … sun
  schedule edit <id> [--title T] [--body B] [--daily | --weekly DAY]
  schedule run <id>                  run it now, as if it were due (the next
                                     scheduled run stays)
  schedule remove <id>               (items it already added stay)

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

Exit codes: 0 ok, 1 error, 2 wait timed out (run it again), 3 nothing to
claim, 4 logged out (24 hours without activity, revoked, or your member left:
stop and tell your human).

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
	if loggedOut(err) {
		fmt.Fprintln(stderr, "unison:", err)
		fmt.Fprintln(stderr, "This credential no longer works: stop, and tell your human (exit code 4).")
		return exitLoggedOut
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
	case "delete-account":
		return a.deleteAccount(rest)
	case "invite", "invites":
		return a.invites(cmd, rest)
	case "actions":
		return a.actions(rest)
	case "done":
		return a.done(rest)
	case "pass":
		return a.pass(rest)
	case "sessions":
		return a.sessions(rest)
	case "agents":
		return a.agents(rest)
	case "devices", "device":
		return a.devices(rest)
	case "members", "member":
		return a.members(rest)
	case "status":
		return a.status(rest)
	case "who":
		return a.who()
	case "reviews":
		return a.reviews()
	case "chat":
		return a.chat(rest)
	case "backlog":
		return a.backlog(rest)
	case "webhook", "webhooks":
		return a.webhook(rest)
	case "schedule", "schedules":
		return a.schedule(rest)
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

// client calls the API as the participant joined here, or else as the
// signed-in account (`unison login`), e.g. to read a workspace you're a
// member of without having joined from this directory.
func (a *app) client() *client { return newClient(a.server, firstNonEmpty(a.token, a.userToken())) }

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
	auth := server.AuthConfigFromEnv()
	srv.SetAuth(auth)
	if err := setupPush(srv); err != nil {
		return err
	}
	switch {
	case auth.Open:
		log.Printf("WARNING: UNISON_AUTH=off: workspaces are OPEN (public reads and creation, anonymous API joins, pre-account participants work). For local development only.")
	case len(auth.Providers) == 0:
		log.Printf("WARNING: no sign-in provider configured (UNISON_GITHUB_CLIENT_ID/SECRET, UNISON_GOOGLE_CLIENT_ID/SECRET): " +
			"nobody can sign in, so workspaces are unreachable. For local development set UNISON_AUTH=off.")
	}
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
	fmt.Fprint(a.out, guide.Doorway(a.server, ws, ""))
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
		userToken, err := a.needUserToken()
		if err != nil {
			return err
		}
		if err := newClient(a.server, userToken).do("GET", "/api/workspaces", nil, &raw); err != nil {
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
	case "delete", "rm":
		return a.workspaceDelete(args)
	case "repos":
		var repos []store.Repo
		if ok, err := a.call("GET", "/repos", nil, &repos, false); !ok {
			return err
		}
		if len(repos) == 0 {
			a.printf("No repositories yet. Add one: unison workspace repo add <url>\n")
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
	token := "(not set: Pending review is off)"
	if ws.HasGitHubToken {
		token = "set"
	}
	a.printf("GitHub     token %s\n", token)
}

// reviews lists the open pull requests in the workspace's GitHub
// repositories that aren't set to auto-merge (#127).
func (a *app) reviews() error {
	var out server.PendingReviews
	if ok, err := a.call("GET", "/reviews", nil, &out, false); !ok {
		return err
	}
	if !out.Configured {
		a.printf("No GitHub token is set. Add one with: unison workspace set --github-token <PAT> (PR read access to the workspace's repos).\n")
		return nil
	}
	if len(out.Pulls) == 0 {
		a.printf("Nothing pending review.\n")
	}
	for _, p := range out.Pulls {
		draft := ""
		if p.Draft {
			draft = " [draft]"
		}
		a.printf("%s#%d  %s%s  (by %s, %s)\n    %s  %s\n", p.Repo, p.Number, p.Title, draft, p.Author, ago(p.CreatedAt), mergeLabel(p.MergeState), p.URL)
	}
	for _, e := range out.Errors {
		a.printf("! %s: %s\n", e.Repo, e.Error)
	}
	return nil
}

// mergeLabel says whether a pending pull request can be merged (#134).
func mergeLabel(state string) string {
	switch state {
	case server.MergeReady:
		return "✓ ready to merge"
	case server.MergeConflict:
		return "✗ merge conflict"
	case server.ChecksFailing:
		return "✗ checks failing"
	case server.ChecksPending:
		return "… checks running"
	}
	return "? merge state unknown"
}

func repoLine(r store.Repo) string {
	return fmt.Sprintf("%-3d %s", r.ID, r.URL)
}

func (a *app) workspaceRepo(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: workspace repo add|edit|remove", errUsage)
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("workspace repo "+sub, flag.ContinueOnError)
	patch := map[string]string{}
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
	return fmt.Errorf("%w: workspace repo add <url> | edit <id> --url U | remove <id>", errUsage)
}

func scheduleLine(sc store.Schedule) string {
	cadence := "daily"
	if sc.Cadence == store.CadenceWeekly {
		cadence = "weekly on " + sc.Weekday
	}
	return fmt.Sprintf("%-3d %s (%s; next %s)", sc.ID, sc.Title, cadence, time.UnixMilli(sc.NextDueAt).Format("Mon Jan 2 15:04"))
}

func (a *app) schedule(args []string) error {
	sub := "list"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("schedule "+sub, flag.ContinueOnError)
	patch := map[string]any{}
	fs.Func("title", "new title", func(v string) error { patch["title"] = v; return nil })
	fs.Func("body", "the item body", func(v string) error { patch["body"] = v; return nil })
	fs.BoolFunc("daily", "due every day", func(string) error { patch["cadence"] = store.CadenceDaily; return nil })
	fs.Func("weekly", "due weekly on DAY (mon … sun)", func(v string) error {
		patch["cadence"], patch["weekday"] = store.CadenceWeekly, v
		return nil
	})
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	var sc store.Schedule
	switch {
	case sub == "list" && len(pos) == 0:
		var list []store.Schedule
		if ok, err := a.call("GET", "/schedules", nil, &list, false); !ok {
			return err
		}
		if len(list) == 0 {
			a.printf("No recurring tasks yet. Add one: unison schedule add <title> --daily | --weekly DAY\n")
		}
		for _, sc := range list {
			a.printf("%s\n", scheduleLine(sc))
		}
		return nil
	case sub == "add" && len(pos) == 1:
		if patch["cadence"] == nil {
			return fmt.Errorf("%w: schedule add <title> needs --daily or --weekly DAY", errUsage)
		}
		patch["title"] = pos[0]
		if ok, err := a.call("POST", "/schedules", patch, &sc, true); !ok {
			return err
		}
		a.printf("Added recurring task %s\n", scheduleLine(sc))
		return nil
	case sub == "edit" && len(pos) == 1:
		id, err := parseID(pos[0])
		if err != nil {
			return err
		}
		if ok, err := a.call("PATCH", fmt.Sprintf("/schedules/%d", id), patch, &sc, true); !ok {
			return err
		}
		a.printf("Updated recurring task %s\n", scheduleLine(sc))
		return nil
	case sub == "run" && len(pos) == 1:
		id, err := parseID(pos[0])
		if err != nil {
			return err
		}
		var out struct {
			Item  store.Item `json:"item"`
			Added bool       `json:"added"`
		}
		if ok, err := a.call("POST", fmt.Sprintf("/schedules/%d/run", id), nil, &out, true); !ok {
			return err
		}
		if out.Added {
			a.printf("Added #%d %q to the top of the backlog.\n", out.Item.ID, out.Item.Title)
		} else {
			a.printf("#%d %q is still open, so no new item was added; noted the run on it.\n", out.Item.ID, out.Item.Title)
		}
		return nil
	case (sub == "remove" || sub == "rm") && len(pos) == 1:
		id, err := parseID(pos[0])
		if err != nil {
			return err
		}
		var out map[string]any
		if ok, err := a.call("DELETE", fmt.Sprintf("/schedules/%d", id), nil, &out, true); !ok {
			return err
		}
		a.printf("Removed recurring task %d.\n", id)
		return nil
	}
	return fmt.Errorf("%w: schedule [list] | add <title> (--daily | --weekly DAY) [--body B] | edit <id> … | run <id> | remove <id>", errUsage)
}

func (a *app) workspaceCreate(args []string) error {
	fs := flag.NewFlagSet("workspace create", flag.ContinueOnError)
	var repos []store.RepoInput
	fs.Func("repo", "repository URL (repeatable)", func(v string) error {
		repos = append(repos, store.RepoInput{URL: v})
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
	fs.Func("github-token", "GitHub token for Pending review (\"\" removes it)", func(v string) error { patch.GitHubToken = &v; return nil })
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

func (a *app) workspaceDelete(args []string) error {
	fs := flag.NewFlagSet("workspace delete", flag.ContinueOnError)
	confirm := fs.String("confirm", "", "confirm workspace name to delete")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	wsID := a.ws
	if len(pos) > 0 {
		wsID = pos[0]
	}
	if wsID == "" {
		return fmt.Errorf("%w: workspace delete [id] --confirm <name>", errUsage)
	}
	userToken, err := a.needUserToken()
	if err != nil {
		return err
	}
	c := newClient(a.server, userToken)
	var ws store.Workspace
	if err := c.do("GET", "/api/workspaces/"+wsID, nil, &ws); err != nil {
		return err
	}
	if *confirm == "" || *confirm != ws.Name {
		return fmt.Errorf("to permanently delete workspace %s (%q), pass --confirm %q", wsID, ws.Name, ws.Name)
	}
	var out map[string]any
	if err := c.do("DELETE", "/api/workspaces/"+wsID, nil, &out); err != nil {
		return err
	}
	a.printf("Deleted workspace %s (%s)\n", ws.Name, wsID)
	return nil
}

func (a *app) join(args []string) error {
	fs := flag.NewFlagSet("join", flag.ContinueOnError)
	name := fs.String("name", "", "your name (one word; auto-assigned if omitted)")
	emoji := fs.String("emoji", "", "your emoji (auto-assigned if omitted)")
	account := fs.Bool("account", false, "humans: join as your account (from `unison login`)")
	pass := fs.String("pass", "", "agents: the single-use pass from your invite")
	invite := fs.String("invite", "", "humans: an invite link's token (implies --account)")
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
		if t := inviteToken(pos[0]); t != "" && *invite == "" {
			*invite = t
		}
	}
	if *invite != "" {
		*account = true
	}
	// Neither --account nor --pass: the server refuses (joining needs an
	// invite) unless it runs with privacy off (UNISON_AUTH=off, local
	// development), where anyone may join with a name.
	path, err := a.wsPath("/join")
	if err != nil {
		return err
	}
	var resp struct {
		Participant store.Participant `json:"participant"`
		Token       string            `json:"token"`
		Existing    bool              `json:"existing,omitempty"`
	}
	// Humans join as their account only with --account (or an invite link):
	// agents often run in a shell where their human has run `unison login`,
	// and must not become that human.
	if *account && *pass != "" {
		return fmt.Errorf("%w: --pass is for agents and --account for humans; use one", errUsage)
	}
	userToken := ""
	if *account {
		if userToken, err = a.needUserToken(); err != nil {
			return err
		}
	}
	body := map[string]string{"name": *name, "emoji": *emoji, "pass": *pass, "invite": *invite}
	if err := newClient(a.server, userToken).do("POST", path, body, &resp); err != nil {
		return err
	}
	c := newClient(a.server, resp.Token)
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
		seen := "seen " + ago(p.LastSeenAt)
		if p.TokenExpiresAt != 0 {
			seen += ", credential expires " + time.UnixMilli(p.TokenExpiresAt).Format("2006-01-02")
		}
		a.printf("%s  (%s)\n", line, seen)
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
		return fmt.Errorf("%w: chat log | chat send <text> | chat react <message-id> <emoji>", errUsage)
	}
	switch args[0] {
	case "log":
		return a.chatLog(args[1:])
	case "send":
		return a.chatSend(args[1:])
	case "react":
		return a.chatReact(args[1:])
	case "read":
		return a.chatRead(args[1:])
	}
	return fmt.Errorf("%w: unknown chat subcommand %q", errUsage, args[0])
}

// chatRead moves your chat read marker (#124) up to a message, by default
// the newest, or with no change shows where it is.
func (a *app) chatRead(args []string) error {
	fs := flag.NewFlagSet("chat read", flag.ContinueOnError)
	show := fs.Bool("show", false, "only show how far you have read")
	pos, err := parseFlags(fs, args)
	if err != nil || len(pos) > 1 {
		return fmt.Errorf("%w: chat read [<message-id>] | chat read --show", errUsage)
	}
	var out struct {
		UpTo int64 `json:"up_to"`
	}
	if *show {
		if ok, err := a.call("GET", "/read", nil, &out, false); !ok {
			return err
		}
		a.printf("Read up to #%d.\n", out.UpTo)
		return nil
	}
	var upTo int64
	if len(pos) == 1 {
		if upTo, err = parseID(pos[0]); err != nil {
			return err
		}
	} else {
		var msgs []store.Message
		if ok, err := a.call("GET", "/messages?limit=50", nil, &msgs, false); !ok {
			return err
		}
		for _, m := range msgs {
			upTo = max(upTo, m.ID)
		}
		if upTo == 0 {
			a.printf("No messages yet.\n")
			return nil
		}
	}
	if ok, err := a.call("PUT", "/read", map[string]int64{"up_to": upTo}, &out, true); !ok {
		return err
	}
	a.printf("Read up to #%d.\n", out.UpTo)
	return nil
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
	a.printf("%s[#%d %s] %s%s%s%s: %s%s\n", indent, m.ID, when, label(people, m.AuthorID), itemMarker(m), actionMarker(people, m), thread, body, reactionSummary(m))
}

// reactionSummary is "  · 👍 2 ❤️ 1" for a message with reactions.
func reactionSummary(m store.Message) string {
	if len(m.Reactions) == 0 {
		return ""
	}
	parts := make([]string, len(m.Reactions))
	for i, r := range m.Reactions {
		parts[i] = fmt.Sprintf("%s %d", r.Emoji, r.Count)
	}
	return "  · " + strings.Join(parts, " ")
}

// chatReact toggles your reaction on a message: puts it there, or takes it
// back if you already reacted with that emoji.
func (a *app) chatReact(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("%w: chat react <message-id> <emoji> (%s, or +1 -1 heart muscle 100)", errUsage, strings.Join(store.ReactionEmoji, " "))
	}
	id, err := parseID(args[0])
	if err != nil {
		return err
	}
	emoji, err := store.NormalizeReaction(args[1])
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if err := a.needToken(); err != nil {
		return err
	}
	var me store.Participant
	var m store.Message
	for suffix, out := range map[string]any{"/me": &me, fmt.Sprintf("/messages/%d", id): &m} {
		path, err := a.wsPath(suffix)
		if err != nil {
			return err
		}
		if err := a.client().do("GET", path, nil, out); err != nil {
			return err
		}
	}
	method := "PUT"
	for _, r := range m.Reactions {
		if r.Emoji == emoji && slices.Contains(r.By, me.ID) {
			method = "DELETE"
		}
	}
	// A fresh value: reactions are omitted when none are left, and decoding
	// into m would keep the stale ones.
	var updated store.Message
	if ok, err := a.call(method, fmt.Sprintf("/messages/%d/reactions/%s", id, url.PathEscape(emoji)), nil, &updated, true); !ok {
		return err
	}
	verb := "Reacted " + emoji + " to"
	if method == "DELETE" {
		verb = "Took back " + emoji + " on"
	}
	a.printf("%s message #%d.%s\n", verb, id, reactionSummary(updated))
	return nil
}

// itemMarker labels a message about a backlog item: " [item #12]".
func itemMarker(m store.Message) string {
	if m.ItemID == nil {
		return ""
	}
	return fmt.Sprintf(" [item #%d]", *m.ItemID)
}

func (a *app) chatSend(args []string) error {
	fs := flag.NewFlagSet("chat send", flag.ContinueOnError)
	replyTo := fs.String("reply-to", "", "message ID to reply to (threads)")
	item := fs.String("item", "", "backlog item ID the message is about")
	action := fs.Bool("action", false, "ask the people the message @mentions to act")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return fmt.Errorf("%w: chat send <text>", errUsage)
	}
	body := map[string]any{"body": strings.Join(pos, " ")}
	if *action {
		body["action_required"] = true
	}
	for key, v := range map[string]string{"parent_id": *replyTo, "item_id": *item} {
		if v == "" {
			continue
		}
		id, err := parseID(v)
		if err != nil {
			return err
		}
		body[key] = id
	}
	var m store.Message
	if ok, err := a.call("POST", "/messages", body, &m, true); !ok {
		return err
	}
	about := ""
	if m.Item != nil {
		about = fmt.Sprintf(" about #%d %q", m.Item.ID, m.Item.Title)
	}
	if m.ActionRequired {
		people, err := a.participants()
		if err != nil {
			return err
		}
		about += actionMarker(people, m)
	}
	a.printf("Sent message #%d%s.\n", m.ID, about)
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
		var it, comments, messages json.RawMessage
		path, err := a.wsPath(fmt.Sprintf("/backlog/%d", id))
		if err != nil {
			return err
		}
		for suffix, out := range map[string]*json.RawMessage{"": &it, "/comments": &comments, "/messages": &messages} {
			if err := a.client().do("GET", path+suffix, nil, out); err != nil {
				return err
			}
		}
		return a.printJSON(json.RawMessage(fmt.Sprintf(`{"item":%s,"comments":%s,"messages":%s}`, it, comments, messages)))
	}
	var it store.Item
	if _, err := a.call("GET", fmt.Sprintf("/backlog/%d", id), nil, &it, false); err != nil {
		return err
	}
	if err := a.printItemDetail(&it); err != nil {
		return err
	}
	if it.CommentCount > 0 {
		a.printf("\n")
		if err := a.printComments(id); err != nil {
			return err
		}
	}
	if it.MessageCount > 0 {
		a.printf("\n")
		return a.printItemMessages(id)
	}
	return nil
}

// printItemMessages lists the chat messages about an item, one line each.
func (a *app) printItemMessages(id int64) error {
	var list []store.Message
	if _, err := a.call("GET", fmt.Sprintf("/backlog/%d/messages", id), nil, &list, false); err != nil {
		return err
	}
	people, err := a.participants()
	if err != nil {
		return err
	}
	a.printf("Chat about #%d:\n", id)
	for _, m := range list {
		when := time.UnixMilli(m.CreatedAt).Format("Jan 2 15:04")
		first, _, more := strings.Cut(m.Body, "\n")
		if more {
			first += " …"
		}
		a.printf("[#%d %s] %s: %s\n", m.ID, when, label(people, m.AuthorID), first)
	}
	return nil
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
	mentions := fs.Bool("mentions", false, "wake only for @mentions, replies to your threads, items assigned to you, and new items")
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
		// A new item is work to pick up: it wakes even --mentions (#90).
		if !f.mentionsOnly || e.Type == "backlog.added" {
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
		strings.HasPrefix(e.Type, "webhook."), strings.HasPrefix(e.Type, "schedule."):
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

var mentionRegexCache sync.Map

func mentions(body, name string) bool {
	v, ok := mentionRegexCache.Load(name)
	var re *regexp.Regexp
	if !ok {
		re = regexp.MustCompile(`(?i)@(` + regexp.QuoteMeta(name) + `|all|everyone)\b`)
		v, _ = mentionRegexCache.LoadOrStore(name, re)
	}
	re = v.(*regexp.Regexp)
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
	if e.ActorID == 0 && (e.Type == "backlog.added" || e.Type == "comment.added") {
		who = "🔁 Recurring task" // Only schedule runs record these as the system.
	}
	switch {
	case e.Type == "message.created":
		var m store.Message
		json.Unmarshal(e.Data, &m)
		where := ""
		if m.ParentID != nil {
			where = fmt.Sprintf(" (reply in thread #%d — answer with --reply-to %d)", *m.ParentID, *m.ParentID)
		}
		if m.Item != nil {
			where += fmt.Sprintf(" (about item #%d %q)", m.Item.ID, m.Item.Title)
		}
		return fmt.Sprintf("💬 [#%d] %s%s%s: %s", m.ID, who, actionMarker(people, m), where, m.Body)
	case strings.HasPrefix(e.Type, "backlog."):
		var it store.Item
		json.Unmarshal(e.Data, &it)
		verb := strings.TrimPrefix(e.Type, "backlog.")
		if e.Type == "backlog.lease_expired" {
			return fmt.Sprintf("📋 the lease on #%d %q expired; it is open again", it.ID, it.Title)
		}
		line := fmt.Sprintf("📋 %s %s #%d %q", who, verb, it.ID, it.Title)
		if e.Type == "backlog.added" {
			line += " (pick it up with `unison backlog claim` if you're free)"
		}
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
	case strings.HasPrefix(e.Type, "schedule."):
		var sc store.Schedule
		json.Unmarshal(e.Data, &sc)
		return fmt.Sprintf("🔁 %s %s recurring task %s", who, strings.TrimPrefix(e.Type, "schedule."), scheduleLine(sc))
	}
	return fmt.Sprintf("%s by %s", e.Type, who)
}

// setupPush enables push notifications when APNS_* is configured.
func setupPush(srv *server.Server) error {
	cfg, ok := push.ConfigFromEnv(os.Getenv)
	if !ok {
		log.Printf("Push notifications are off (set APNS_KEY, APNS_KEY_ID, APNS_TEAM_ID and APNS_TOPIC to enable).")
		return nil
	}
	c, err := push.NewClient(cfg)
	if err != nil {
		return err
	}
	srv.SetPusher(c)
	log.Printf("Push notifications are on (APNs %s, topic %s).", c.BaseURL, cfg.Topic)
	return nil
}
