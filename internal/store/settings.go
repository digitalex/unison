package store

import (
	"context"
	"encoding/json"
	"slices"
)

// User settings: a person's preferences, such as the integrations to set up
// for every new workspace they create. They are kept per user, not per
// workspace, so the web UI and the CLI agree on any machine. Like the chat
// read marker they are private to their user: no event is recorded. A setting
// at its default has no row.

// GetUserSettings returns the user's settings, each at its default unless
// they set it.
func (s *Store) GetUserSettings(ctx context.Context, userID int64) (UserSettings, error) {
	out := DefaultUserSettings()
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM user_settings WHERE user_id = ?`, userID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return out, err
		}
		if !slices.Contains(UserSettingKeys(), key) {
			continue // A setting this server no longer knows.
		}
		if err := out.ApplyUserSetting(key, json.RawMessage(value)); err != nil {
			return out, err
		}
	}
	return out, rows.Err()
}

// SetUserSettings validates and stores settings (setting key to JSON value)
// for the user, all in one transaction, and returns their settings after.
// Setting one to its default (an empty integrations list) clears it. It
// returns ErrInvalid, changing nothing, if any key is unknown or any value
// malformed.
func (s *Store) SetUserSettings(ctx context.Context, userID int64, values map[string]json.RawMessage) (UserSettings, error) {
	type change struct {
		key, value string
		clear      bool
	}
	var changes []change
	for key, raw := range values {
		canonical, isDefault, err := NormalizeUserSetting(key, raw)
		if err != nil {
			return UserSettings{}, err
		}
		changes = append(changes, change{key: key, value: string(canonical), clear: isDefault})
	}
	now := s.nowMillis()
	t, err := s.db.begin(ctx)
	if err != nil {
		return UserSettings{}, err
	}
	for _, c := range changes {
		if c.clear {
			if _, err := t.Exec(`DELETE FROM user_settings WHERE user_id = ? AND key = ?`, userID, c.key); err != nil {
				t.Rollback()
				return UserSettings{}, err
			}
			continue
		}
		res, err := t.Exec(`UPDATE user_settings SET value = ?, updated_at = ? WHERE user_id = ? AND key = ?`,
			c.value, now, userID, c.key)
		if err != nil {
			t.Rollback()
			return UserSettings{}, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			continue
		}
		if _, err := t.Exec(`INSERT INTO user_settings (user_id, key, value, updated_at) VALUES (?, ?, ?, ?)`,
			userID, c.key, c.value, now); err != nil {
			t.Rollback()
			return UserSettings{}, err
		}
	}
	if err := t.Commit(); err != nil {
		return UserSettings{}, err
	}
	return s.GetUserSettings(ctx, userID)
}

// NewProjectIntegrations returns the providers the user wants set up for
// every new workspace they create (none unless they chose some), in
// ProviderKeys order.
func (s *Store) NewProjectIntegrations(ctx context.Context, userID int64) ([]string, error) {
	settings, err := s.GetUserSettings(ctx, userID)
	return settings.NewProjectIntegrations, err
}
