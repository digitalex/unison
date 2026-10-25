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

// claim links the participant in .unison.json to your account.
func (a *app) claim(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: claim takes no arguments (run it where you joined)", errUsage)
	}
	if err := a.needToken(); err != nil {
		return err
	}
	userToken, err := a.needUserToken()
	if err != nil {
		return err
	}
	path, err := a.wsPath("/claim")
	if err != nil {
		return err
	}
	var raw json.RawMessage
	if err := newClient(a.server, userToken).do("POST", path, map[string]string{"participant_token": a.token}, &raw); err != nil {
		return err
	}
	if a.jsonOut {
		return a.printJSON(raw)
	}
	var p store.Participant
	if err := json.Unmarshal(raw, &p); err != nil {
		return err
	}
	a.printf("%s %s is now linked to your account (user %d).\n", p.Emoji, p.Name, p.UserID)
	return nil
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
		a.printf("No members yet (people become members by joining while signed in, or with `unison claim`).\n")
	}
	for _, mem := range m.Members {
		a.printf("user %d  %s: %s\n", mem.UserID, firstNonEmpty(mem.DisplayName, "(no name)"), participantList(mem.Participants))
	}
	if len(m.Unlinked) > 0 {
		a.printf("Not linked to a member: %s\n", participantList(m.Unlinked))
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
