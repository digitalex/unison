package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/digitalex/unison/internal/store"
)

// defaultsDirEnv overrides ~/.unison/defaults for tests and automation.
const defaultsDirEnv = "UNISON_DEFAULTS_DIR"

// projectDefaultSettings holds optional scalar workspace defaults loaded from
// ~/.unison/defaults/settings.json. Explicit CLI flags always win.
type projectDefaultSettings struct {
	InstructionsPath string `json:"instructions_path,omitempty"`
	RepoURL          string `json:"repo_url,omitempty"`
	CanvasURL        string `json:"canvas_url,omitempty"`
}

// defaultRoutine is one entry in ~/.unison/defaults/routines.json. Cadence
// matches POST /api/workspaces/{ws}/schedules ("hourly", "daily", "weekly");
// Schedule is accepted as a friendly alias for Cadence.
type defaultRoutine struct {
	Title    string `json:"title"`
	Body     string `json:"body,omitempty"`
	Cadence  string `json:"cadence,omitempty"`
	Schedule string `json:"schedule,omitempty"`
	Weekday  string `json:"weekday,omitempty"`
}

func (r defaultRoutine) toScheduleInput() store.ScheduleInput {
	cadence := strings.TrimSpace(r.Cadence)
	if cadence == "" {
		cadence = strings.TrimSpace(r.Schedule)
	}
	return store.ScheduleInput{
		Title:   strings.TrimSpace(r.Title),
		Body:    r.Body,
		Cadence: cadence,
		Weekday: strings.TrimSpace(r.Weekday),
	}
}

type projectDefaults struct {
	Dir             string
	Exists          bool
	HasInstructions bool
	Instructions    string
	HasSettings     bool
	Settings        projectDefaultSettings
	HasRoutines     bool
	Routines        []defaultRoutine
}

func resolveDefaultsDir() string {
	if v := strings.TrimSpace(os.Getenv(defaultsDirEnv)); v != "" {
		return v
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".unison", "defaults")
	}
	return filepath.Join(".unison", "defaults")
}

// loadProjectDefaults reads ~/.unison/defaults (or UNISON_DEFAULTS_DIR).
// Missing directory or missing files are silently ignored (100% opt-in).
// Malformed files are reported as warnings without aborting workspace create.
func loadProjectDefaults() (*projectDefaults, []string) {
	dir := resolveDefaultsDir()
	d := &projectDefaults{Dir: dir}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return d, nil
	}
	d.Exists = true
	var warnings []string

	if b, err := os.ReadFile(filepath.Join(dir, "instructions.md")); err == nil {
		if strings.TrimSpace(string(b)) != "" {
			d.HasInstructions = true
			d.Instructions = string(b)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		warnings = append(warnings, fmt.Sprintf("could not read %s: %v", filepath.Join(dir, "instructions.md"), err))
	}

	if b, err := os.ReadFile(filepath.Join(dir, "settings.json")); err == nil {
		if strings.TrimSpace(string(b)) != "" {
			var s projectDefaultSettings
			if err := json.Unmarshal(b, &s); err != nil {
				warnings = append(warnings, fmt.Sprintf("ignoring invalid %s: %v", filepath.Join(dir, "settings.json"), err))
			} else {
				d.HasSettings = true
				d.Settings = s
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		warnings = append(warnings, fmt.Sprintf("could not read %s: %v", filepath.Join(dir, "settings.json"), err))
	}

	if b, err := os.ReadFile(filepath.Join(dir, "routines.json")); err == nil {
		if strings.TrimSpace(string(b)) != "" {
			var routines []defaultRoutine
			if err := json.Unmarshal(b, &routines); err != nil {
				warnings = append(warnings, fmt.Sprintf("ignoring invalid %s: %v", filepath.Join(dir, "routines.json"), err))
			} else if len(routines) > 0 {
				d.HasRoutines = true
				d.Routines = routines
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		warnings = append(warnings, fmt.Sprintf("could not read %s: %v", filepath.Join(dir, "routines.json"), err))
	}

	return d, warnings
}

// applyDefaultRoutines joins the newly created workspace as the current user
// (without overwriting any local session config) and creates each default
// routine best-effort via POST /api/workspaces/{ws}/schedules.
func (a *app) applyDefaultRoutines(wsID string, routines []defaultRoutine) (int, []string) {
	if len(routines) == 0 {
		return 0, nil
	}
	var joinResp struct {
		Token string `json:"token"`
	}
	joinClient := newClient(a.server, firstNonEmpty(a.userToken(), a.token))
	if err := joinClient.do("POST", "/api/workspaces/"+wsID+"/join", map[string]any{}, &joinResp); err != nil {
		return 0, []string{fmt.Sprintf("could not apply default routines (join failed): %v", err)}
	}
	c := newClient(a.server, joinResp.Token)
	created := 0
	var warnings []string
	for _, r := range routines {
		in := r.toScheduleInput()
		var sc store.Schedule
		if err := c.do("POST", "/api/workspaces/"+wsID+"/schedules", in, &sc); err != nil {
			warnings = append(warnings, fmt.Sprintf("default routine %q skipped: %v", in.Title, err))
			continue
		}
		created++
	}
	return created, warnings
}

func (a *app) printWarnings(warnings []string) {
	for _, w := range warnings {
		fmt.Fprintf(a.errOut, "warning: %s\n", w)
	}
}

func (a *app) printJSONValue(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return a.printJSON(raw)
}

type defaultsShowJSON struct {
	Path         string                    `json:"path"`
	Exists       bool                      `json:"exists"`
	Instructions *defaultsInstructionsJSON `json:"instructions,omitempty"`
	Settings     *projectDefaultSettings   `json:"settings,omitempty"`
	Routines     []store.ScheduleInput     `json:"routines,omitempty"`
	Warnings     []string                  `json:"warnings,omitempty"`
}

type defaultsInstructionsJSON struct {
	Chars    int      `json:"chars"`
	Mode     string   `json:"mode"`
	Headings []string `json:"headings"`
}

func (a *app) defaultsCmd(args []string) error {
	sub := "show"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	if len(args) > 0 {
		return fmt.Errorf("%w: defaults [show | path]", errUsage)
	}
	switch sub {
	case "path":
		dir := resolveDefaultsDir()
		if a.jsonOut {
			return a.printJSONValue(map[string]string{"path": dir})
		}
		a.printf("%s\n", dir)
		return nil
	case "show":
		d, warnings := loadProjectDefaults()
		if a.jsonOut {
			out := defaultsShowJSON{
				Path:     d.Dir,
				Exists:   d.Exists,
				Warnings: warnings,
			}
			if d.HasInstructions {
				chars := utf8.RuneCountInString(strings.TrimSpace(d.Instructions))
				mode := "inline"
				if chars > store.InstructionsInlineLimit {
					mode = "indexed"
				}
				out.Instructions = &defaultsInstructionsJSON{
					Chars:    chars,
					Mode:     mode,
					Headings: store.ExtractInstructionHeadings(d.Instructions),
				}
			}
			if d.HasSettings {
				s := d.Settings
				out.Settings = &s
			}
			if d.HasRoutines {
				out.Routines = make([]store.ScheduleInput, 0, len(d.Routines))
				for _, r := range d.Routines {
					out.Routines = append(out.Routines, r.toScheduleInput())
				}
			}
			return a.printJSONValue(out)
		}
		a.printf("Defaults directory: %s\n", d.Dir)
		if !d.Exists || (!d.HasInstructions && !d.HasSettings && !d.HasRoutines) {
			a.printf("No local project defaults configured.\n")
			a.printf("Add optional files there to apply on `unison workspace create`:\n")
			a.printf("  instructions.md  default workspace agent instructions\n")
			a.printf("  routines.json    default recurring schedules (e.g. nightly audits)\n")
			a.printf("  settings.json    scalar defaults (e.g. instructions_path)\n")
			a.printWarnings(warnings)
			return nil
		}
		if d.HasInstructions {
			chars := utf8.RuneCountInString(strings.TrimSpace(d.Instructions))
			headings := store.ExtractInstructionHeadings(d.Instructions)
			if chars > store.InstructionsInlineLimit {
				a.printf("Instructions:      instructions.md (%d chars, indexed by %d heading(s))\n", chars, len(headings))
			} else {
				a.printf("Instructions:      instructions.md (%d / %d chars, inline on join)\n", chars, store.InstructionsInlineLimit)
			}
		}
		if d.HasSettings {
			var parts []string
			if d.Settings.InstructionsPath != "" {
				parts = append(parts, "instructions_path="+d.Settings.InstructionsPath)
			}
			if d.Settings.RepoURL != "" {
				parts = append(parts, "repo_url="+d.Settings.RepoURL)
			}
			if d.Settings.CanvasURL != "" {
				parts = append(parts, "canvas_url="+d.Settings.CanvasURL)
			}
			if len(parts) > 0 {
				a.printf("Settings:          %s\n", strings.Join(parts, ", "))
			}
		}
		if d.HasRoutines {
			a.printf("Routines (%d):\n", len(d.Routines))
			for _, r := range d.Routines {
				in := r.toScheduleInput()
				cadence := in.Cadence
				if in.Weekday != "" {
					cadence += " " + in.Weekday
				}
				a.printf("  - %s (%s)\n", in.Title, cadence)
			}
		}
		a.printWarnings(warnings)
		return nil
	default:
		return fmt.Errorf("%w: defaults [show | path]", errUsage)
	}
}
