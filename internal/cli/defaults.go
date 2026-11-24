package cli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/digitalex/unison/internal/store"
)

// Personal defaults: the settings of the person running the CLI, kept on the
// server so the web UI (Settings → My defaults / Profile) and the CLI agree on
// any machine. Only people may read or change them, never their agents.

const meUsage = `me defaults [show] | me defaults set --integrations isocan,github ("" clears)`

// me runs `unison me defaults ...`.
func (a *app) me(args []string) error {
	if len(args) == 0 || args[0] != "defaults" {
		return fmt.Errorf("%w: %s", errUsage, meUsage)
	}
	if err := a.checkNotAgent("read or change a person's defaults"); err != nil {
		return err
	}
	args = args[1:]
	switch {
	case len(args) == 0 || (len(args) == 1 && args[0] == "show"):
		var settings store.UserSettings
		if ok, err := a.userCall("GET", "/api/me/settings", nil, &settings); !ok {
			return err
		}
		a.printDefaults(&settings)
		return nil
	case args[0] == "set":
		return a.setDefaults(args[1:])
	}
	return fmt.Errorf("%w: %s", errUsage, meUsage)
}

// setDefaults runs `unison me defaults set --integrations a,b`.
func (a *app) setDefaults(args []string) error {
	fs := flag.NewFlagSet("me defaults set", flag.ContinueOnError)
	var integrations *string
	fs.Func("integrations", `comma-separated integrations to set up for every new workspace ("" clears)`, func(v string) error {
		integrations = &v
		return nil
	})
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 || integrations == nil {
		return fmt.Errorf("%w: me defaults set --integrations %s (\"\" clears)", errUsage, strings.Join(store.ProviderKeys(), ","))
	}
	var keys []string
	for _, k := range strings.Split(*integrations, ",") {
		if k = strings.TrimSpace(k); k != "" {
			keys = append(keys, k)
		}
	}
	keys, err = store.NormalizeProviders(keys)
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	var settings store.UserSettings
	body := map[string]any{store.SettingNewProjectIntegrations: keys}
	if ok, err := a.userCall("PUT", "/api/me/settings", body, &settings); !ok {
		return err
	}
	a.printDefaults(&settings)
	return nil
}

func (a *app) printDefaults(s *store.UserSettings) {
	if len(s.NewProjectIntegrations) == 0 {
		a.printf("New workspaces: no integrations set up by default (choose from %s)\n", strings.Join(store.ProviderKeys(), ", "))
		return
	}
	a.printf("New workspaces: set up %s\n", strings.Join(s.NewProjectIntegrations, ", "))
}
