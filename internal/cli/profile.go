package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/digitalex/unison/internal/store"
)

// Profile commands (the web's /me page): your sign-in sessions and the
// agents you invited, across workspaces. They use the `unison login` token.

// userCall calls an account endpoint with the saved user token. In --json
// mode it prints the response and returns ok=false.
func (a *app) userCall(method, path string, body, out any) (ok bool, err error) {
	token, err := a.needUserToken()
	if err != nil {
		return false, err
	}
	var raw json.RawMessage
	if err := newClient(a.server, token).do(method, path, body, &raw); err != nil {
		return false, err
	}
	if a.jsonOut {
		return false, a.printJSON(raw)
	}
	return true, json.Unmarshal(raw, out)
}

type sessionLine struct {
	store.Session
	Current bool `json:"current"`
}

func (a *app) sessions(args []string) error {
	if len(args) > 0 && args[0] == "revoke" {
		return a.sessionsRevoke(args[1:])
	}
	if len(args) > 0 {
		return fmt.Errorf("%w: sessions [revoke <id>|--others]", errUsage)
	}
	var list []sessionLine
	if ok, err := a.userCall("GET", "/api/me/sessions", nil, &list); !ok {
		return err
	}
	for _, s := range list {
		mark := ""
		if s.Current {
			mark = "  (this one)"
		}
		a.printf("%-4d %-8s %s — last seen %s, signed in %s%s\n", s.ID, s.Kind, firstNonEmpty(s.Label, "(no label)"), ago(s.LastSeenAt), ago(s.CreatedAt), mark)
	}
	return nil
}

func (a *app) sessionsRevoke(args []string) error {
	if len(args) == 1 && args[0] == "--others" {
		var out struct {
			Revoked int `json:"revoked"`
		}
		if ok, err := a.userCall("POST", "/api/me/sessions/revoke-others", nil, &out); !ok {
			return err
		}
		a.printf("Signed out %d other session(s).\n", out.Revoked)
		return nil
	}
	if len(args) != 1 {
		return fmt.Errorf("%w: sessions revoke <id>|--others", errUsage)
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("%w: session id must be a number (see `unison sessions`)", errUsage)
	}
	var out map[string]any
	if ok, err := a.userCall("DELETE", fmt.Sprintf("/api/me/sessions/%d", id), nil, &out); !ok {
		return err
	}
	a.printf("Signed out session %d.\n", id)
	return nil
}

func (a *app) agents(args []string) error {
	if len(args) > 0 && args[0] == "revoke" {
		if len(args) != 2 {
			return fmt.Errorf("%w: agents revoke <participant-id>", errUsage)
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(args[1], "#"), 10, 64)
		if err != nil {
			return fmt.Errorf("%w: participant id must be a number (see `unison agents`)", errUsage)
		}
		var p store.Participant
		if ok, err := a.userCall("POST", fmt.Sprintf("/api/me/agents/%d/revoke", id), nil, &p); !ok {
			return err
		}
		a.printf("Revoked %s %s (participant %d): its credential no longer works.\n", p.Emoji, p.Name, p.ID)
		return nil
	}
	if len(args) > 0 {
		return fmt.Errorf("%w: agents [revoke <participant-id>]", errUsage)
	}
	var list []store.Agent
	if ok, err := a.userCall("GET", "/api/me/agents", nil, &list); !ok {
		return err
	}
	if len(list) == 0 {
		a.printf("No agents yet. Invite one with `unison pass` in a workspace.\n")
	}
	for _, ag := range list {
		state := "credential expires " + time.UnixMilli(ag.TokenExpiresAt).Format("2006-01-02")
		if ag.RevokedAt != 0 {
			state = "revoked " + time.UnixMilli(ag.RevokedAt).Format("2006-01-02")
		}
		a.printf("%-4d %s %s in %s (%s) — seen %s, %s\n", ag.ID, ag.Emoji, ag.Name, ag.WorkspaceName, ag.WorkspaceID, ago(ag.LastSeenAt), state)
	}
	return nil
}

// devices lists the iOS devices registered for push notifications, or
// removes one. (The app registers them; there is no `devices add`.)
func (a *app) devices(args []string) error {
	if len(args) > 0 && args[0] == "remove" {
		if len(args) != 2 {
			return fmt.Errorf("%w: devices remove <id>", errUsage)
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(args[1], "#"), 10, 64)
		if err != nil {
			return fmt.Errorf("%w: device id must be a number (see `unison devices`)", errUsage)
		}
		var out map[string]any
		if ok, err := a.userCall("DELETE", fmt.Sprintf("/api/me/devices/%d", id), nil, &out); !ok {
			return err
		}
		a.printf("Removed device %d: it gets no more push notifications.\n", id)
		return nil
	}
	if len(args) > 0 {
		return fmt.Errorf("%w: devices [remove <id>]", errUsage)
	}
	var list []store.Device
	if ok, err := a.userCall("GET", "/api/me/devices", nil, &list); !ok {
		return err
	}
	if len(list) == 0 {
		a.printf("No devices. The Unison iOS app registers one when you allow notifications.\n")
	}
	for _, d := range list {
		used := "never notified"
		if d.LastUsedAt != 0 {
			used = "last notified " + ago(d.LastUsedAt)
		}
		a.printf("%-4d %s — registered %s, %s\n", d.ID, firstNonEmpty(d.Label, "(no label)"), ago(d.CreatedAt), used)
	}
	return nil
}
