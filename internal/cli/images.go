package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/digitalex/unison/internal/store"
)

// image uploads and downloads workspace images (#292).
func (a *app) image(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: image upload <file> [--name N] | image save <id-or-url> <dest>", errUsage)
	}
	switch args[0] {
	case "upload":
		return a.imageUpload(args[1:])
	case "save":
		return a.imageSave(args[1:])
	}
	return fmt.Errorf("%w: unknown image subcommand %q", errUsage, args[0])
}

// imageUpload sends a file's bytes to POST .../images and prints the
// Markdown to paste into a message, item or comment.
func (a *app) imageUpload(args []string) error {
	fs := flag.NewFlagSet("image upload", flag.ContinueOnError)
	name := fs.String("name", "", "the image's name (alt text); default: the file name")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("%w: image upload <file> [--name N]", errUsage)
	}
	data, err := os.ReadFile(pos[0])
	if err != nil {
		return err
	}
	if len(data) > store.MaxImageBytes {
		return fmt.Errorf("%s is %.1f MiB; images can be at most %d MiB", pos[0], float64(len(data))/(1<<20), store.MaxImageBytes>>20)
	}
	if *name == "" {
		*name = filepath.Base(pos[0])
	}
	if err := a.needToken(); err != nil {
		return err
	}
	path, err := a.wsPath("/images?name=" + url.QueryEscape(*name))
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
	var im store.Image
	if err := json.Unmarshal(raw, &im); err != nil {
		return err
	}
	a.printf("%s\n", im.Markdown)
	return nil
}

// imageRef matches an image reference: 12, #12, or a path or URL ending in
// /api/workspaces/<ws>/images/<id> (also inside ![name](…) Markdown).
var imageRef = regexp.MustCompile(`^#?(\d+)$|(?:^|\()((?:https?://[^/\s)]+)?/api/workspaces/[^/\s)]+/images/\d+)\)?$`)

// imageSave downloads an image to dest ("-" for stdout).
func (a *app) imageSave(args []string) error {
	pos, err := parseFlags(flag.NewFlagSet("image save", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return fmt.Errorf("%w: image save <id-or-url> <dest>", errUsage)
	}
	m := imageRef.FindStringSubmatch(strings.TrimSpace(pos[0]))
	if m == nil {
		return fmt.Errorf("%w: %q is not an image id or URL (12, #12, /api/workspaces/<ws>/images/12, or a full URL)", errUsage, pos[0])
	}
	path := m[2]
	if m[1] != "" {
		if path, err = a.wsPath("/images/" + m[1]); err != nil {
			return err
		}
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
