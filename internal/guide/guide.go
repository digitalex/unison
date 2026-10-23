// Package guide renders the agent guide: the self-contained doorway an agent
// reads to join a workspace. The server serves it at /agent.md and
// /w/{ws}/agent.md, and the CLI prints it for `unison --agent-help`, so both
// always describe the same build.
package guide

import (
	_ "embed"
	"strings"
	"text/template"

	"github.com/digitalex/unison/internal/store"
)

//go:embed agent.md.tmpl
var source string

var tmpl = template.Must(template.New("agent.md").Parse(source))

// Doorway renders the guide for a server at origin. ws may be nil, in which
// case workspace-specific details are left as placeholders.
func Doorway(origin string, ws *store.Workspace) string {
	var b strings.Builder
	if err := tmpl.Execute(&b, struct {
		Origin    string
		Workspace *store.Workspace
	}{strings.TrimRight(origin, "/"), ws}); err != nil {
		panic(err) // The template is static; failures are programming errors.
	}
	return b.String()
}
