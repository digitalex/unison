package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/digitalex/unison/internal/store"
)

// instructionsStdin is the reader used when --file - or --instructions-file -
// is passed; tests override it.
var instructionsStdin io.Reader = os.Stdin

func readInstructionsSource(path string) (string, error) {
	if path == "-" {
		b, err := io.ReadAll(instructionsStdin)
		if err != nil {
			return "", fmt.Errorf("read instructions from stdin: %w", err)
		}
		return string(b), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read instructions file %s: %w", path, err)
	}
	return string(b), nil
}

// resolveRepoFile finds relPath either in cwd or relative to the nearest
// enclosing repo root (.git or .hg).
func resolveRepoFile(relPath string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	direct := filepath.Join(cwd, filepath.FromSlash(relPath))
	if info, err := os.Stat(direct); err == nil && !info.IsDir() {
		return direct, nil
	}
	dir := cwd
	for {
		for _, marker := range []string{".git", ".hg"} {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				cand := filepath.Join(dir, filepath.FromSlash(relPath))
				if info, err := os.Stat(cand); err == nil && !info.IsDir() {
					return cand, nil
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("instructions file %q not found in %s or enclosing repo root", relPath, cwd)
}

func (a *app) workspaceInstructions(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "set":
			return a.workspaceInstructionsSet(args[1:])
		case "sync":
			return a.workspaceInstructionsSync(args[1:])
		}
	}
	fs := flag.NewFlagSet("workspace instructions", flag.ContinueOnError)
	headingsOnly := fs.Bool("headings", false, "print only ## headings")
	section := fs.String("section", "", "print only the named ## section")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fmt.Errorf("%w: workspace instructions [--headings | --section NAME] | set ... | sync [--path REPO_FILE]", errUsage)
	}
	if *headingsOnly && strings.TrimSpace(*section) != "" {
		return fmt.Errorf("%w: --headings and --section cannot both be set", errUsage)
	}
	endpoint := "/instructions"
	if sec := strings.TrimSpace(*section); sec != "" {
		endpoint += "?section=" + url.QueryEscape(sec)
	}
	var inst store.WorkspaceInstructions
	if ok, err := a.call("GET", endpoint, nil, &inst, false); !ok {
		return err
	}
	if *headingsOnly {
		if len(inst.Headings) == 0 {
			if inst.Instructions == "" {
				a.printf("(no workspace instructions)\n")
			} else {
				a.printf("(no ## headings; %d chars)\n", utf8.RuneCountInString(inst.Instructions))
			}
			return nil
		}
		for _, h := range inst.Headings {
			a.printf("%s\n", h)
		}
		return nil
	}
	if strings.TrimSpace(inst.Instructions) == "" {
		if inst.Path != "" {
			a.printf("(no workspace instructions yet; configured path: %s — run `unison workspace instructions sync`)\n", inst.Path)
		} else {
			a.printf("(no workspace instructions)\n")
		}
		return nil
	}
	body := inst.Instructions
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	a.printf("%s", body)
	return nil
}

func (a *app) workspaceInstructionsSet(args []string) error {
	fs := flag.NewFlagSet("workspace instructions set", flag.ContinueOnError)
	var (
		hasText  bool
		textVal  string
		filePath string
		clearVal bool
		hasPath  bool
		repoPath string
	)
	fs.Func("text", "instructions Markdown text", func(v string) error {
		hasText = true
		textVal = v
		return nil
	})
	fs.StringVar(&filePath, "file", "", "read instructions from file (- for stdin)")
	fs.BoolVar(&clearVal, "clear", false, "clear workspace instructions")
	fs.Func("path", "repo-relative instructions file path (\"\" clears)", func(v string) error {
		hasPath = true
		repoPath = v
		return nil
	})
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fmt.Errorf("%w: workspace instructions set (--text TEXT | --file PATH | --clear) [--path REPO_FILE]", errUsage)
	}
	modes := 0
	if hasText {
		modes++
	}
	if filePath != "" {
		modes++
	}
	if clearVal {
		modes++
	}
	if modes > 1 {
		return fmt.Errorf("%w: --text, --file, and --clear are mutually exclusive", errUsage)
	}
	if modes == 0 && !hasPath {
		return fmt.Errorf("%w: workspace instructions set needs --text, --file, --clear, or --path", errUsage)
	}
	var in store.InstructionsInput
	switch {
	case hasText:
		in.Instructions = &textVal
	case filePath != "":
		content, err := readInstructionsSource(filePath)
		if err != nil {
			return err
		}
		in.Instructions = &content
	case clearVal:
		empty := ""
		in.Instructions = &empty
	}
	if hasPath {
		in.Path = &repoPath
	}
	var inst store.WorkspaceInstructions
	if ok, err := a.call("PUT", "/instructions", in, &inst, true); !ok {
		return err
	}
	a.printInstructionsSummary(&inst)
	return nil
}

func (a *app) workspaceInstructionsSync(args []string) error {
	fs := flag.NewFlagSet("workspace instructions sync", flag.ContinueOnError)
	pathFlag := fs.String("path", "", "repo-relative instructions file path (defaults to workspace's instructions_path)")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fmt.Errorf("%w: workspace instructions sync [--path REPO_FILE]", errUsage)
	}
	relPath := strings.TrimSpace(*pathFlag)
	if relPath == "" {
		p, err := a.wsPath("/instructions")
		if err != nil {
			return err
		}
		var cur store.WorkspaceInstructions
		if err := a.client().do("GET", p, nil, &cur); err != nil {
			return err
		}
		relPath = strings.TrimSpace(cur.Path)
		if relPath == "" {
			return errors.New("workspace has no instructions_path set; pass --path <repo-relative-file>")
		}
	}
	cleanPath, err := store.NormalizeInstructionsPath(relPath)
	if err != nil {
		return err
	}
	fullPath, err := resolveRepoFile(cleanPath)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", fullPath, err)
	}
	content := string(b)
	in := store.InstructionsInput{
		Instructions: &content,
		Path:         &cleanPath,
	}
	var inst store.WorkspaceInstructions
	if ok, err := a.call("PUT", "/instructions", in, &inst, true); !ok {
		return err
	}
	a.printInstructionsSummary(&inst)
	return nil
}

func (a *app) printInstructionsSummary(inst *store.WorkspaceInstructions) {
	n := utf8.RuneCountInString(inst.Instructions)
	if n == 0 {
		if inst.Path != "" {
			a.printf("Cleared workspace instructions (path: %s).\n", inst.Path)
		} else {
			a.printf("Cleared workspace instructions.\n")
		}
		return
	}
	mode := fmt.Sprintf("inline on join, %d / %d chars", n, store.InstructionsInlineLimit)
	if n > store.InstructionsInlineLimit {
		mode = fmt.Sprintf("indexed by %d heading(s), %d chars", len(inst.Headings), n)
	}
	if inst.Path != "" {
		a.printf("Updated workspace instructions (%s; path: %s).\n", mode, inst.Path)
	} else {
		a.printf("Updated workspace instructions (%s).\n", mode)
	}
}
