package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/digitalex/unison/internal/store"
)

// ---- feedback (#281) ----

func (a *app) feedback(args []string) error {
	sub := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "list", "ls":
		return a.feedbackList(args)
	case "show":
		return a.feedbackShow(args)
	case "add":
		return a.feedbackAdd(args)
	case "edit":
		return a.feedbackEdit(args)
	case "promote":
		return a.feedbackPromote(args)
	case "dismiss":
		return a.feedbackSetStatus(args, "dismiss", "Dismissed")
	case "reopen":
		return a.feedbackSetStatus(args, "reopen", "Reopened")
	case "delete", "rm":
		id, err := oneID(args, "feedback rm <id>")
		if err != nil {
			return err
		}
		var out map[string]any
		if ok, err := a.call("DELETE", fmt.Sprintf("/feedback/%d", id), nil, &out, true); !ok {
			return err
		}
		a.printf("Deleted feedback %d.\n", id)
		return nil
	case "enable":
		return a.feedbackToggle(args, true)
	case "disable":
		return a.feedbackToggle(args, false)
	}
	return fmt.Errorf("%w: unknown feedback subcommand %q", errUsage, sub)
}

func (a *app) feedbackToggle(args []string, enabled bool) error {
	if _, err := parseFlags(flag.NewFlagSet("feedback", flag.ContinueOnError), args); err != nil {
		return err
	}
	var ws store.Workspace
	if ok, err := a.call("PATCH", "", store.WorkspacePatch{FeedbackEnabled: &enabled}, &ws, true); !ok {
		return err
	}
	if ws.FeedbackEnabled {
		a.printf("Enabled Send feedback & triage in workspace %s.\n", ws.Name)
	} else {
		a.printf("Disabled Send feedback & triage in workspace %s.\n", ws.Name)
	}
	return nil
}

func (a *app) feedbackList(args []string) error {
	fs := flag.NewFlagSet("feedback list", flag.ContinueOnError)
	status := fs.String("status", "", "open, promoted, dismissed or all (default: open)")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	var list []store.Feedback
	if ok, err := a.call("GET", "/feedback?status="+url.QueryEscape(*status), nil, &list, false); !ok {
		return err
	}
	people, err := a.participants()
	if err != nil {
		return err
	}
	if len(list) == 0 {
		a.printf("No feedback reports found. Submit one with `unison feedback add <title>`.\n")
		return nil
	}
	for _, f := range list {
		a.printf("%s\n", feedbackLine(people, &f))
	}
	return nil
}

func promotedFeedbackItemNum(f *store.Feedback) int64 {
	if f.PromotedItemNumber != nil && *f.PromotedItemNumber > 0 {
		return *f.PromotedItemNumber
	}
	if f.PromotedItemID != nil {
		return *f.PromotedItemID
	}
	return 0
}

func feedbackState(f *store.Feedback) string {
	switch f.Status {
	case store.FeedbackPromoted:
		if f.PromotedItemID != nil {
			return fmt.Sprintf("[moved to backlog #%d]", promotedFeedbackItemNum(f))
		}
		return "[moved to backlog]"
	case store.FeedbackDismissed:
		return "[dismissed]"
	default:
		return "[open]"
	}
}

func feedbackLine(people map[int64]store.Participant, f *store.Feedback) string {
	line := fmt.Sprintf("feedback %-4d [%-10s %-8s] %-36s %s  by %s",
		f.ID, f.Kind, f.Severity, f.Title, feedbackState(f), label(people, f.CreatedBy))
	if f.ImageURL != "" {
		line += "  [screenshot]"
	}
	return line
}

func (a *app) printFeedbackDetail(f *store.Feedback) error {
	people, err := a.participants()
	if err != nil {
		return err
	}
	a.printf("Feedback %d  %s\n", f.ID, f.Title)
	a.printf("Type      %s (%s)\n", f.Kind, f.Severity)
	a.printf("State     %s\n", feedbackState(f))
	a.printf("Created   by %s, %s\n", label(people, f.CreatedBy), ago(f.CreatedAt))
	if f.PageURL != "" {
		a.printf("Page      %s\n", f.PageURL)
	}
	if f.ImageURL != "" {
		a.printf("Image     %s\n", f.ImageURL)
	}
	if f.PromotedBy != nil && f.PromotedAt != nil {
		a.printf("Moved     by %s, %s (unison backlog show %d)\n", label(people, *f.PromotedBy), ago(*f.PromotedAt), promotedFeedbackItemNum(f))
	}
	if f.Body != "" {
		a.printf("\n%s\n", f.Body)
	}
	return nil
}

func (a *app) feedbackShow(args []string) error {
	id, err := oneID(args, "feedback show <id>")
	if err != nil {
		return err
	}
	var f store.Feedback
	if ok, err := a.call("GET", fmt.Sprintf("/feedback/%d", id), nil, &f, false); !ok {
		return err
	}
	return a.printFeedbackDetail(&f)
}

func (a *app) resolveFeedbackImageArg(val string) (string, error) {
	val = strings.TrimSpace(val)
	if val == "" || val == "none" || val == "-" {
		return "", nil
	}
	if strings.HasPrefix(val, "/api/workspaces/") || strings.HasPrefix(val, "http://") || strings.HasPrefix(val, "https://") {
		return val, nil
	}
	data, err := os.ReadFile(val)
	if err != nil {
		return "", err
	}
	if len(data) > store.MaxImageBytes {
		return "", fmt.Errorf("%s is %.1f MiB; images can be at most %d MiB", val, float64(len(data))/(1<<20), store.MaxImageBytes>>20)
	}
	if err := a.needToken(); err != nil {
		return "", err
	}
	path, err := a.wsPath("/images?name=" + url.QueryEscape(filepath.Base(val)))
	if err != nil {
		return "", err
	}
	raw, err := a.client().raw("POST", path, "application/octet-stream", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	var im store.Image
	if err := json.Unmarshal(raw, &im); err != nil {
		return "", err
	}
	return im.URL, nil
}

func (a *app) feedbackAdd(args []string) error {
	fs := flag.NewFlagSet("feedback add", flag.ContinueOnError)
	body := fs.String("body", "", "details (Markdown)")
	kind := fs.String("type", store.FeedbackBug, "bug or suggestion")
	fs.StringVar(kind, "kind", store.FeedbackBug, "bug or suggestion")
	severity := fs.String("severity", store.SeverityMedium, "low, medium, high or critical")
	image := fs.String("image", "", "local image file path or URL")
	page := fs.String("page", "", "page route or URL where feedback occurred")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return fmt.Errorf("%w: feedback add <title> [--body B] [--type bug|suggestion] [--severity low|medium|high|critical] [--image FILE_OR_URL] [--page URL]", errUsage)
	}
	var imageURL string
	if *image != "" {
		if imageURL, err = a.resolveFeedbackImageArg(*image); err != nil {
			return err
		}
	}
	var f store.Feedback
	req := store.FeedbackInput{
		Kind:     *kind,
		Severity: *severity,
		Title:    strings.Join(pos, " "),
		Body:     *body,
		ImageURL: imageURL,
		PageURL:  *page,
	}
	if ok, err := a.call("POST", "/feedback", req, &f, true); !ok {
		return err
	}
	return a.printFeedbackDetail(&f)
}

func (a *app) feedbackEdit(args []string) error {
	fs := flag.NewFlagSet("feedback edit", flag.ContinueOnError)
	var patch store.FeedbackPatch
	var imageArg *string
	fs.Func("title", "new title", func(v string) error { patch.Title = &v; return nil })
	fs.Func("body", "new details", func(v string) error { patch.Body = &v; return nil })
	fs.Func("type", "bug or suggestion", func(v string) error { patch.Kind = &v; return nil })
	fs.Func("kind", "bug or suggestion", func(v string) error { patch.Kind = &v; return nil })
	fs.Func("severity", "low, medium, high or critical", func(v string) error { patch.Severity = &v; return nil })
	fs.Func("image", "local image file, URL, or none", func(v string) error { imageArg = &v; return nil })
	fs.Func("page", "page route/URL or none", func(v string) error {
		if v == "none" || v == "-" {
			v = ""
		}
		patch.PageURL = &v
		return nil
	})
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	id, err := oneID(pos, "feedback edit <id> [--title T] [--body B] [--type bug|suggestion] [--severity low|medium|high|critical] [--image FILE_OR_URL|none] [--page URL|none]")
	if err != nil {
		return err
	}
	if imageArg != nil {
		resolved, err := a.resolveFeedbackImageArg(*imageArg)
		if err != nil {
			return err
		}
		patch.ImageURL = &resolved
	}
	var f store.Feedback
	if ok, err := a.call("PATCH", fmt.Sprintf("/feedback/%d", id), patch, &f, true); !ok {
		return err
	}
	return a.printFeedbackDetail(&f)
}

func (a *app) feedbackPromote(args []string) error {
	fs := flag.NewFlagSet("feedback promote", flag.ContinueOnError)
	position := fs.Int("position", 0, "priority position of the new item (1 = top; default bottom)")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	id, err := oneID(pos, "feedback promote <id> [--position N]")
	if err != nil {
		return err
	}
	var out store.FeedbackPromotion
	if ok, err := a.call("POST", fmt.Sprintf("/feedback/%d/promote", id), map[string]int{"position": *position}, &out, true); !ok {
		return err
	}
	a.printf("Moved feedback %d to the backlog as #%d.\n\n", id, itemNum(out.Item))
	return a.printItemDetail(out.Item)
}

func (a *app) feedbackSetStatus(args []string, action, past string) error {
	id, err := oneID(args, fmt.Sprintf("feedback %s <id>", action))
	if err != nil {
		return err
	}
	var f store.Feedback
	if ok, err := a.call("POST", fmt.Sprintf("/feedback/%d/%s", id, action), nil, &f, true); !ok {
		return err
	}
	a.printf("%s feedback %d.\n", past, f.ID)
	return nil
}

// describeFeedbackEvent describes a feedback.* event for `unison wait`.
func describeFeedbackEvent(who string, e store.Event) string {
	var f store.Feedback
	json.Unmarshal(e.Data, &f)
	switch e.Type {
	case "feedback.created":
		return fmt.Sprintf("📣 %s submitted %s feedback %d [%s] %q (`unison feedback show %d`)", who, f.Kind, f.ID, f.Severity, f.Title, f.ID)
	case "feedback.promoted":
		if f.PromotedItemID != nil {
			return fmt.Sprintf("📣 %s moved feedback %d %q to the backlog as #%d", who, f.ID, f.Title, promotedFeedbackItemNum(&f))
		}
	}
	return fmt.Sprintf("📣 %s %s feedback %d %q", who, strings.TrimPrefix(e.Type, "feedback."), f.ID, f.Title)
}
