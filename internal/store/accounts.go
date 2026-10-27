package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Accounts: people sign in with GitHub or Google (an identity), get a User,
// and hold Sessions (a browser cookie or a CLI token). Accounts are not
// workspace-scoped, so these tables record no workspace events.

// Session kinds.
const (
	SessionBrowser = "browser"
	SessionCLI     = "cli"
	SessionApp     = "app" // a native app (the iPhone app), via RedeemAppCode
)

// SessionTTL is how long a session lives without use; every use (at most
// every SessionSlideEvery) extends it by SessionTTL again.
const (
	SessionTTL        = 90 * 24 * time.Hour
	SessionSlideEvery = 5 * time.Minute
)

// User is a signed-in person (one per human, across providers).
type User struct {
	ID          int64      `json:"id"`
	DisplayName string     `json:"display_name"`
	AvatarURL   string     `json:"avatar_url"`
	CreatedAt   int64      `json:"created_at"`
	Identities  []Identity `json:"identities"`
}

// Identity is a provider account linked to a user.
type Identity struct {
	Provider      string `json:"provider"`
	Subject       string `json:"subject"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

// VerifiedEmails returns the user's verified email addresses (lowercased).
func (u *User) VerifiedEmails() []string {
	var out []string
	for _, id := range u.Identities {
		if id.EmailVerified && id.Email != "" {
			out = append(out, strings.ToLower(id.Email))
		}
	}
	return out
}

// Profile is what an OAuth provider told us about a person.
type Profile struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	AvatarURL     string
}

// Session is a signed-in browser or CLI. The raw token is never stored.
type Session struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"user_id"`
	Kind       string `json:"kind"`
	Label      string `json:"label"`
	CreatedAt  int64  `json:"created_at"`
	LastSeenAt int64  `json:"last_seen_at"`
	ExpiresAt  int64  `json:"expires_at"`
}

// UserTokenPrefix marks user session tokens, telling them apart from
// workspace participant tokens (32 hex characters).
const UserTokenPrefix = "us_"

func randomToken(prefix string, n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b)
}

// HashToken is how secrets (session tokens, OAuth states, device codes) are
// stored and looked up.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ---- Users and identities ----

// LoginWithProvider finds or creates the user for a provider profile:
// by (provider, subject); else, when the provider vouches for the email
// (verified), the user owning another identity with the same verified email;
// else a new user. The identity's email and the user's name/avatar are
// refreshed on every login.
func (s *Store) LoginWithProvider(ctx context.Context, provider string, p Profile) (*User, error) {
	if provider == "" || p.Subject == "" {
		return nil, fmt.Errorf("%w: provider and subject are required", ErrInvalid)
	}
	email := strings.TrimSpace(p.Email)
	var userID int64
	_, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		err := t.QueryRow(`SELECT user_id FROM user_identities WHERE provider = ? AND subject = ?`, provider, p.Subject).Scan(&userID)
		switch {
		case err == nil:
			_, err = t.Exec(`UPDATE user_identities SET email = ?, email_verified = ? WHERE provider = ? AND subject = ?`,
				email, p.EmailVerified, provider, p.Subject)
			if err != nil {
				return err
			}
		case errors.Is(err, sql.ErrNoRows):
			userID = 0
			if p.EmailVerified && email != "" {
				err := t.QueryRow(`SELECT user_id FROM user_identities WHERE lower(email) = lower(?) AND email_verified = ? ORDER BY id LIMIT 1`,
					email, true).Scan(&userID)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return err
				}
			}
			if userID == 0 {
				if userID, err = t.insert(`INSERT INTO users (display_name, avatar_url, created_at) VALUES (?, ?, ?)`, "id",
					p.Name, p.AvatarURL, log.now); err != nil {
					return err
				}
			}
			if _, err := t.Exec(`INSERT INTO user_identities (user_id, provider, subject, email, email_verified, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
				userID, provider, p.Subject, email, p.EmailVerified, log.now); err != nil {
				return err
			}
		default:
			return err
		}
		if p.Name == "" && p.AvatarURL == "" {
			return nil
		}
		_, err = t.Exec(`UPDATE users SET display_name = CASE WHEN ? <> '' THEN ? ELSE display_name END,
			avatar_url = CASE WHEN ? <> '' THEN ? ELSE avatar_url END WHERE id = ?`, p.Name, p.Name, p.AvatarURL, p.AvatarURL, userID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetUser(ctx, userID)
}

// GetUser returns a user with its identities.
func (s *Store) GetUser(ctx context.Context, id int64) (*User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `SELECT id, display_name, avatar_url, created_at FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.DisplayName, &u.AvatarURL, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("user %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT provider, subject, email, email_verified FROM user_identities WHERE user_id = ? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	u.Identities = []Identity{}
	for rows.Next() {
		var i Identity
		if err := rows.Scan(&i.Provider, &i.Subject, &i.Email, &i.EmailVerified); err != nil {
			return nil, err
		}
		u.Identities = append(u.Identities, i)
	}
	return &u, rows.Err()
}

// ---- Sessions ----

const sessionCols = `id, user_id, kind, label, created_at, last_seen_at, expires_at`

func scanSession(row interface{ Scan(...any) error }) (*Session, error) {
	var se Session
	if err := row.Scan(&se.ID, &se.UserID, &se.Kind, &se.Label, &se.CreatedAt, &se.LastSeenAt, &se.ExpiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &se, nil
}

func createSession(t *tx, userID int64, kind, label string, now int64) (string, *Session, error) {
	token := randomToken(UserTokenPrefix, 32)
	se := &Session{UserID: userID, Kind: kind, Label: truncate(label, 200), CreatedAt: now, LastSeenAt: now, ExpiresAt: now + SessionTTL.Milliseconds()}
	id, err := t.insert(`INSERT INTO sessions (user_id, token_hash, kind, label, created_at, last_seen_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "id",
		se.UserID, HashToken(token), se.Kind, se.Label, se.CreatedAt, se.LastSeenAt, se.ExpiresAt)
	if err != nil {
		return "", nil, err
	}
	se.ID = id
	return token, se, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// CreateSession starts a session for a user and returns its raw token (only
// its hash is stored).
func (s *Store) CreateSession(ctx context.Context, userID int64, kind, label string) (string, *Session, error) {
	if kind != SessionBrowser && kind != SessionCLI {
		return "", nil, fmt.Errorf("%w: unknown session kind %q", ErrInvalid, kind)
	}
	var token string
	var se *Session
	_, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var err error
		token, se, err = createSession(t, userID, kind, label, log.now)
		return err
	})
	return token, se, err
}

// SessionByToken resolves a raw session token to its live session and user.
// Expired and revoked sessions are ErrNotFound. Use slides the expiry
// forward, writing at most every SessionSlideEvery.
func (s *Store) SessionByToken(ctx context.Context, token string) (*Session, *User, error) {
	if !strings.HasPrefix(token, UserTokenPrefix) {
		return nil, nil, ErrNotFound
	}
	now := s.nowMillis()
	se, err := scanSession(s.db.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM sessions WHERE token_hash = ? AND revoked_at IS NULL AND expires_at > ?`,
		HashToken(token), now))
	if err != nil {
		return nil, nil, err
	}
	if now-se.LastSeenAt >= SessionSlideEvery.Milliseconds() {
		se.LastSeenAt, se.ExpiresAt = now, now+SessionTTL.Milliseconds()
		if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id = ?`, se.LastSeenAt, se.ExpiresAt, se.ID); err != nil {
			return nil, nil, err
		}
	}
	u, err := s.GetUser(ctx, se.UserID)
	if err != nil {
		return nil, nil, err
	}
	return se, u, nil
}

// ListSessions returns a user's live sessions, most recently used first.
func (s *Store) ListSessions(ctx context.Context, userID int64) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+sessionCols+` FROM sessions WHERE user_id = ? AND revoked_at IS NULL AND expires_at > ? ORDER BY last_seen_at DESC, id DESC`,
		userID, s.nowMillis())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		se, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *se)
	}
	return out, rows.Err()
}

// RevokeSession signs out one of the user's sessions.
func (s *Store) RevokeSession(ctx context.Context, userID, sessionID int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL`, s.nowMillis(), sessionID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("session %d: %w", sessionID, ErrNotFound)
	}
	return s.deleteSessionDevices(ctx, userID, `session_id = ?`, sessionID)
}

// ---- OAuth login state ----

// OAuthState is a pending OAuth login (state, PKCE verifier, return path).
// LinkUserID, when set, makes it a link: the identity is attached to that
// (signed-in) user instead of signing in. AppChallenge, when set, makes it
// a native app's login: the callback hands the app a one-time code (see
// CreateAppCode) bound to this PKCE challenge instead of setting a cookie.
type OAuthState struct {
	Provider     string
	Verifier     string
	ReturnTo     string
	LinkUserID   int64
	AppChallenge string
}

// OAuthStateTTL bounds how long a login may take at the provider.
const OAuthStateTTL = 10 * time.Minute

// SaveOAuthState records a pending login under the hash of state.
func (s *Store) SaveOAuthState(ctx context.Context, state string, st OAuthState) error {
	now := s.nowMillis()
	_, err := s.db.ExecContext(ctx, `DELETE FROM oauth_states WHERE expires_at <= ?`, now)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO oauth_states (state_hash, provider, verifier, return_to, expires_at, link_user_id, app_challenge) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		HashToken(state), st.Provider, st.Verifier, st.ReturnTo, now+OAuthStateTTL.Milliseconds(), nullable(st.LinkUserID), nullString(st.AppChallenge))
	return err
}

// TakeOAuthState returns and deletes a pending login (single use).
func (s *Store) TakeOAuthState(ctx context.Context, state string) (*OAuthState, error) {
	var st OAuthState
	var expires int64
	var link sql.NullInt64
	var app sql.NullString
	_, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		h := HashToken(state)
		err := t.QueryRow(`SELECT provider, verifier, return_to, expires_at, link_user_id, app_challenge FROM oauth_states WHERE state_hash = ?`, h).
			Scan(&st.Provider, &st.Verifier, &st.ReturnTo, &expires, &link, &app)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := t.Exec(`DELETE FROM oauth_states WHERE state_hash = ?`, h); err != nil {
			return err
		}
		if expires <= log.now {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	st.LinkUserID, st.AppChallenge = link.Int64, app.String
	return &st, nil
}

// ---- Native app sign-in ----
//
// A native app signs in through the browser OAuth flow, started with its own
// PKCE challenge. Instead of a session cookie, the callback hands the app a
// one-time code (through its unison:// redirect), and the app redeems the
// code with the matching verifier for a session token. A code stolen on the
// way (another app claiming the scheme) is useless without the verifier.

// AppCodeTTL bounds how long the app has to redeem its code.
const AppCodeTTL = 2 * time.Minute

// CreateAppCode issues a single-use code that RedeemAppCode turns into a
// session for userID, given the verifier for challenge (S256).
func (s *Store) CreateAppCode(ctx context.Context, userID int64, challenge string) (string, error) {
	now := s.nowMillis()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM app_codes WHERE expires_at <= ?`, now); err != nil {
		return "", err
	}
	code := randomToken("ac_", 32)
	_, err := s.db.ExecContext(ctx, `INSERT INTO app_codes (code_hash, user_id, challenge, expires_at) VALUES (?, ?, ?, ?)`,
		HashToken(code), userID, challenge, now+AppCodeTTL.Milliseconds())
	return code, err
}

// RedeemAppCode spends a code from CreateAppCode and starts an app session,
// returning its raw token. A code works once, before it expires, and only
// with the verifier whose S256 hash is its challenge; otherwise ErrNotFound.
func (s *Store) RedeemAppCode(ctx context.Context, code, verifier, label string) (string, error) {
	var token string
	var spent bool // a wrong verifier or a stale code: deleted, but no session
	_, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		h := HashToken(code)
		var userID, expires int64
		var challenge string
		err := t.QueryRow(`SELECT user_id, challenge, expires_at FROM app_codes WHERE code_hash = ?`, h).Scan(&userID, &challenge, &expires)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		// Spent even on a wrong verifier, so a code can't be guessed at.
		if _, err := t.Exec(`DELETE FROM app_codes WHERE code_hash = ?`, h); err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(verifier))
		if expires <= log.now || subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) != 1 {
			spent = true // commit the delete
			return nil
		}
		token, _, err = createSession(t, userID, SessionApp, label, log.now)
		return err
	})
	if err != nil {
		return "", err
	}
	if spent {
		return "", ErrNotFound
	}
	return token, nil
}

// ---- Device authorization (unison login) ----

// Device flow statuses (as returned to the polling CLI).
const (
	DevicePending  = "pending"
	DeviceApproved = "approved"
	DeviceDenied   = "denied"
	DeviceExpired  = "expired"
	DeviceSlowDown = "slow_down"
	deviceUsed     = "used"
)

// DeviceCodeTTL is how long a `unison login` code stays valid.
const DeviceCodeTTL = 10 * time.Minute

// userCodeAlphabet avoids vowels (no accidental words) and look-alikes.
const userCodeAlphabet = "BCDFGHJKLMNPQRSTVWXZ"

// DeviceStart is a new device authorization.
type DeviceStart struct {
	DeviceCode string
	UserCode   string // e.g. "BCDF-GHJK"
	Interval   int    // seconds between polls
	ExpiresIn  int    // seconds
}

// NormalizeUserCode uppercases a user code and drops separators.
func NormalizeUserCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(code) {
		if strings.ContainsRune(userCodeAlphabet, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func newUserCode() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	code := make([]byte, 8)
	for i, v := range b {
		code[i] = userCodeAlphabet[int(v)%len(userCodeAlphabet)]
	}
	return string(code)
}

// StartDevice creates a device authorization: a secret device code for the
// CLI and a short user code for the person approving in a browser.
func (s *Store) StartDevice(ctx context.Context, interval time.Duration) (*DeviceStart, error) {
	secs := max(int(interval/time.Second), 1)
	d := &DeviceStart{DeviceCode: randomToken("ud_", 32), Interval: secs, ExpiresIn: int(DeviceCodeTTL / time.Second)}
	_, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		if _, err := t.Exec(`DELETE FROM device_codes WHERE expires_at <= ?`, log.now-time.Hour.Milliseconds()); err != nil {
			return err
		}
		for attempt := 0; ; attempt++ {
			code := newUserCode()
			var n int
			if err := t.QueryRow(`SELECT COUNT(*) FROM device_codes WHERE user_code = ?`, code).Scan(&n); err != nil {
				return err
			}
			if n > 0 && attempt < 5 {
				continue
			}
			d.UserCode = code[:4] + "-" + code[4:]
			_, err := t.Exec(`INSERT INTO device_codes (device_code_hash, user_code, status, interval_secs, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
				HashToken(d.DeviceCode), code, DevicePending, secs, log.now, log.now+DeviceCodeTTL.Milliseconds())
			return err
		}
	})
	if err != nil {
		return nil, err
	}
	return d, nil
}

// DecideDevice approves (or denies) a pending device authorization for the
// signed-in user. Unknown, expired or already decided codes are ErrNotFound.
func (s *Store) DecideDevice(ctx context.Context, userID int64, userCode string, approve bool) error {
	status := DeviceDenied
	if approve {
		status = DeviceApproved
	}
	res, err := s.db.ExecContext(ctx, `UPDATE device_codes SET status = ?, user_id = ? WHERE user_code = ? AND status = ? AND expires_at > ?`,
		status, userID, NormalizeUserCode(userCode), DevicePending, s.nowMillis())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: unknown or expired code (start `unison login` again)", ErrNotFound)
	}
	return nil
}

// PendingDeviceCode reports whether a user code is waiting for approval.
func (s *Store) PendingDeviceCode(ctx context.Context, userCode string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_codes WHERE user_code = ? AND status = ? AND expires_at > ?`,
		NormalizeUserCode(userCode), DevicePending, s.nowMillis()).Scan(&n)
	return n > 0, err
}

// PollDevice is the CLI's poll. It returns a status (pending, slow_down,
// denied, expired) or, once approved, a new CLI session token — exactly once.
func (s *Store) PollDevice(ctx context.Context, deviceCode, label string) (status, token string, err error) {
	_, err = s.tx(ctx, func(t *tx, log *eventLog) error {
		var id, expires, lastPoll int64
		var interval int
		var userID sql.NullInt64
		err := t.QueryRow(`SELECT id, status, user_id, interval_secs, last_poll_at, expires_at FROM device_codes WHERE device_code_hash = ?`,
			HashToken(deviceCode)).Scan(&id, &status, &userID, &interval, &lastPoll, &expires)
		if errors.Is(err, sql.ErrNoRows) {
			status = DeviceExpired
			return nil
		}
		if err != nil {
			return err
		}
		switch {
		case status == deviceUsed || expires <= log.now:
			status = DeviceExpired
			return nil
		case status == DeviceDenied:
			return nil
		case status == DevicePending && log.now-lastPoll < int64(interval)*1000:
			status = DeviceSlowDown
			_, err = t.Exec(`UPDATE device_codes SET last_poll_at = ? WHERE id = ?`, log.now, id)
			return err
		case status == DevicePending:
			_, err = t.Exec(`UPDATE device_codes SET last_poll_at = ? WHERE id = ?`, log.now, id)
			return err
		}
		// Approved: issue the CLI session once.
		if _, err := t.Exec(`UPDATE device_codes SET status = ? WHERE id = ?`, deviceUsed, id); err != nil {
			return err
		}
		token, _, err = createSession(t, userID.Int64, SessionCLI, label, log.now)
		status = DeviceApproved
		return err
	})
	return status, token, err
}

// LinkIdentity attaches a provider identity to an existing user (the
// "Link GitHub" flow on the profile page). Linking an identity the user
// already has refreshes it; one that belongs to another user is
// ErrConflict: an identity identifies exactly one user.
func (s *Store) LinkIdentity(ctx context.Context, userID int64, provider string, p Profile) (*User, error) {
	if provider == "" || p.Subject == "" {
		return nil, fmt.Errorf("%w: provider and subject are required", ErrInvalid)
	}
	email := strings.TrimSpace(p.Email)
	_, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var owner int64
		err := t.QueryRow(`SELECT user_id FROM user_identities WHERE provider = ? AND subject = ?`, provider, p.Subject).Scan(&owner)
		switch {
		case err == nil && owner != userID:
			return fmt.Errorf("%w: that %s account is already linked to another Unison account; sign in with it instead", ErrConflict, provider)
		case err == nil:
			_, err = t.Exec(`UPDATE user_identities SET email = ?, email_verified = ? WHERE provider = ? AND subject = ?`,
				email, p.EmailVerified, provider, p.Subject)
			return err
		case errors.Is(err, sql.ErrNoRows):
			if _, err := displayName(t, userID); err != nil {
				return err
			}
			_, err = t.Exec(`INSERT INTO user_identities (user_id, provider, subject, email, email_verified, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
				userID, provider, p.Subject, email, p.EmailVerified, log.now)
			return err
		default:
			return err
		}
	})
	if err != nil {
		return nil, err
	}
	return s.GetUser(ctx, userID)
}

// RevokeOtherSessions signs out all of the user's sessions except keep (the
// current one) and returns how many it revoked.
func (s *Store) RevokeOtherSessions(ctx context.Context, userID, keep int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at = ? WHERE user_id = ? AND id <> ? AND revoked_at IS NULL AND expires_at > ?`,
		s.nowMillis(), userID, keep, s.nowMillis())
	if err != nil {
		return 0, err
	}
	if err := s.deleteSessionDevices(ctx, userID, `session_id <> ?`, keep); err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
