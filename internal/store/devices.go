package store

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Devices are an account's iOS devices registered for push notifications
// (#54). A device belongs to one account at a time: registering a token
// another account holds moves it. Each device remembers the session that
// registered it, so signing that session out stops its pushes.

// Device is a registered APNs device token.
type Device struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"user_id"`
	APNSToken  string `json:"apns_token"`
	Label      string `json:"label"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt int64  `json:"last_used_at,omitempty"`
	// Environment is the APNs environment the token is for (#137):
	// EnvSandbox (Xcode builds) or EnvProduction (TestFlight and the App
	// Store); "" for devices registered before apps said, which get the
	// server's default.
	Environment string `json:"environment,omitempty"`
}

// APNs environments a device token can be for.
const (
	EnvSandbox    = "sandbox"
	EnvProduction = "production"
)

// apnsTokenRE accepts an APNs device token: hex (32 bytes today, but Apple
// says not to rely on the length).
var apnsTokenRE = regexp.MustCompile(`^[0-9a-f]{16,400}$`)

const maxDeviceLabel = 100

const deviceCols = `id, user_id, apns_token, label, created_at, last_used_at, apns_env`

func scanDevice(row interface{ Scan(...any) error }) (*Device, error) {
	var d Device
	var used sql.NullInt64
	var env sql.NullString
	if err := row.Scan(&d.ID, &d.UserID, &d.APNSToken, &d.Label, &d.CreatedAt, &used, &env); err != nil {
		return nil, err
	}
	d.LastUsedAt = used.Int64
	d.Environment = env.String
	return &d, nil
}

func (s *Store) queryDevices(ctx context.Context, where string, args ...any) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// RegisterDevice registers (or re-registers) an APNs token for userID from
// sessionID. A token already registered, by this account or another, is
// moved to userID and sessionID with the new label. env is the token's APNs
// environment (EnvSandbox or EnvProduction); "" leaves it as it was.
func (s *Store) RegisterDevice(ctx context.Context, userID, sessionID int64, token, label, env string) (*Device, error) {
	token = strings.ToLower(strings.TrimSpace(token))
	if !apnsTokenRE.MatchString(token) {
		return nil, fmt.Errorf("%w: apns_token must be the device token in hex", ErrInvalid)
	}
	var envArg any // NULL: unknown, or unchanged on re-registering.
	switch env = strings.TrimSpace(env); env {
	case "":
	case EnvSandbox, EnvProduction:
		envArg = env
	default:
		return nil, fmt.Errorf("%w: environment must be %q or %q", ErrInvalid, EnvSandbox, EnvProduction)
	}
	label = strings.TrimSpace(label)
	if r := []rune(label); len(r) > maxDeviceLabel {
		label = string(r[:maxDeviceLabel])
	}
	return scanDevice(s.db.QueryRowContext(ctx, `INSERT INTO devices (user_id, apns_token, label, session_id, created_at, apns_env) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (apns_token) DO UPDATE SET user_id = excluded.user_id, label = excluded.label, session_id = excluded.session_id,
			apns_env = COALESCE(excluded.apns_env, devices.apns_env)
		RETURNING `+deviceCols, userID, token, label, sessionID, s.nowMillis(), envArg))
}

// ListDevices lists userID's devices, oldest first.
func (s *Store) ListDevices(ctx context.Context, userID int64) ([]Device, error) {
	return s.queryDevices(ctx, `user_id = ?`, userID)
}

// DeleteDevice removes one of userID's devices; anyone else's is
// ErrNotFound.
func (s *Store) DeleteDevice(ctx context.Context, userID, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("device %d: %w", id, ErrNotFound)
	}
	return nil
}

// ForgetDevice removes a device APNs reported as no longer valid.
func (s *Store) ForgetDevice(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE id = ?`, id)
	return err
}

// TouchDevice records a successful push to a device.
func (s *Store) TouchDevice(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE devices SET last_used_at = ? WHERE id = ?`, s.nowMillis(), id)
	return err
}

// PushRecipients returns the accounts to notify about a post by authorID in
// wsID: action, the accounts of the participants actionFor asks to act, and
// mention, the other accounts whose participants body @mentions. Each
// account appears once (action wins); the author's own account and accounts
// that are no longer members are left out.
func (s *Store) PushRecipients(ctx context.Context, wsID string, authorID int64, body string, actionFor []int64) (action, mention []int64, err error) {
	var author sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT user_id FROM participants WHERE id = ?`, authorID).Scan(&author); err != nil && err != sql.ErrNoRows {
		return nil, nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT p.id, p.name, p.user_id FROM participants p
		JOIN memberships m ON m.workspace_id = p.workspace_id AND m.user_id = p.user_id
		WHERE p.workspace_id = ? AND p.id <> ? ORDER BY p.id`, wsID, authorID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	asked := map[int64]bool{}
	for _, id := range actionFor {
		asked[id] = true
	}
	actionUsers, mentionUsers := map[int64]bool{}, map[int64]bool{}
	for rows.Next() {
		var id, user int64
		var name string
		if err := rows.Scan(&id, &name, &user); err != nil {
			return nil, nil, err
		}
		switch {
		case author.Valid && user == author.Int64:
		case asked[id]:
			actionUsers[user] = true
		case mentions(body, name):
			mentionUsers[user] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	for user := range actionUsers {
		action = append(action, user)
		delete(mentionUsers, user)
	}
	for user := range mentionUsers {
		mention = append(mention, user)
	}
	slices.Sort(action)
	slices.Sort(mention)
	return action, mention, nil
}

// deleteSessionDevices removes the devices registered by userID's sessions
// that match cond (a condition on session_id).
func (s *Store) deleteSessionDevices(ctx context.Context, userID int64, cond string, args ...any) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE user_id = ? AND session_id IS NOT NULL AND `+cond, append([]any{userID}, args...)...)
	return err
}
