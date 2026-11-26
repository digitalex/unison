package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/digitalex/unison/internal/store"
)

// ---- ideas (#917) ----

// Ideas are numbered on their own, so they print as "idea N", never "#N"
// (which is a backlog item).

func (a *app) idea(args []string) error {
	if err := dispatcherHelp("idea", args); err != nil {
		return err
	}
	sub := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "list", "ls":
		return a.ideaList(args)
	case "show":
		return a.ideaShow(args)
	case "add":
		return a.ideaAdd(args)
	case "edit":
		return a.ideaEdit(args)
	case "comment":
		return a.ideaComment(args)
	case "promote":
		return a.ideaPromote(args)
	case "delete", "rm":
		args, err := positionalArgs("idea delete", args)
		if err != nil {
			return err
		}
		id, err := oneID(args, "idea delete <id>")
		if err != nil {
			return err
		}
		var out map[string]any
		if ok, err := a.call("DELETE", fmt.Sprintf("/ideas/%d", id), nil, &out, true); !ok {
			return err
		}
		a.printf("Deleted idea %d.\n", id)
		return nil
	}
	return fmt.Errorf("%w: unknown idea subcommand %q", errUsage, sub)
}

func (a *app) ideaList(args []string) error {
	fs := flag.NewFlagSet("idea list", flag.ContinueOnError)
	status := fs.String("status", "", "open, promoted or all (default: open)")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	var ideas []store.Idea
	if ok, err := a.call("GET", "/ideas?status="+*status, nil, &ideas, false); !ok {
		return err
	}
	people, err := a.participants()
	if err != nil {
		return err
	}
	if len(ideas) == 0 {
		a.printf("No ideas yet. Add one with `unison idea add <title>`.\n")
	}
	for _, d := range ideas {
		a.printf("%s\n", ideaLine(people, &d))
	}
	return nil
}

func ideaLine(people map[int64]store.Participant, d *store.Idea) string {
	line := fmt.Sprintf("idea %-4d %-40s %s  by %s", d.ID, d.Title, ideaState(d), label(people, d.CreatedBy))
	if d.CommentCount > 0 {
		line += fmt.Sprintf("  (%d comment%s)", d.CommentCount, plural(d.CommentCount))
	}
	return line
}

func promotedItemNum(d *store.Idea) int64 {
	if d.PromotedItemNumber != nil && *d.PromotedItemNumber > 0 {
		return *d.PromotedItemNumber
	}
	if d.PromotedItemID != nil {
		return *d.PromotedItemID
	}
	return 0
}

func ideaState(d *store.Idea) string {
	if d.Status == store.IdeaPromoted && d.PromotedItemID != nil {
		return fmt.Sprintf("[moved to backlog #%d]", promotedItemNum(d))
	}
	return "[open]"
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (a *app) printIdeaDetail(d *store.Idea) error {
	people, err := a.participants()
	if err != nil {
		return err
	}
	a.printf("Idea %d  %s\n", d.ID, d.Title)
	a.printf("State     %s\n", ideaState(d))
	a.printf("Created   by %s, %s\n", label(people, d.CreatedBy), ago(d.CreatedAt))
	if d.PromotedBy != nil && d.PromotedAt != nil {
		a.printf("Moved     by %s, %s (unison backlog show %d)\n", label(people, *d.PromotedBy), ago(*d.PromotedAt), promotedItemNum(d))
	}
	if d.CommentCount > 0 {
		a.printf("Comments  %d\n", d.CommentCount)
	}
	if d.Body != "" {
		a.printf("\n%s\n", d.Body)
	}
	return nil
}

// ideaShow prints an idea with its discussion.
func (a *app) ideaShow(args []string) error {
	args, err := positionalArgs("idea show", args)
	if err != nil {
		return err
	}
	id, err := oneID(args, "idea show <id>")
	if err != nil {
		return err
	}
	if a.jsonOut {
		var d, comments json.RawMessage
		path, err := a.wsPath(fmt.Sprintf("/ideas/%d", id))
		if err != nil {
			return err
		}
		for suffix, out := range map[string]*json.RawMessage{"": &d, "/comments": &comments} {
			if err := a.client().do("GET", path+suffix, nil, out); err != nil {
				return err
			}
		}
		return a.printJSON(json.RawMessage(fmt.Sprintf(`{"idea":%s,"comments":%s}`, d, comments)))
	}
	var d store.Idea
	if _, err := a.call("GET", fmt.Sprintf("/ideas/%d", id), nil, &d, false); err != nil {
		return err
	}
	if err := a.printIdeaDetail(&d); err != nil {
		return err
	}
	var list []store.IdeaComment
	if _, err := a.call("GET", fmt.Sprintf("/ideas/%d/comments", id), nil, &list, false); err != nil {
		return err
	}
	people, err := a.participants()
	if err != nil {
		return err
	}
	a.printf("\n")
	if len(list) == 0 {
		a.printf("No discussion on idea %d yet (unison idea comment %d <text>).\n", id, id)
	}
	for _, c := range list {
		when := time.UnixMilli(c.CreatedAt).Format("Jan 2 15:04")
		a.printf("[comment %d, %s] %s: %s\n", c.ID, when, label(people, c.AuthorID), strings.ReplaceAll(c.Body, "\n", "\n      "))
	}
	return nil
}

func (a *app) ideaAdd(args []string) error {
	fs := flag.NewFlagSet("idea add", flag.ContinueOnError)
	body := fs.String("body", "", "description (Markdown)")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return fmt.Errorf("%w: idea add <title> [--body B]", errUsage)
	}
	var d store.Idea
	if ok, err := a.call("POST", "/ideas", map[string]string{"title": strings.Join(pos, " "), "body": *body}, &d, true); !ok {
		return err
	}
	return a.printIdeaDetail(&d)
}

func (a *app) ideaEdit(args []string) error {
	fs := flag.NewFlagSet("idea edit", flag.ContinueOnError)
	var patch store.IdeaPatch
	fs.Func("title", "new title", func(v string) error { patch.Title = &v; return nil })
	fs.Func("body", "new description", func(v string) error { patch.Body = &v; return nil })
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	id, err := oneID(pos, "idea edit <id> [--title T] [--body B]")
	if err != nil {
		return err
	}
	var d store.Idea
	if ok, err := a.call("PATCH", fmt.Sprintf("/ideas/%d", id), patch, &d, true); !ok {
		return err
	}
	return a.printIdeaDetail(&d)
}

func (a *app) ideaComment(args []string) error {
	fs := flag.NewFlagSet("idea comment", flag.ContinueOnError)
	del := fs.String("delete", "", "comment id to delete (your own)")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 || (*del == "" && len(pos) < 2) {
		return fmt.Errorf("%w: idea comment <id> <text> | idea comment <id> --delete <comment-id>", errUsage)
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
		if ok, err := a.call("DELETE", fmt.Sprintf("/ideas/%d/comments/%d", id, cid), nil, &out, true); !ok {
			return err
		}
		a.printf("Deleted comment %d on idea %d.\n", cid, id)
		return nil
	}
	var c store.IdeaComment
	if ok, err := a.call("POST", fmt.Sprintf("/ideas/%d/comments", id), map[string]string{"body": strings.Join(pos[1:], " ")}, &c, true); !ok {
		return err
	}
	a.printf("Commented on idea %d (comment %d).\n", id, c.ID)
	return nil
}

// ideaPromote moves an idea to the backlog and prints the new item.
func (a *app) ideaPromote(args []string) error {
	fs := flag.NewFlagSet("idea promote", flag.ContinueOnError)
	position := fs.Int("position", 0, "priority position of the new item (1 = top; default bottom)")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	id, err := oneID(pos, "idea promote <id> [--position N]")
	if err != nil {
		return err
	}
	var out store.IdeaPromotion
	if ok, err := a.call("POST", fmt.Sprintf("/ideas/%d/promote", id), map[string]int{"position": *position}, &out, true); !ok {
		return err
	}
	a.printf("Moved idea %d to the backlog as #%d.\n\n", id, itemNum(out.Item))
	return a.printItemDetail(out.Item)
}

// describeIdeaEvent describes an idea.* event for `unison wait`.
func describeIdeaEvent(people map[int64]store.Participant, who string, e store.Event) string {
	if e.Type == "idea.commented" || e.Type == "idea.comment_deleted" {
		var ce store.IdeaCommentEvent
		json.Unmarshal(e.Data, &ce)
		if ce.Comment == nil || ce.Idea == nil {
			return fmt.Sprintf("%s by %s", e.Type, who)
		}
		if e.Type == "idea.comment_deleted" {
			return fmt.Sprintf("💡 %s deleted a comment on idea %d %q", who, ce.Idea.ID, ce.Idea.Title)
		}
		return fmt.Sprintf("💡 %s commented on idea %d %q (answer with `unison idea comment %d <text>`): %s",
			who, ce.Idea.ID, ce.Idea.Title, ce.Idea.ID, ce.Comment.Body)
	}
	var d store.Idea
	json.Unmarshal(e.Data, &d)
	switch e.Type {
	case "idea.added":
		return fmt.Sprintf("💡 %s added idea %d %q (discuss with `unison idea comment %d <text>`)", who, d.ID, d.Title, d.ID)
	case "idea.promoted":
		if d.PromotedItemID != nil {
			return fmt.Sprintf("💡 %s moved idea %d %q to the backlog as #%d", who, d.ID, d.Title, promotedItemNum(&d))
		}
	}
	return fmt.Sprintf("💡 %s %s idea %d %q", who, strings.TrimPrefix(e.Type, "idea."), d.ID, d.Title)
}
