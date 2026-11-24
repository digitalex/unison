package store

import (
	"fmt"
	"slices"
	"strings"
)

// Integration providers: the services a workspace can be set up with. The
// keys are stable identifiers stored in settings and sent over the API; the
// web UI mirrors this list in web/src/providers.js. Adding a provider means
// adding its key here (and there).
const (
	ProviderIsocan = "isocan"
	ProviderGitHub = "github"
)

// providerKeys lists the known providers in display order.
var providerKeys = []string{ProviderIsocan, ProviderGitHub}

// ProviderKeys returns the known provider keys, in display order.
func ProviderKeys() []string { return slices.Clone(providerKeys) }

// IsProvider reports whether key is a known provider key.
func IsProvider(key string) bool { return slices.Contains(providerKeys, key) }

// NormalizeProviders validates provider keys (trimmed, case-insensitive) and
// returns them deduplicated in ProviderKeys order. It returns an empty, non-nil
// slice for none, and ErrInvalid for an unknown key.
func NormalizeProviders(keys []string) ([]string, error) {
	seen := map[string]bool{}
	for _, k := range keys {
		k = strings.ToLower(strings.TrimSpace(k))
		if !IsProvider(k) {
			return nil, fmt.Errorf("%w: unknown integration %q (known: %s)", ErrInvalid, k, strings.Join(providerKeys, ", "))
		}
		seen[k] = true
	}
	return orderedProviders(seen), nil
}

// FilterProviders returns the known provider keys among keys, deduplicated and
// in ProviderKeys order, ignoring unknown keys. It returns an empty, non-nil
// slice when none match.
func FilterProviders(keys []string) []string {
	seen := map[string]bool{}
	for _, k := range keys {
		k = strings.ToLower(strings.TrimSpace(k))
		if IsProvider(k) {
			seen[k] = true
		}
	}
	return orderedProviders(seen)
}

func orderedProviders(seen map[string]bool) []string {
	out := []string{}
	for _, k := range providerKeys {
		if seen[k] {
			out = append(out, k)
		}
	}
	return out
}
