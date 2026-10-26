package cli

import (
	"flag"
	"fmt"
	"slices"
	"strings"

	"github.com/digitalex/unison/internal/store"
)

// Action-required messages (#49): `chat send --action` asks the people a
// message @mentions to act; `actions` lists what asks you; `done` marks one
// done (or, with --undo, not done).

// actions lists the messages that ask you to act and that you haven't
// marked done.
func (a *app) actions(args []string) error {
	fs := flag.NewFlagSet("actions", flag.ContinueOnError)
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	var list []store.Message
	if ok, err := a.call("GET", "/actions", nil, &list, true); !ok {
		return err
	}
	if len(list) == 0 {
		a.printf("Nothing needs you.\n")
		return nil
	}
	people, err := a.participants()
	if err != nil {
		return err
	}
	for _, m := range list {
		a.printMessage(people, m, "")
	}
	return nil
}

// done marks a message that asks you to act done, or not done with --undo.
func (a *app) done(args []string) error {
	fs := flag.NewFlagSet("done", flag.ContinueOnError)
	undo := fs.Bool("undo", false, "mark it not done again")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("%w: done <message-id> [--undo]", errUsage)
	}
	id, err := parseID(pos[0])
	if err != nil {
		return err
	}
	method, verb := "POST", "Marked message #%d done.\n"
	if *undo {
		method, verb = "DELETE", "Marked message #%d not done.\n"
	}
	var m store.Message
	if ok, err := a.call(method, fmt.Sprintf("/messages/%d/done", id), nil, &m, true); !ok {
		return err
	}
	a.printf(verb, id)
	return nil
}

// actionMarker labels a message that asks people to act:
// " [needs @Alex ✓ @Bob]", a check for each who has marked it done.
func actionMarker(people map[int64]store.Participant, m store.Message) string {
	if !m.ActionRequired {
		return ""
	}
	who := make([]string, len(m.ActionFor))
	for i, id := range m.ActionFor {
		name := fmt.Sprintf("participant %d", id)
		if p, ok := people[id]; ok {
			name = p.Name
		}
		who[i] = "@" + name
		if slices.Contains(m.DoneBy, id) {
			who[i] += " ✓"
		}
	}
	return " [needs " + strings.Join(who, " ") + "]"
}
