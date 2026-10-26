package cli

import (
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/digitalex/unison/internal/store"
)

// inbox: `inbox [--all]`, `inbox send`, `inbox dismiss|restore <id>`.
func (a *app) inbox(args []string) error {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "send":
			return a.inboxSend(args[1:])
		case "dismiss", "restore":
			return a.inboxDismiss(args[0], args[1:])
		}
		return fmt.Errorf("%w: inbox [--all] | inbox send <text> | inbox dismiss <id>", errUsage)
	}
	fs := flag.NewFlagSet("inbox", flag.ContinueOnError)
	all := fs.Bool("all", false, "include dismissed entries")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	suffix := "/inbox"
	if *all {
		suffix += "?all=1"
	}
	var list []store.InboxEntry
	if ok, err := a.call("GET", suffix, nil, &list, true); !ok {
		return err
	}
	if len(list) == 0 {
		a.printf("Nothing needs your attention.\n")
	}
	for _, e := range list {
		a.printf("%s\n", inboxLine(e))
	}
	return nil
}

// inboxLine is one entry: "[3 Jan 2 15:04] 🐦 Wren [item #12]: text  <url>".
func inboxLine(e store.InboxEntry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%d %s] %s %s", e.ID, time.UnixMilli(e.CreatedAt).Format("Jan 2 15:04"), e.SenderEmoji, e.SenderName)
	if e.ItemID != nil {
		fmt.Fprintf(&b, " [item #%d]", *e.ItemID)
	}
	fmt.Fprintf(&b, ": %s", strings.ReplaceAll(e.Body, "\n", "\n      "))
	if e.URL != "" {
		fmt.Fprintf(&b, "  %s", e.URL)
	}
	if e.DismissedAt != 0 {
		b.WriteString("  (dismissed)")
	}
	return b.String()
}

func (a *app) inboxSend(args []string) error {
	fs := flag.NewFlagSet("inbox send", flag.ContinueOnError)
	to := fs.String("to", "", "whose inbox: a participant's name (default for agents: the member you act for)")
	item := fs.String("item", "", "backlog item ID the entry is about")
	link := fs.String("url", "", "a link to act on (e.g. the PR)")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return fmt.Errorf("%w: inbox send <text> [--to NAME] [--item ID] [--url URL]", errUsage)
	}
	body := map[string]any{"body": strings.Join(pos, " "), "to": *to, "url": *link}
	if *item != "" {
		id, err := parseID(*item)
		if err != nil {
			return err
		}
		body["item_id"] = id
	}
	var e store.InboxEntry
	if ok, err := a.call("POST", "/inbox", body, &e, true); !ok {
		return err
	}
	a.printf("Sent to %s's inbox (entry %d).\n", e.RecipientName, e.ID)
	return nil
}

func (a *app) inboxDismiss(verb string, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%w: inbox %s <id>", errUsage, verb)
	}
	id, err := parseID(args[0])
	if err != nil {
		return err
	}
	var e store.InboxEntry
	if ok, err := a.call("POST", fmt.Sprintf("/inbox/%d/%s", id, verb), nil, &e, true); !ok {
		return err
	}
	a.printf("%s entry %d.\n", map[string]string{"dismiss": "Dismissed", "restore": "Restored"}[verb], id)
	return nil
}
