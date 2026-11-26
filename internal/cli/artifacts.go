package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/digitalex/unison/internal/store"
)

// artifact manages workspace artifacts (#918): design docs, HTML prototypes,
// reports, and screenshots served at /w/{ws}/artifacts/{file}.
func (a *app) artifact(args []string) error {
	sub := "ls"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "ls", "list":
		return a.artifactList(args)
	case "show":
		return a.artifactShow(args)
	case "add", "upload":
		return a.artifactAdd(args)
	case "save":
		return a.artifactSave(args)
	case "update", "replace":
		return a.artifactUpdate(args)
	case "delete", "rm":
		return a.artifactDelete(args)
	}
	return fmt.Errorf("%w: unknown artifact subcommand %q", errUsage, sub)
}

func (a *app) stdin() io.Reader {
	if a.in != nil {
		return a.in
	}
	return os.Stdin
}

func formatArtifactBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func (a *app) artifactList(args []string) error {
	fs := flag.NewFlagSet("artifact ls", flag.ContinueOnError)
	itemRaw := fs.String("item", "", "filter by attached backlog item id")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fmt.Errorf("%w: artifact ls [--item ID]", errUsage)
	}
	suffix := "/artifacts"
	if strings.TrimSpace(*itemRaw) != "" {
		id, err := parseID(*itemRaw)
		if err != nil || id <= 0 {
			return fmt.Errorf("%w: --item must be a positive item id", errUsage)
		}
		suffix += "?item_id=" + strconv.FormatInt(id, 10)
	}
	var list []store.Artifact
	if ok, err := a.call("GET", suffix, nil, &list, false); !ok {
		return err
	}
	if len(list) == 0 {
		a.printf("No artifacts yet. Upload one: unison artifact add <path>\n")
		return nil
	}
	for _, art := range list {
		itemTag := ""
		if art.ItemID != nil {
			itemTag = fmt.Sprintf("  [item #%d]", artifactItemNum(&art))
		}
		titlePart := ""
		if art.Title != "" && art.Title != art.Name {
			titlePart = fmt.Sprintf("  %q", art.Title)
		}
		who := firstNonEmpty(art.UpdaterName, art.UploaderName)
		byPart := ""
		if who != "" {
			byPart = " by " + who
		}
		a.printf("%-28s  v%-2d  %9s%s%s  (%s%s)  %s\n",
			art.Name, art.Version, formatArtifactBytes(art.Size), titlePart, itemTag, ago(art.UpdatedAt), byPart, art.URL)
	}
	return nil
}

type artifactDetail struct {
	store.Artifact
	Content *string `json:"content,omitempty"`
}

func artifactItemNum(art *store.Artifact) int64 {
	if art.ItemNumber > 0 {
		return art.ItemNumber
	}
	if art.Item != nil && art.Item.Number > 0 {
		return art.Item.Number
	}
	if art.ItemID != nil {
		return *art.ItemID
	}
	return 0
}

func (a *app) artifactShow(args []string) error {
	fs := flag.NewFlagSet("artifact show", flag.ContinueOnError)
	showContent := fs.Bool("content", false, "include UTF-8 text content")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("%w: artifact show <file-or-url> [--content]", errUsage)
	}
	path, err := a.resolveArtifactDownloadPath(pos[0])
	if err != nil {
		return err
	}
	q := url.Values{"meta": {"1"}}
	if *showContent {
		q.Set("content", "1")
	}
	path += "?" + q.Encode()
	var raw json.RawMessage
	if err := a.client().do("GET", path, nil, &raw); err != nil {
		return err
	}
	if a.jsonOut {
		return a.printJSON(raw)
	}
	var art artifactDetail
	if err := json.Unmarshal(raw, &art); err != nil {
		return err
	}
	a.printArtifactDetail(&art, *showContent)
	return nil
}

func (a *app) printArtifactDetail(art *artifactDetail, showContent bool) {
	a.printf("%s  (v%d)\n", art.Name, art.Version)
	if art.Title != "" && art.Title != art.Name {
		a.printf("Title     %s\n", art.Title)
	}
	a.printf("Type      %s\n", art.MimeType)
	a.printf("Size      %s\n", formatArtifactBytes(art.Size))
	a.printf("SHA-256   %s\n", art.SHA256)
	if art.ItemID != nil {
		num := artifactItemNum(&art.Artifact)
		if art.Item != nil && art.Item.Title != "" {
			a.printf("Item      #%d %s\n", num, art.Item.Title)
		} else {
			a.printf("Item      #%d\n", num)
		}
	}
	if art.UploadedBy != nil {
		a.printf("Uploaded  by %s, %s\n", todoPersonLabel(art.UploaderEmoji, art.UploaderName, *art.UploadedBy), ago(art.CreatedAt))
	} else if art.CreatedAt > 0 {
		a.printf("Uploaded  %s\n", ago(art.CreatedAt))
	}
	if art.Version > 1 || art.UpdatedAt > art.CreatedAt {
		if art.UpdatedBy != nil {
			a.printf("Updated   by %s, %s\n", todoPersonLabel(art.UpdaterEmoji, art.UpdaterName, *art.UpdatedBy), ago(art.UpdatedAt))
		} else if art.UpdatedAt > 0 {
			a.printf("Updated   %s\n", ago(art.UpdatedAt))
		}
	}
	a.printf("URL       %s\n", art.URL)
	if showContent && art.Content != nil {
		a.printf("\n%s", *art.Content)
		if !strings.HasSuffix(*art.Content, "\n") {
			a.printf("\n")
		}
	}
}

func (a *app) readArtifactSource(src string) ([]byte, error) {
	var data []byte
	var err error
	if src == "-" {
		data, err = io.ReadAll(a.stdin())
	} else {
		data, err = os.ReadFile(src)
	}
	if err != nil {
		return nil, err
	}
	if len(data) > store.MaxArtifactBytes {
		return nil, fmt.Errorf("%s is %.1f MiB; artifacts can be at most %d MiB", src, float64(len(data))/(1<<20), store.MaxArtifactBytes>>20)
	}
	return data, nil
}

func (a *app) artifactAdd(args []string) error {
	fs := flag.NewFlagSet("artifact add", flag.ContinueOnError)
	name := fs.String("name", "", "artifact filename (required when reading stdin '-')")
	title := fs.String("title", "", "optional human-readable title")
	itemRaw := fs.String("item", "", "optional attached backlog item id")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("%w: artifact add <path> [--name FILE] [--title T] [--item ID]", errUsage)
	}
	src := pos[0]
	if src == "-" && strings.TrimSpace(*name) == "" {
		return fmt.Errorf("%w: --name is required when reading from stdin (-)", errUsage)
	}
	data, err := a.readArtifactSource(src)
	if err != nil {
		return err
	}
	artName := strings.TrimSpace(*name)
	if artName == "" {
		artName = filepath.Base(src)
	}
	if _, err := store.ValidArtifactName(artName); err != nil {
		return err
	}
	q := url.Values{}
	q.Set("name", artName)
	if strings.TrimSpace(*title) != "" {
		q.Set("title", strings.TrimSpace(*title))
	}
	if strings.TrimSpace(*itemRaw) != "" {
		id, err := parseID(*itemRaw)
		if err != nil || id <= 0 {
			return fmt.Errorf("%w: --item must be a positive item id", errUsage)
		}
		q.Set("item_id", strconv.FormatInt(id, 10))
	}
	if err := a.needToken(); err != nil {
		return err
	}
	path, err := a.wsPath("/artifacts?" + q.Encode())
	if err != nil {
		return err
	}
	raw, err := a.client().raw("POST", path, "application/octet-stream", bytes.NewReader(data))
	if err != nil {
		return err
	}
	if a.jsonOut {
		return a.printJSON(raw)
	}
	var art store.Artifact
	if err := json.Unmarshal(raw, &art); err != nil {
		return err
	}
	a.printf("%s\n", art.URL)
	return nil
}

var markdownLinkTargetRe = regexp.MustCompile(`^\[[^\]]*\]\(([^)]+)\)$`)

func unwrapArtifactRef(raw string) string {
	s := strings.TrimSpace(raw)
	if m := markdownLinkTargetRe.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	return s
}

func extractArtifactName(raw string) (string, error) {
	s := unwrapArtifactRef(raw)
	if strings.HasPrefix(s, "/w/") || strings.HasPrefix(s, "/api/workspaces/") || strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		u, err := url.Parse(s)
		if err == nil {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) == 4 && parts[0] == "w" && parts[2] == "artifacts" {
				return store.ValidArtifactName(parts[3])
			}
			if len(parts) == 5 && parts[0] == "api" && parts[1] == "workspaces" && parts[3] == "artifacts" {
				return store.ValidArtifactName(parts[4])
			}
		}
	}
	return store.ValidArtifactName(s)
}

func (a *app) resolveArtifactDownloadPath(raw string) (string, error) {
	s := unwrapArtifactRef(raw)
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		u, err := url.Parse(s)
		if err != nil || u.Host == "" {
			return "", fmt.Errorf("%w: invalid artifact URL %q", errUsage, raw)
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		switch {
		case len(parts) == 4 && parts[0] == "w" && parts[2] == "artifacts":
			name, err := store.ValidArtifactName(parts[3])
			if err != nil {
				return "", err
			}
			return u.Scheme + "://" + u.Host + store.ArtifactRawURL(parts[1], name), nil
		case len(parts) == 5 && parts[0] == "api" && parts[1] == "workspaces" && parts[3] == "artifacts":
			name, err := store.ValidArtifactName(parts[4])
			if err != nil {
				return "", err
			}
			return u.Scheme + "://" + u.Host + store.ArtifactRawURL(parts[2], name), nil
		default:
			return "", fmt.Errorf("%w: %q is not an artifact URL (/w/<ws>/artifacts/<name> or /api/workspaces/<ws>/artifacts/<name>)", errUsage, raw)
		}
	}
	if strings.HasPrefix(s, "/w/") || strings.HasPrefix(s, "/api/workspaces/") {
		u, err := url.Parse(s)
		if err != nil {
			return "", fmt.Errorf("%w: invalid artifact path %q", errUsage, raw)
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		switch {
		case len(parts) == 4 && parts[0] == "w" && parts[2] == "artifacts":
			name, err := store.ValidArtifactName(parts[3])
			if err != nil {
				return "", err
			}
			return store.ArtifactRawURL(parts[1], name), nil
		case len(parts) == 5 && parts[0] == "api" && parts[1] == "workspaces" && parts[3] == "artifacts":
			name, err := store.ValidArtifactName(parts[4])
			if err != nil {
				return "", err
			}
			return store.ArtifactRawURL(parts[2], name), nil
		default:
			return "", fmt.Errorf("%w: %q is not an artifact path", errUsage, raw)
		}
	}
	name, err := store.ValidArtifactName(s)
	if err != nil {
		return "", fmt.Errorf("%w: %v", errUsage, err)
	}
	return a.wsPath("/artifacts/" + url.PathEscape(name))
}

func (a *app) artifactSave(args []string) error {
	pos, err := parseFlags(flag.NewFlagSet("artifact save", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return fmt.Errorf("%w: artifact save <file-or-url> <dest>", errUsage)
	}
	path, err := a.resolveArtifactDownloadPath(pos[0])
	if err != nil {
		return err
	}
	data, err := a.client().raw("GET", path, "", nil)
	if err != nil {
		return err
	}
	if pos[1] == "-" {
		_, err = a.out.Write(data)
		return err
	}
	if err := os.WriteFile(pos[1], data, 0o644); err != nil {
		return err
	}
	a.printf("Saved %s (%d bytes).\n", pos[1], len(data))
	return nil
}

func parseArtifactItemFlag(v string) (int64, error) {
	s := strings.TrimSpace(v)
	if strings.EqualFold(s, "none") || s == "0" {
		return 0, nil
	}
	id, err := parseID(s)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("expected item id or none")
	}
	return id, nil
}

func (a *app) artifactUpdate(args []string) error {
	fs := flag.NewFlagSet("artifact update", flag.ContinueOnError)
	var titleVal string
	hasTitle := false
	var itemIDVal int64
	hasItem := false
	fs.Func("title", "new title (\"\" resets to filename)", func(v string) error {
		titleVal, hasTitle = v, true
		return nil
	})
	fs.Func("item", "attached backlog item id, or none/0 to unlink", func(v string) error {
		parsed, err := parseArtifactItemFlag(v)
		if err != nil {
			return err
		}
		itemIDVal, hasItem = parsed, true
		return nil
	})
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 1 || len(pos) > 2 {
		return fmt.Errorf("%w: artifact update <name> [<path>] [--title T] [--item ID]", errUsage)
	}
	name, err := extractArtifactName(pos[0])
	if err != nil {
		return err
	}
	if err := a.needToken(); err != nil {
		return err
	}

	if len(pos) == 2 {
		data, err := a.readArtifactSource(pos[1])
		if err != nil {
			return err
		}
		q := url.Values{}
		if hasTitle {
			q.Set("title", titleVal)
		}
		if hasItem {
			q.Set("item_id", strconv.FormatInt(itemIDVal, 10))
		}
		suffix := "/artifacts/" + url.PathEscape(name)
		if encoded := q.Encode(); encoded != "" {
			suffix += "?" + encoded
		}
		path, err := a.wsPath(suffix)
		if err != nil {
			return err
		}
		raw, err := a.client().raw("PUT", path, "application/octet-stream", bytes.NewReader(data))
		if err != nil {
			return err
		}
		if a.jsonOut {
			return a.printJSON(raw)
		}
		var art store.Artifact
		if err := json.Unmarshal(raw, &art); err != nil {
			return err
		}
		a.printf("%s  (v%d)\n", art.URL, art.Version)
		return nil
	}

	if !hasTitle && !hasItem {
		return fmt.Errorf("%w: artifact update <name> [<path>] [--title T] [--item ID]", errUsage)
	}
	patch := map[string]any{}
	if hasTitle {
		patch["title"] = titleVal
	}
	if hasItem {
		patch["item_id"] = itemIDVal
	}
	var art store.Artifact
	if ok, err := a.call("PATCH", "/artifacts/"+url.PathEscape(name), patch, &art, true); !ok {
		return err
	}
	a.printf("%s  (v%d)\n", art.URL, art.Version)
	return nil
}

func (a *app) artifactDelete(args []string) error {
	pos, err := parseFlags(flag.NewFlagSet("artifact delete", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("%w: artifact delete <name>", errUsage)
	}
	name, err := extractArtifactName(pos[0])
	if err != nil {
		return err
	}
	var res map[string]any
	if ok, err := a.call("DELETE", "/artifacts/"+url.PathEscape(name), nil, &res, true); !ok {
		return err
	}
	a.printf("Deleted artifact %s.\n", name)
	return nil
}
