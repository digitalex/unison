package store

import (
	"context"
	"database/sql"
	"errors"
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
	return t.Commit()
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
`
