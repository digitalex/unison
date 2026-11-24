// Package guide renders the agent guide: the self-contained doorway an agent
// reads to join a workspace. The server serves it at /agent.md and
// /w/{ws}/agent.md, and the CLI prints it for `unison --agent-help`, so both
// always describe the same build.
package guide

import (
	_ "embed"
	"strings"
	"text/template"
	"unicode/utf8"

	"github.com/digitalex/unison/internal/store"
)

//go:embed agent.md.tmpl
var source string

// cell makes text safe in a Markdown table cell (a link's name, #299).
func cell(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

var tmpl = template.Must(template.New("agent.md").Funcs(template.FuncMap{"cell": cell}).Parse(source))

// DoorwayOptions configures the rendered agent guide.
type DoorwayOptions struct {
	Origin       string
	Workspace    *store.Workspace
	Instructions *store.WorkspaceInstructions
	Pass         string
}

// Doorway renders the guide for a server at origin. ws may be nil, in which
// case workspace-specific details are left as placeholders. pass, when set,
// is the reader's invite pass, filled into the join command.
func Doorway(origin string, ws *store.Workspace, pass string) string {
	return RenderDoorway(DoorwayOptions{Origin: origin, Workspace: ws, Pass: pass})
}

// RenderDoorway renders the agent guide with optional workspace instructions.
// When Instructions is nil or both body and path are empty, zero extra text is
// emitted for instructions.
func RenderDoorway(opts DoorwayOptions) string {
	var (
		hasInstrSection bool
		instrInline     bool
		instrBody       string
		instrPath       string
		instrHeadings   []string
		instrChars      int
	)
	if opts.Instructions != nil {
		body := strings.TrimSpace(opts.Instructions.Instructions)
		instrPath = strings.TrimSpace(opts.Instructions.Path)
		if body != "" || instrPath != "" {
			hasInstrSection = true
			instrChars = utf8.RuneCountInString(body)
			if body != "" && instrChars <= store.InstructionsInlineLimit {
				instrInline = true
				instrBody = body
			} else if body != "" {
				instrHeadings = opts.Instructions.Headings
				if instrHeadings == nil {
					instrHeadings = store.ExtractInstructionHeadings(body)
				}
			}
		}
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, struct {
		Origin                 string
		Workspace              *store.Workspace
		Pass                   string
		HasInstructionsSection bool
		InstructionsInline     bool
		InstructionsBody       string
		InstructionsPath       string
		InstructionsHeadings   []string
		InstructionsChars      int
	}{
		Origin:                 strings.TrimRight(opts.Origin, "/"),
		Workspace:              opts.Workspace,
		Pass:                   opts.Pass,
		HasInstructionsSection: hasInstrSection,
		InstructionsInline:     instrInline,
		InstructionsBody:       instrBody,
		InstructionsPath:       instrPath,
		InstructionsHeadings:   instrHeadings,
		InstructionsChars:      instrChars,
	}); err != nil {
		panic(err) // The template is static; failures are programming errors.
	}
	return b.String()
}
