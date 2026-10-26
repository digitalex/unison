package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/digitalex/unison/internal/store"
)

// Membership commands. Claiming and removing members need `unison login`;
// listing members does not.

// needUserToken returns the saved account token for the current server.
func (a *app) needUserToken() (string, error) {
	token := a.userToken()
	if token == "" {
		return "", fmt.Errorf("not signed in to %s: run `unison login` first", serverKey(a.server))
	}
	return token, nil
}

func (a *app) members(args []string) error {
	sub := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "list", "ls":
		return a.membersList()
	case "remove", "rm":
		if len(args) != 1 {
			return fmt.Errorf("%w: members remove <user-id>", errUsage)
		}
		return a.membersRemove(args[0])
	}
	return fmt.Errorf("%w: unknown members command %q", errUsage, sub)
}

func participantList(ps []store.Participant) string {
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = fmt.Sprintf("%s %s (participant %d)", p.Emoji, p.Name, p.ID)
	}
	return strings.Join(names, ", ")
}

func (a *app) membersList() error {
	var m store.Members
	if ok, err := a.call("GET", "/members", nil, &m, false); !ok {
		return err
	}
	if len(m.Members) == 0 {
		a.printf("No members yet (people become members by joining with an invite while signed in).\n")
	}
	for _, mem := range m.Members {
		a.printf("user %d  %s: %s\n", mem.UserID, firstNonEmpty(mem.DisplayName, "(no name)"), participantList(mem.Participants))
	}
	return nil
}

func (a *app) membersRemove(arg string) error {
	id, err := strconv.ParseInt(strings.TrimPrefix(arg, "user"), 10, 64)
	if err != nil {
		return errors.New("user id must be a number (see `unison members`)")
	}
	userToken, err := a.needUserToken()
	if err != nil {
		return err
	}
	path, err := a.wsPath("/members/" + strconv.FormatInt(id, 10))
	if err != nil {
		return err
	}
	var raw json.RawMessage
	if err := newClient(a.server, userToken).do("DELETE", path, nil, &raw); err != nil {
		return err
	}
	if a.jsonOut {
		return a.printJSON(raw)
	}
	a.printf("Removed user %d from the workspace's members (their participants stay, for history).\n", id)
	return nil
}

// pass mints an agent pass and prints the invite prompt that carries it.
func (a *app) pass(args []string) error {
	fs := flag.NewFlagSet("pass", flag.ContinueOnError)
	fs.Bool("agent", true, "an agent pass (the only kind)")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	userToken, err := a.needUserToken()
	if err != nil {
		return err
	}
	path, err := a.wsPath("/passes")
	if err != nil {
		return err
	}
	var raw json.RawMessage
	if err := newClient(a.server, userToken).do("POST", path, nil, &raw); err != nil {
		return err
	}
	if a.jsonOut {
		return a.printJSON(raw)
	}
	var resp struct {
		Pass         string `json:"pass"`
		ExpiresAt    int64  `json:"expires_at"`
		InvitePrompt string `json:"invite_prompt"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return err
	}
	a.printf("Paste this into a coding agent (the pass works once, until %s):\n\n%s\n",
		time.UnixMilli(resp.ExpiresAt).Format("15:04"), resp.InvitePrompt)
	return nil
}

// invites handles `unison invite …` and `unison invites`.
func (a *app) invites(cmd string, args []string) error {
	if cmd == "invites" || len(args) == 0 || args[0] == "list" {
		return a.invitesList()
	}
	sub, args := args[0], args[1:]
	switch sub {
	case "email":
		if len(args) != 1 {
			return fmt.Errorf("%w: invite email <address>", errUsage)
		}
		var resp struct {
			Invite store.Invite `json:"invite"`
		}
		if ok, err := a.wsUserCall("POST", "/invites", map[string]string{"email": args[0]}, &resp); !ok {
			return err
		}
		a.printf("Invited %s (invite %d): they can join within 48 hours by signing in with that verified email.\n", resp.Invite.Email, resp.Invite.ID)
		return nil
	case "link":
		if len(args) > 0 {
			return fmt.Errorf("%w: invite link takes no options (every invite works for 48 hours)", errUsage)
		}
		var resp struct {
			Invite store.Invite `json:"invite"`
			URL    string       `json:"url"`
		}
		if ok, err := a.wsUserCall("POST", "/invites", map[string]any{"link": true}, &resp); !ok {
			return err
		}
		a.printf("Invite link (invite %d, works until %s; anyone signed in can use it, so share it carefully):\n%s\n",
			resp.Invite.ID, time.UnixMilli(resp.Invite.ExpiresAt).Format("2006-01-02 15:04"), resp.URL)
		return nil
	case "revoke":
		if len(args) != 1 {
			return fmt.Errorf("%w: invite revoke <id>", errUsage)
		}
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("%w: invite id must be a number (see `unison invites`)", errUsage)
		}
		var inv store.Invite
		if ok, err := a.wsUserCall("DELETE", fmt.Sprintf("/invites/%d", id), nil, &inv); !ok {
			return err
		}
		a.printf("Revoked invite %d.\n", id)
		return nil
	}
	return fmt.Errorf("%w: invite email <address> | invite link | invites | invite revoke <id>", errUsage)
}

func (a *app) invitesList() error {
	var list []store.Invite
	if ok, err := a.call("GET", "/invites", nil, &list, false); !ok {
		return err
	}
	if len(list) == 0 {
		a.printf("No open invites. Invite someone: unison invite email <address>, or unison invite link\n")
	}
	for _, i := range list {
		what := i.Email
		if i.Kind == store.InviteLink {
			what = fmt.Sprintf("link, used %d time(s)", i.Uses)
		}
		if i.ExpiresAt != 0 {
			what += ", expires " + time.UnixMilli(i.ExpiresAt).Format("2006-01-02 15:04")
		}
		a.printf("%-4d %-5s %s (by %s, %s)\n", i.ID, i.Kind, what, firstNonEmpty(i.CreatorName, "a member"), ago(i.CreatedAt))
	}
	return nil
}

// wsUserCall calls a workspace endpoint that needs your account (`unison
// login`). In --json mode it prints the response and returns ok=false.
func (a *app) wsUserCall(method, suffix string, body, out any) (bool, error) {
	path, err := a.wsPath(suffix)
	if err != nil {
		return false, err
	}
	return a.userCall(method, path, body, out)
}
