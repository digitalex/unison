package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // Registers the "pgx" database/sql driver.
	_ "modernc.org/sqlite"             // Registers the pure-Go "sqlite" driver.
)

// The store speaks two SQL dialects: SQLite (local development and tests)
// and Postgres (production). Queries are written once with `?` placeholders
// in the common subset of both; this file holds everything that differs.

// writeLockKey is the Postgres advisory lock every write transaction takes.
// It serializes writers exactly like SQLite's single connection does, which
// keeps the atomic claim, position renumbering, unique-name allocation and
// event ordering (seq commits in order, so `seq > after` never skips one)
// correct without per-operation locking. Unison's write volume is tiny.
const writeLockKey = 0x756e69736f6e // "unison"

// IsPostgresDSN reports whether dsn selects Postgres: a postgres:// or
// postgresql:// URL, or a key=value DSN with a host. Anything else is a
// SQLite path.
func IsPostgresDSN(dsn string) bool {
	return strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") ||
		(strings.Contains(dsn, "=") && strings.Contains(dsn, "host="))
}

// db and tx wrap database/sql so call sites keep `?` placeholders; on
// Postgres they are rewritten to $1, $2, ...
type db struct {
	*sql.DB
	pg bool
}

func (d db) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return d.DB.ExecContext(ctx, rebind(d.pg, q), args...)
}

func (d db) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return d.DB.QueryContext(ctx, rebind(d.pg, q), args...)
}

func (d db) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return d.DB.QueryRowContext(ctx, rebind(d.pg, q), args...)
}

type tx struct {
	*sql.Tx
	pg bool
}

func (t *tx) Exec(q string, args ...any) (sql.Result, error) {
	return t.Tx.Exec(rebind(t.pg, q), args...)
}

func (t *tx) Query(q string, args ...any) (*sql.Rows, error) {
	return t.Tx.Query(rebind(t.pg, q), args...)
}

func (t *tx) QueryRow(q string, args ...any) *sql.Row { return t.Tx.QueryRow(rebind(t.pg, q), args...) }

// insert runs an INSERT and returns the generated key column (RETURNING is
// supported by both Postgres and SQLite >= 3.35).
func (t *tx) insert(q, key string, args ...any) (int64, error) {
	var id int64
	err := t.QueryRow(q+" RETURNING "+key, args...).Scan(&id)
	return id, err
}

// rebind rewrites `?` placeholders to Postgres's $n. Queries never contain a
// literal question mark.
func rebind(pg bool, q string) string {
	if !pg || !strings.Contains(q, "?") {
		return q
	}
	var b strings.Builder
	n := 0
	for _, r := range q {
		if r == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// isUniqueViolation reports whether err is a unique-constraint failure.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return strings.Contains(err.Error(), "UNIQUE")
}

// openDB opens the database for dsn and applies the schema.
func openDB(dsn string) (db, error) {
	if IsPostgresDSN(dsn) {
		conn, err := sql.Open("pgx", dsn)
		if err != nil {
			return db{}, err
		}
		d := db{DB: conn, pg: true}
		// Lock while creating the schema so overlapping instances during a
		// rollout don't race on CREATE ... IF NOT EXISTS.
		if err := d.applySchema("SELECT pg_advisory_xact_lock(?)", postgresSchema); err != nil {
			conn.Close()
			return db{}, err
		}
		return d, nil
	}
	conn, err := sql.Open("sqlite", dsn+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return db{}, err
	}
	// A single connection serializes all access, which makes every
	// transaction (notably the backlog assign) trivially atomic.
	conn.SetMaxOpenConns(1)
	d := db{DB: conn}
	if err := d.applySchema("", sqliteSchema); err != nil {
		conn.Close()
		return db{}, err
	}
	return d, nil
}

func (d db) applySchema(lock, schema string) error {
	t, err := d.Begin()
	if err != nil {
		return err
	}
	defer t.Rollback()
	if lock != "" {
		if _, err := t.Exec(rebind(d.pg, lock), writeLockKey); err != nil {
			return err
		}
	}
	for _, stmt := range strings.Split(schema, ";") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := t.Exec(stmt); err != nil {
			return err
		}
	}
	// Data migrations: idempotent statements safe to run on every boot.
	//
	// The metrics feature was removed (backlog #22). Its table only referenced
	// other tables (workspace, item, creator) and nothing references it, so it
	// can be dropped outright (its unique index goes with it). "Track
	// metric: …" backlog items stay as ordinary items. This step can go once
	// every deployment has booted past it.
	if _, err := t.Exec(`DROP TABLE IF EXISTS metrics`); err != nil {
		return fmt.Errorf("dropping the removed metrics table: %w", err)
	}
	if _, err := t.Exec(migrateRepoURLs); err != nil {
		return fmt.Errorf("migrating repo_url into workspace_repos: %w", err)
	}
	if err := d.addParticipantColumns(t); err != nil {
		return err
	}
	return t.Commit()
}

// addedColumns are columns added to tables after they were first deployed,
// each nullable so existing rows (NULL) keep working unchanged:
//   - user_id: the account a participant is linked to (membership, #15);
//   - sponsor_user_id: the member who invited an agent with a pass (#16);
//   - token_expires_at: when a pass-joined agent's token lapses (sliding,
//     90 days after last use); NULL for everyone else (no expiry);
//   - revoked_at: when the sponsor revoked an agent's credential (#18).
//
// oauth_states.link_user_id (#18) marks a pending OAuth login that links a
// new identity to an already signed-in user; NULL for ordinary sign-ins.
// messages.item_id (#27) ties a chat message to the backlog item it is
// about; NULL for most messages. participants.last_active_at (#37) is an
// agent's last activity (NULL for humans, and for agents until next seen);
// participants.revoke_reason is "idle" for agents logged out after
// AgentIdleTimeout without activity (NULL otherwise).
var addedColumns = []struct{ table, name, sqlite, pg string }{
	{"participants", "user_id", "INTEGER REFERENCES users(id)", "BIGINT REFERENCES users(id)"},
	{"participants", "sponsor_user_id", "INTEGER REFERENCES users(id)", "BIGINT REFERENCES users(id)"},
	{"participants", "token_expires_at", "INTEGER", "BIGINT"},
	{"participants", "revoked_at", "INTEGER", "BIGINT"},
	{"oauth_states", "link_user_id", "INTEGER", "BIGINT"},
	{"messages", "item_id", "INTEGER REFERENCES items(id)", "BIGINT REFERENCES items(id)"},
	{"participants", "last_active_at", "INTEGER", "BIGINT"},
	{"participants", "revoke_reason", "TEXT", "TEXT"},
}

// addParticipantColumns adds addedColumns to databases created before them,
// plus their indexes. Idempotent: it runs on every boot.
func (d db) addParticipantColumns(t *sql.Tx) error {
	for _, c := range addedColumns {
		if err := d.addColumn(t, c.table, c.name, map[bool]string{false: c.sqlite, true: c.pg}[d.pg]); err != nil {
			return fmt.Errorf("adding %s.%s: %w", c.table, c.name, err)
		}
	}
	for _, q := range []string{
		`CREATE INDEX IF NOT EXISTS participants_user ON participants(user_id)`,
		`CREATE INDEX IF NOT EXISTS participants_sponsor ON participants(sponsor_user_id)`,
		`CREATE INDEX IF NOT EXISTS messages_item ON messages(item_id)`,
	} {
		if _, err := t.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// addColumn adds a column unless it exists. It checks first: on Postgres
// even a no-op ALTER TABLE takes an exclusive lock on the table.
func (d db) addColumn(t *sql.Tx, table, column, def string) error {
	exists := `SELECT COUNT(*) FROM pragma_table_info('` + table + `') WHERE name = ?`
	add := `ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + def
	if d.pg {
		exists = `SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = '` + table + `' AND column_name = $1`
		add = `ALTER TABLE ` + table + ` ADD COLUMN IF NOT EXISTS ` + column + ` ` + def
	}
	var n int
	if err := t.QueryRow(exists, column).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := t.Exec(add)
	return err
}

// begin starts a write transaction, taking the writer lock on Postgres.
func (d db) begin(ctx context.Context) (*tx, error) {
	t, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	w := &tx{Tx: t, pg: d.pg}
	if d.pg {
		if _, err := w.Exec("SELECT pg_advisory_xact_lock(?)", writeLockKey); err != nil {
			t.Rollback()
			return nil, err
		}
	}
	return w, nil
}

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS workspaces (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	repo_url TEXT NOT NULL DEFAULT '',
	canvas_url TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS participants (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	name TEXT NOT NULL,
	emoji TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT '',
	token TEXT NOT NULL UNIQUE,
	created_at INTEGER NOT NULL,
	last_seen_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS participants_name
	ON participants(workspace_id, name COLLATE NOCASE);
CREATE TABLE IF NOT EXISTS messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	author_id INTEGER NOT NULL REFERENCES participants(id),
	parent_id INTEGER REFERENCES messages(id),
	body TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS messages_ws ON messages(workspace_id, id);
CREATE TABLE IF NOT EXISTS items (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	title TEXT NOT NULL,
	body TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	position INTEGER NOT NULL,
	assignee_id INTEGER REFERENCES participants(id),
	lease_expires_at INTEGER,
	created_by INTEGER NOT NULL REFERENCES participants(id),
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	closed_at INTEGER,
	close_note TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS items_ws ON items(workspace_id, status, position);
CREATE TABLE IF NOT EXISTS events (
	seq INTEGER PRIMARY KEY AUTOINCREMENT,
	workspace_id TEXT NOT NULL,
	type TEXT NOT NULL,
	actor_id INTEGER,
	data TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS events_ws ON events(workspace_id, seq);
CREATE TABLE IF NOT EXISTS item_comments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	item_id INTEGER NOT NULL REFERENCES items(id),
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	author_id INTEGER NOT NULL REFERENCES participants(id),
	body TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS item_comments_item ON item_comments(item_id, id);
CREATE INDEX IF NOT EXISTS item_comments_ws ON item_comments(workspace_id);
CREATE TABLE IF NOT EXISTS item_prs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	item_id INTEGER NOT NULL REFERENCES items(id),
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	url TEXT NOT NULL,
	added_by INTEGER NOT NULL REFERENCES participants(id),
	created_at INTEGER NOT NULL,
	UNIQUE (item_id, url)
);
CREATE INDEX IF NOT EXISTS item_prs_ws ON item_prs(workspace_id);
CREATE TABLE IF NOT EXISTS workspace_repos (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	url TEXT NOT NULL,
	label TEXT NOT NULL DEFAULT '',
	position INTEGER NOT NULL,
	created_at INTEGER NOT NULL,
	UNIQUE (workspace_id, url)
);
CREATE TABLE IF NOT EXISTS webhooks (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	name TEXT NOT NULL,
	instructions TEXT NOT NULL DEFAULT '',
	token TEXT NOT NULL UNIQUE,
	signing_secret TEXT NOT NULL DEFAULT '',
	enabled INTEGER NOT NULL DEFAULT 1,
	created_by INTEGER NOT NULL REFERENCES participants(id),
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS webhook_deliveries (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	webhook_id INTEGER NOT NULL REFERENCES webhooks(id),
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	received_at INTEGER NOT NULL,
	source TEXT NOT NULL,
	event TEXT NOT NULL DEFAULT '',
	summary TEXT NOT NULL,
	headers TEXT NOT NULL,
	payload TEXT NOT NULL,
	size INTEGER NOT NULL,
	truncated INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS webhook_deliveries_hook ON webhook_deliveries(webhook_id, id);
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	display_name TEXT NOT NULL DEFAULT '',
	avatar_url TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS user_identities (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id),
	provider TEXT NOT NULL,
	subject TEXT NOT NULL,
	email TEXT NOT NULL DEFAULT '',
	email_verified INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	UNIQUE (provider, subject)
);
CREATE INDEX IF NOT EXISTS user_identities_user ON user_identities(user_id);
CREATE TABLE IF NOT EXISTS sessions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id),
	token_hash TEXT NOT NULL UNIQUE,
	kind TEXT NOT NULL,
	label TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	last_seen_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	revoked_at INTEGER
);
CREATE INDEX IF NOT EXISTS sessions_user ON sessions(user_id);
CREATE TABLE IF NOT EXISTS oauth_states (
	state_hash TEXT PRIMARY KEY,
	provider TEXT NOT NULL,
	verifier TEXT NOT NULL,
	return_to TEXT NOT NULL,
	expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS device_codes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	device_code_hash TEXT NOT NULL UNIQUE,
	user_code TEXT NOT NULL UNIQUE,
	status TEXT NOT NULL,
	user_id INTEGER REFERENCES users(id),
	interval_secs INTEGER NOT NULL,
	last_poll_at INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS memberships (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	user_id INTEGER NOT NULL REFERENCES users(id),
	created_at INTEGER NOT NULL,
	UNIQUE (workspace_id, user_id)
);
CREATE TABLE IF NOT EXISTS agent_passes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	pass_hash TEXT NOT NULL UNIQUE,
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	sponsor_user_id INTEGER NOT NULL REFERENCES users(id),
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	used_at INTEGER,
	participant_id INTEGER REFERENCES participants(id)
);
CREATE TABLE IF NOT EXISTS workspace_invites (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	email TEXT,
	token_hash TEXT UNIQUE,
	created_by INTEGER NOT NULL REFERENCES users(id),
	created_at INTEGER NOT NULL,
	expires_at INTEGER,
	uses INTEGER NOT NULL DEFAULT 0,
	used_at INTEGER,
	revoked_at INTEGER
);
CREATE INDEX IF NOT EXISTS workspace_invites_ws ON workspace_invites(workspace_id);
CREATE TABLE IF NOT EXISTS inbox (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	recipient_user_id INTEGER NOT NULL REFERENCES users(id),
	sender_id INTEGER NOT NULL REFERENCES participants(id),
	body TEXT NOT NULL,
	item_id INTEGER REFERENCES items(id),
	url TEXT,
	created_at INTEGER NOT NULL,
	dismissed_at INTEGER
);
CREATE INDEX IF NOT EXISTS inbox_recipient ON inbox(workspace_id, recipient_user_id, id);
CREATE INDEX IF NOT EXISTS inbox_item ON inbox(item_id);
CREATE TABLE IF NOT EXISTS reactions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	message_id INTEGER NOT NULL REFERENCES messages(id),
	participant_id INTEGER NOT NULL REFERENCES participants(id),
	emoji TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	UNIQUE (message_id, participant_id, emoji)
);
CREATE INDEX IF NOT EXISTS reactions_ws ON reactions(workspace_id, message_id);
`

// postgresSchema mirrors sqliteSchema. Timestamps are Unix milliseconds in
// BIGINT; names are unique case-insensitively via an index on lower(name).
const postgresSchema = `
CREATE TABLE IF NOT EXISTS workspaces (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	repo_url TEXT NOT NULL DEFAULT '',
	canvas_url TEXT NOT NULL DEFAULT '',
	created_at BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS participants (
	id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	name TEXT NOT NULL,
	emoji TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT '',
	token TEXT NOT NULL UNIQUE,
	created_at BIGINT NOT NULL,
	last_seen_at BIGINT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS participants_name
	ON participants(workspace_id, lower(name));
CREATE TABLE IF NOT EXISTS messages (
	id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	author_id BIGINT NOT NULL REFERENCES participants(id),
	parent_id BIGINT REFERENCES messages(id),
	body TEXT NOT NULL,
	created_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS messages_ws ON messages(workspace_id, id);
CREATE TABLE IF NOT EXISTS items (
	id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	title TEXT NOT NULL,
	body TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	position BIGINT NOT NULL,
	assignee_id BIGINT REFERENCES participants(id),
	lease_expires_at BIGINT,
	created_by BIGINT NOT NULL REFERENCES participants(id),
	created_at BIGINT NOT NULL,
	updated_at BIGINT NOT NULL,
	closed_at BIGINT,
	close_note TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS items_ws ON items(workspace_id, status, position);
CREATE TABLE IF NOT EXISTS events (
	seq BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	workspace_id TEXT NOT NULL,
	type TEXT NOT NULL,
	actor_id BIGINT,
	data TEXT NOT NULL,
	created_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS events_ws ON events(workspace_id, seq);
CREATE TABLE IF NOT EXISTS item_comments (
	id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	item_id BIGINT NOT NULL REFERENCES items(id),
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	author_id BIGINT NOT NULL REFERENCES participants(id),
	body TEXT NOT NULL,
	created_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS item_comments_item ON item_comments(item_id, id);
CREATE INDEX IF NOT EXISTS item_comments_ws ON item_comments(workspace_id);
CREATE TABLE IF NOT EXISTS item_prs (
	id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	item_id BIGINT NOT NULL REFERENCES items(id),
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	url TEXT NOT NULL,
	added_by BIGINT NOT NULL REFERENCES participants(id),
	created_at BIGINT NOT NULL,
	UNIQUE (item_id, url)
);
CREATE INDEX IF NOT EXISTS item_prs_ws ON item_prs(workspace_id);
CREATE TABLE IF NOT EXISTS workspace_repos (
	id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	url TEXT NOT NULL,
	label TEXT NOT NULL DEFAULT '',
	position BIGINT NOT NULL,
	created_at BIGINT NOT NULL,
	UNIQUE (workspace_id, url)
);
CREATE TABLE IF NOT EXISTS webhooks (
	id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	name TEXT NOT NULL,
	instructions TEXT NOT NULL DEFAULT '',
	token TEXT NOT NULL UNIQUE,
	signing_secret TEXT NOT NULL DEFAULT '',
	enabled BOOLEAN NOT NULL DEFAULT TRUE,
	created_by BIGINT NOT NULL REFERENCES participants(id),
	created_at BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS webhook_deliveries (
	id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	webhook_id BIGINT NOT NULL REFERENCES webhooks(id),
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	received_at BIGINT NOT NULL,
	source TEXT NOT NULL,
	event TEXT NOT NULL DEFAULT '',
	summary TEXT NOT NULL,
	headers TEXT NOT NULL,
	payload TEXT NOT NULL,
	size BIGINT NOT NULL,
	truncated BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX IF NOT EXISTS webhook_deliveries_hook ON webhook_deliveries(webhook_id, id);
CREATE TABLE IF NOT EXISTS users (
	id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	display_name TEXT NOT NULL DEFAULT '',
	avatar_url TEXT NOT NULL DEFAULT '',
	created_at BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS user_identities (
	id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	user_id BIGINT NOT NULL REFERENCES users(id),
	provider TEXT NOT NULL,
	subject TEXT NOT NULL,
	email TEXT NOT NULL DEFAULT '',
	email_verified BOOLEAN NOT NULL DEFAULT FALSE,
	created_at BIGINT NOT NULL,
	UNIQUE (provider, subject)
);
CREATE INDEX IF NOT EXISTS user_identities_user ON user_identities(user_id);
CREATE TABLE IF NOT EXISTS sessions (
	id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	user_id BIGINT NOT NULL REFERENCES users(id),
	token_hash TEXT NOT NULL UNIQUE,
	kind TEXT NOT NULL,
	label TEXT NOT NULL DEFAULT '',
	created_at BIGINT NOT NULL,
	last_seen_at BIGINT NOT NULL,
	expires_at BIGINT NOT NULL,
	revoked_at BIGINT
);
CREATE INDEX IF NOT EXISTS sessions_user ON sessions(user_id);
CREATE TABLE IF NOT EXISTS oauth_states (
	state_hash TEXT PRIMARY KEY,
	provider TEXT NOT NULL,
	verifier TEXT NOT NULL,
	return_to TEXT NOT NULL,
	expires_at BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS device_codes (
	id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	device_code_hash TEXT NOT NULL UNIQUE,
	user_code TEXT NOT NULL UNIQUE,
	status TEXT NOT NULL,
	user_id BIGINT REFERENCES users(id),
	interval_secs BIGINT NOT NULL,
	last_poll_at BIGINT NOT NULL DEFAULT 0,
	created_at BIGINT NOT NULL,
	expires_at BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS memberships (
	id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	user_id BIGINT NOT NULL REFERENCES users(id),
	created_at BIGINT NOT NULL,
	UNIQUE (workspace_id, user_id)
);
CREATE TABLE IF NOT EXISTS agent_passes (
	id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	pass_hash TEXT NOT NULL UNIQUE,
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	sponsor_user_id BIGINT NOT NULL REFERENCES users(id),
	created_at BIGINT NOT NULL,
	expires_at BIGINT NOT NULL,
	used_at BIGINT,
	participant_id BIGINT REFERENCES participants(id)
);
CREATE TABLE IF NOT EXISTS workspace_invites (
	id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	email TEXT,
	token_hash TEXT UNIQUE,
	created_by BIGINT NOT NULL REFERENCES users(id),
	created_at BIGINT NOT NULL,
	expires_at BIGINT,
	uses BIGINT NOT NULL DEFAULT 0,
	used_at BIGINT,
	revoked_at BIGINT
);
CREATE INDEX IF NOT EXISTS workspace_invites_ws ON workspace_invites(workspace_id);
CREATE TABLE IF NOT EXISTS inbox (
	id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	recipient_user_id BIGINT NOT NULL REFERENCES users(id),
	sender_id BIGINT NOT NULL REFERENCES participants(id),
	body TEXT NOT NULL,
	item_id BIGINT REFERENCES items(id),
	url TEXT,
	created_at BIGINT NOT NULL,
	dismissed_at BIGINT
);
CREATE INDEX IF NOT EXISTS inbox_recipient ON inbox(workspace_id, recipient_user_id, id);
CREATE INDEX IF NOT EXISTS inbox_item ON inbox(item_id);
CREATE TABLE IF NOT EXISTS reactions (
	id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	workspace_id TEXT NOT NULL REFERENCES workspaces(id),
	message_id BIGINT NOT NULL REFERENCES messages(id),
	participant_id BIGINT NOT NULL REFERENCES participants(id),
	emoji TEXT NOT NULL,
	created_at BIGINT NOT NULL,
	UNIQUE (message_id, participant_id, emoji)
);
CREATE INDEX IF NOT EXISTS reactions_ws ON reactions(workspace_id, message_id);
`
