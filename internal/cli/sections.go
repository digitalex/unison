package cli

import (
	"flag"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/digitalex/unison/internal/store"
)

func (a *app) section(args []string) error {
	if err := dispatcherHelp("section", args); err != nil {
		return err
	}
	sub := "ls"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "ls", "list":
		return a.sectionList(args)
	case "add":
		return a.sectionAdd(args)
	case "edit":
		return a.sectionEdit(args)
	case "move":
		return a.sectionMove(args)
	case "delete", "rm", "remove":
		return a.sectionDelete(args)
	}
	return fmt.Errorf("%w: unknown section subcommand %q", errUsage, sub)
}

func sectionLine(sec *store.Section) string {
	flags := "icon: " + sec.Icon
	if sec.Notify {
		flags += ", notify"
	}
	return fmt.Sprintf("%-3d #%-3d %-28s (%s)  [%s]", sec.Position, sec.ID, sec.Name, sec.Slug, flags)
}

func (a *app) sectionList(args []string) error {
	fs := flag.NewFlagSet("section ls", flag.ContinueOnError)
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fmt.Errorf("%w: section [ls]", errUsage)
	}
	var list []store.Section
	if ok, err := a.call("GET", "/sections", nil, &list, false); !ok {
		return err
	}
	if len(list) == 0 {
		a.printf("No custom sections configured. Add one: unison section add <name> [--icon ICON] [--notify]\n")
		return nil
	}
	for i := range list {
		a.printf("%s\n", sectionLine(&list[i]))
	}
	return nil
}

func (a *app) sectionAdd(args []string) error {
	fs := flag.NewFlagSet("section add", flag.ContinueOnError)
	slug := fs.String("slug", "", "URL-safe lowercase slug (default: derived from name)")
	icon := fs.String("icon", "", "Lucide icon name ("+strings.Join(store.SectionIcons, ", ")+")")
	notify := fs.Bool("notify", false, "surface open human-assigned items in Actionable / TODOs")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return fmt.Errorf("%w: section add <name> [--slug S] [--icon ICON] [--notify]", errUsage)
	}
	req := store.SectionInput{
		Name:   strings.Join(pos, " "),
		Slug:   *slug,
		Icon:   *icon,
		Notify: *notify,
	}
	var sec store.Section
	if ok, err := a.call("POST", "/sections", req, &sec, true); !ok {
		return err
	}
	a.printf("Added section %s\n", sectionLine(&sec))
	return nil
}

func (a *app) sectionEdit(args []string) error {
	fs := flag.NewFlagSet("section edit", flag.ContinueOnError)
	var patch store.SectionPatch
	var changed bool
	fs.Func("name", "new section name", func(v string) error {
		patch.Name = &v
		changed = true
		return nil
	})
	fs.Func("slug", "new section slug", func(v string) error {
		patch.Slug = &v
		changed = true
		return nil
	})
	fs.Func("icon", "new Lucide icon name", func(v string) error {
		patch.Icon = &v
		changed = true
		return nil
	})
	fs.BoolFunc("notify", "enable Actionable notifications", func(string) error {
		t := true
		patch.Notify = &t
		changed = true
		return nil
	})
	fs.BoolFunc("no-notify", "disable Actionable notifications", func(string) error {
		f := false
		patch.Notify = &f
		changed = true
		return nil
	})
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 || !changed {
		return fmt.Errorf("%w: section edit <id-or-slug> [--name N] [--slug S] [--icon ICON] [--notify|--no-notify]", errUsage)
	}
	ref := strings.TrimPrefix(strings.TrimSpace(pos[0]), "#")
	var sec store.Section
	if ok, err := a.call("PATCH", "/sections/"+url.PathEscape(ref), patch, &sec, true); !ok {
		return err
	}
	a.printf("Updated section %s\n", sectionLine(&sec))
	return nil
}

func (a *app) sectionMove(args []string) error {
	args, err := positionalArgs("section move", args)
	if err != nil {
		return err
	}
	if len(args) != 2 {
		return fmt.Errorf("%w: section move <id-or-slug> <position>", errUsage)
	}
	ref := strings.TrimPrefix(strings.TrimSpace(args[0]), "#")
	position, err := strconv.Atoi(args[1])
	if err != nil {
		return fmt.Errorf("%w: position must be a number", errUsage)
	}
	var sec store.Section
	if ok, err := a.call("POST", "/sections/"+url.PathEscape(ref)+"/move", map[string]int{"position": position}, &sec, true); !ok {
		return err
	}
	a.printf("Moved section %s\n", sectionLine(&sec))
	return nil
}

func (a *app) sectionDelete(args []string) error {
	args, err := positionalArgs("section delete", args)
	if err != nil {
		return err
	}
	if len(args) != 1 {
		return fmt.Errorf("%w: section delete <id-or-slug>", errUsage)
	}
	ref := strings.TrimPrefix(strings.TrimSpace(args[0]), "#")
	var out struct {
		Deleted int64  `json:"deleted"`
		Slug    string `json:"slug"`
	}
	if ok, err := a.call("DELETE", "/sections/"+url.PathEscape(ref), nil, &out, true); !ok {
		return err
	}
	a.printf("Deleted section %s (#%d); remaining items moved to Backlog.\n", out.Slug, out.Deleted)
	return nil
}

func (a *app) backlogAnswer(args []string) error {
	fs := flag.NewFlagSet("backlog answer", flag.ContinueOnError)
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return fmt.Errorf("%w: backlog answer <id> <choice>", errUsage)
	}
	id, err := parseID(pos[0])
	if err != nil {
		return err
	}
	choice := strings.Join(pos[1:], " ")
	var it store.Item
	if ok, err := a.call("POST", fmt.Sprintf("/backlog/%d/answer", id), map[string]string{"choice": choice}, &it, true); !ok {
		return err
	}
	return a.printItemDetail(&it)
}
