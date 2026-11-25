package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/digitalex/unison/internal/store"
)

func (a *app) todo(args []string) error {
	sub := "ls"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "ls", "list":
		return a.todoList(args)
	case "add":
		return a.todoAdd(args)
	case "done":
		return a.todoDone(args)
	case "reopen":
		return a.todoReopen(args)
	case "snooze":
		return a.todoSnooze(args)
	case "delete", "rm":
		return a.todoDelete(args)
	}
	return fmt.Errorf("%w: unknown todo subcommand %q", errUsage, sub)
}

func (a *app) accountToken() (string, error) {
	if strings.HasPrefix(a.token, store.UserTokenPrefix) {
		return a.token, nil
	}
	return a.needUserToken()
}

func (a *app) todoList(args []string) error {
	fs := flag.NewFlagSet("todo ls", flag.ContinueOnError)
	status := fs.String("status", store.TodoFilterActive, "active, snoozed, open, done or all")
	mine := fs.Bool("mine", false, "only TODOs assigned to you")
	allWorkspaces := fs.Bool("all-workspaces", false, "your TODOs across all workspaces (uses `unison login`)")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fmt.Errorf("%w: todo [ls] [--status active|snoozed|open|done|all] [--mine] [--all-workspaces]", errUsage)
	}

	if *allWorkspaces {
		tok, err := a.accountToken()
		if err != nil {
			return err
		}
		path := "/api/me/todos"
		if *status != "" {
			path += "?status=" + url.QueryEscape(*status)
		}
		var raw json.RawMessage
		if err := a.clientWithToken(tok).do("GET", path, nil, &raw); err != nil {
			return err
		}
		if a.jsonOut {
			return a.printJSON(raw)
		}
		var list []store.UserTodo
		if err := json.Unmarshal(raw, &list); err != nil {
			return err
		}
		if len(list) == 0 {
			a.printf("No TODOs.\n")
			return nil
		}
		for _, ut := range list {
			a.printf("[%s (%s)]  %s\n", ut.WorkspaceName, ut.WorkspaceID, todoLine(&ut.Todo))
		}
		return nil
	}

	q := url.Values{}
	if *status != "" {
		q.Set("status", *status)
	}
	if *mine {
		q.Set("assignee", "me")
	}
	suffix := "/todos"
	if enc := q.Encode(); enc != "" {
		suffix += "?" + enc
	}
	var list []store.Todo
	if ok, err := a.call("GET", suffix, nil, &list, false); !ok {
		return err
	}
	if len(list) == 0 {
		a.printf("No TODOs.\n")
		return nil
	}
	for _, td := range list {
		a.printf("%s\n", todoLine(&td))
	}
	return nil
}

func (a *app) todoAdd(args []string) error {
	fs := flag.NewFlagSet("todo add", flag.ContinueOnError)
	to := fs.String("to", "", "human participant name to assign to (required)")
	body := fs.String("body", "", "optional details (Markdown)")
	item := fs.String("item", "", "optional backlog item ID to link")
	pr := fs.String("pr", "", "optional GitHub PR URL to link")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 || strings.TrimSpace(*to) == "" {
		return fmt.Errorf("%w: todo add <title> --to <Name> [--body B] [--item ID] [--pr URL]", errUsage)
	}
	req := map[string]any{
		"title":    strings.Join(pos, " "),
		"assignee": strings.TrimPrefix(strings.TrimSpace(*to), "@"),
	}
	if *body != "" {
		req["body"] = *body
	}
	if *item != "" {
		itemID, err := parseID(*item)
		if err != nil {
			return err
		}
		req["item_id"] = itemID
	}
	if *pr != "" {
		req["pr_url"] = *pr
	}
	var td store.Todo
	if ok, err := a.call("POST", "/todos", req, &td, true); !ok {
		return err
	}
	a.printTodoDetail(&td)
	return nil
}

func (a *app) todoDone(args []string) error {
	fs := flag.NewFlagSet("todo done", flag.ContinueOnError)
	note := fs.String("note", "", "completion note (required for agents)")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("%w: todo done <id> [--note N]", errUsage)
	}
	id, err := parseID(pos[0])
	if err != nil {
		return err
	}
	if strings.TrimSpace(*note) == "" && a.cfg != nil && a.cfg.Agent && (a.token == "" || a.token == a.cfg.Token) {
		return fmt.Errorf("%w: agents must provide --note \"...\" when marking a TODO done", errUsage)
	}
	var td store.Todo
	if ok, err := a.call("POST", fmt.Sprintf("/todos/%d/done", id), map[string]string{"note": *note}, &td, true); !ok {
		return err
	}
	a.printTodoDetail(&td)
	return nil
}

func (a *app) todoReopen(args []string) error {
	id, err := oneID(args, "todo reopen <id>")
	if err != nil {
		return err
	}
	var td store.Todo
	if ok, err := a.call("POST", fmt.Sprintf("/todos/%d/reopen", id), nil, &td, true); !ok {
		return err
	}
	a.printTodoDetail(&td)
	return nil
}

func (a *app) todoSnooze(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("%w: todo snooze <id> (1d | 1w | off)", errUsage)
	}
	id, err := parseID(args[0])
	if err != nil {
		return err
	}
	dur := strings.ToLower(strings.TrimSpace(args[1]))
	switch dur {
	case "1d", "1w":
	case "off", "none", "clear":
		dur = store.SnoozeNone
	default:
		return fmt.Errorf("%w: snooze duration must be 1d, 1w or off", errUsage)
	}
	var td store.Todo
	if ok, err := a.call("POST", fmt.Sprintf("/todos/%d/snooze", id), map[string]string{"duration": dur}, &td, true); !ok {
		return err
	}
	a.printTodoDetail(&td)
	return nil
}

func (a *app) todoDelete(args []string) error {
	id, err := oneID(args, "todo delete <id>")
	if err != nil {
		return err
	}
	var out map[string]any
	if ok, err := a.call("DELETE", fmt.Sprintf("/todos/%d", id), nil, &out, true); !ok {
		return err
	}
	a.printf("Deleted TODO #%d.\n", id)
	return nil
}

func todoPersonLabel(emoji, name string, id int64) string {
	if name != "" {
		if emoji != "" {
			return emoji + " " + name
		}
		return name
	}
	return fmt.Sprintf("participant %d", id)
}

func todoState(td *store.Todo) string {
	if td.Status == store.TodoStatusDone {
		s := "[done"
		if td.DoneByID != nil {
			s += " by " + todoPersonLabel(td.DoneByEmoji, td.DoneByName, *td.DoneByID)
		}
		if td.DoneNote != "" {
			s += ": " + td.DoneNote
		}
		return s + "]"
	}
	who := todoPersonLabel(td.AssigneeEmoji, td.AssigneeName, td.AssigneeID)
	if td.SnoozedUntil != nil && *td.SnoozedUntil > time.Now().UnixMilli() {
		return fmt.Sprintf("[snoozed → %s until %s]", who, time.UnixMilli(*td.SnoozedUntil).Format("Jan 2 15:04"))
	}
	return fmt.Sprintf("[open → %s]", who)
}

func todoItemNum(td *store.Todo) int64 {
	if td.ItemNumber > 0 {
		return td.ItemNumber
	}
	if td.Item != nil && td.Item.Number > 0 {
		return td.Item.Number
	}
	if td.ItemID != nil {
		return *td.ItemID
	}
	return td.ID
}

func todoLine(td *store.Todo) string {
	var extras []string
	rowID := td.ID
	if td.Kind == store.TodoKindSectionItem {
		rowID = todoItemNum(td)
		if td.SectionName != "" {
			extras = append(extras, fmt.Sprintf("[section: %s]", td.SectionName))
		}
		if td.AnsweredChoice != "" {
			extras = append(extras, fmt.Sprintf("[decision: %s]", td.AnsweredChoice))
		} else if len(td.Choices) > 0 {
			extras = append(extras, fmt.Sprintf("[choices: %s]", strings.Join(td.Choices, " | ")))
		}
	} else if td.ItemID != nil {
		extras = append(extras, fmt.Sprintf("[item #%d]", todoItemNum(td)))
	}
	if td.PRURL != "" {
		extras = append(extras, td.PRURL)
	}
	tail := ""
	if len(extras) > 0 {
		tail = "  " + strings.Join(extras, " ")
	}
	return fmt.Sprintf("#%-4d %-36s %s%s", rowID, td.Title, todoState(td), tail)
}

func (a *app) printTodoDetail(td *store.Todo) {
	a.printf("TODO #%d  %s\n", td.ID, td.Title)
	a.printf("State     %s\n", todoState(td))
	if td.CreatorID != nil {
		a.printf("Created   by %s, %s\n", todoPersonLabel(td.CreatorEmoji, td.CreatorName, *td.CreatorID), ago(td.CreatedAt))
	}
	if td.ItemID != nil {
		num := todoItemNum(td)
		if td.Item != nil && td.Item.Title != "" {
			a.printf("Item      #%d %s\n", num, td.Item.Title)
		} else {
			a.printf("Item      #%d\n", num)
		}
	}
	if td.PRURL != "" {
		a.printf("PR        %s\n", td.PRURL)
	}
	if td.Body != "" {
		a.printf("\n%s\n", td.Body)
	}
}
