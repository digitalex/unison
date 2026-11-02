package cli

import (
	"encoding/json"
	"fmt"
	"runtime/debug"
)

// Version is stamped into release builds (-ldflags -X, see release.yml).
var Version string

// buildVersion names this binary: the release it was stamped with, else what
// the Go toolchain recorded (`go install …@latest` knows its module version; a
// build from a clone knows its commit), else "dev".
func buildVersion() string {
	if Version != "" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 12 {
			return "dev-" + s.Value[:12]
		}
	}
	return "dev"
}

func (a *app) version(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("%w: version", errUsage)
	}
	v := buildVersion()
	if a.jsonOut {
		raw, err := json.Marshal(map[string]string{"version": v})
		if err != nil {
			return err
		}
		return a.printJSON(raw)
	}
	a.printf("unison %s\n", v)
	return nil
}
