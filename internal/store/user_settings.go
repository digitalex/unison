package store

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// User settings: a person's preferences, kept on the server so the web UI
// and the CLI agree on any machine (GET/PUT /api/me/settings).

// SettingNewProjectIntegrations is the setting holding the providers to set
// up for every new workspace the person creates: a JSON array of provider
// keys, empty (all off) by default.
const SettingNewProjectIntegrations = "new_project.integrations"

// userSettingKeys lists the known settings.
var userSettingKeys = []string{SettingNewProjectIntegrations}

// UserSettingKeys returns the known setting keys.
func UserSettingKeys() []string { return slices.Clone(userSettingKeys) }

// UserSettings is a person's settings, each at its default unless they set
// it. Its JSON form is what GET and PUT /api/me/settings return.
type UserSettings struct {
	// NewProjectIntegrations are the providers to set up for every new
	// workspace (SettingNewProjectIntegrations), in ProviderKeys order.
	NewProjectIntegrations []string `json:"new_project.integrations"`
}

// DefaultUserSettings returns the settings of someone who never set any.
func DefaultUserSettings() UserSettings {
	return UserSettings{NewProjectIntegrations: []string{}}
}

// NormalizeUserSetting validates value for setting key and returns its
// canonical JSON, and whether that is the setting's default. It returns
// ErrInvalid for an unknown key or a malformed value.
func NormalizeUserSetting(key string, value json.RawMessage) (canonical json.RawMessage, isDefault bool, err error) {
	switch key {
	case SettingNewProjectIntegrations:
		var keys []string
		if err := json.Unmarshal(value, &keys); err != nil {
			return nil, false, fmt.Errorf("%w: %s must be an array of integration keys (%s)", ErrInvalid, key, strings.Join(providerKeys, ", "))
		}
		keys, err := NormalizeProviders(keys)
		if err != nil {
			return nil, false, err
		}
		canonical, err := json.Marshal(keys)
		return canonical, len(keys) == 0, err
	}
	return nil, false, fmt.Errorf("%w: unknown setting %q (known: %s)", ErrInvalid, key, strings.Join(userSettingKeys, ", "))
}

// ApplyUserSetting sets setting key in s to value, a canonical value from
// NormalizeUserSetting. It returns ErrInvalid for an unknown key.
func (s *UserSettings) ApplyUserSetting(key string, value json.RawMessage) error {
	switch key {
	case SettingNewProjectIntegrations:
		var keys []string
		if err := json.Unmarshal(value, &keys); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrInvalid, key, err)
		}
		s.NewProjectIntegrations = FilterProviders(keys)
		return nil
	}
	return fmt.Errorf("%w: unknown setting %q", ErrInvalid, key)
}
