// Package store is Unison's SQLite persistence layer.
//
// Every mutation runs in a single transaction and records an Event alongside
// the change it describes. Events are the one feed that powers real-time
// updates for every client: the web UI's stream and the CLI's `unison wait`.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // Registers the pure-Go "sqlite" driver.
)

// LeaseDuration is how long an assignment holds a backlog item before it is
// automatically released back to the queue.
const LeaseDuration = 12 * time.Hour

var (
	// ErrNotFound is returned when a requested record does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict is returned when an operation conflicts with current state
	// (an item already assigned, a name already taken, ...).
	ErrConflict = errors.New("conflict")
	// ErrInvalid is returned for malformed input.
	ErrInvalid = errors.New("invalid input")
)

// Store wraps the SQLite database.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// Workspace is a shared project space.
type Workspace struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	RepoURL   string `json:"repo_url"`
	CanvasURL string `json:"canvas_url"`
	CreatedAt int64  `json:"created_at"`
}

// Participant is a registered human or agent. Unison makes no distinction
// between the two.
type Participant struct {
	ID          int64  `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Emoji       string `json:"emoji"`
	Status      string `json:"status"`
	CreatedAt   int64  `json:"created_at"`
	LastSeenAt  int64  `json:"last_seen_at"`
}

// Message is a chat message. ParentID is set for replies in a thread.
type Message struct {
	ID          int64  `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	AuthorID    int64  `json:"author_id"`
	ParentID    *int64 `json:"parent_id,omitempty"`
	Body        string `json:"body"`
	CreatedAt   int64  `json:"created_at"`
}

// Backlog item statuses.
const (
	StatusOpen     = "open"
	StatusAssigned = "assigned"
	StatusDone     = "done"
)

// Item is a backlog item. Open and assigned items are ordered by Position
// (1 = highest priority); done items have Position 0.
type Item struct {
	ID             int64  `json:"id"`
	WorkspaceID    string `json:"workspace_id"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	Status         string `json:"status"`
	Position       int64  `json:"position"`
	AssigneeID     *int64 `json:"assignee_id,omitempty"`
	LeaseExpiresAt *int64 `json:"lease_expires_at,omitempty"`
	CreatedBy      int64  `json:"created_by"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
	ClosedAt       *int64 `json:"closed_at,omitempty"`
	CloseNote      string `json:"close_note,omitempty"`
}

// Event records a change in a workspace. Seq is globally increasing.
type Event struct {
	Seq         int64           `json:"seq"`
	WorkspaceID string          `json:"workspace_id"`
	Type        string          `json:"type"`
	ActorID     int64           `json:"actor_id,omitempty"`
	Data        json.RawMessage `json:"data"`
	CreatedAt   int64           `json:"created_at"`
}

const schema = `
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

// Open opens (creating if needed) the database at path. Use ":memory:" for
// an ephemeral database.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	// A single connection serializes all access, which makes every
	// transaction (notably the backlog assign) trivially atomic.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}
	return &Store{db: db, now: time.Now}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// SetClock overrides the store's clock. Intended for tests.
func (s *Store) SetClock(now func() time.Time) { s.now = now }

func (s *Store) nowMillis() int64 { return s.now().UnixMilli() }

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// tx runs fn in a transaction and returns the events it recorded.
func (s *Store) tx(ctx context.Context, fn func(*sql.Tx, *eventLog) error) ([]Event, error) {
	t, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	log := &eventLog{tx: t, now: s.nowMillis()}
	if err := fn(t, log); err != nil {
		t.Rollback()
		return nil, err
	}
	if err := t.Commit(); err != nil {
		return nil, err
	}
	return log.events, nil
}

type eventLog struct {
	tx     *sql.Tx
	now    int64
	events []Event
}

func (l *eventLog) add(ws, typ string, actor int64, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	var actorArg any
	if actor != 0 {
		actorArg = actor
	}
	res, err := l.tx.Exec(`INSERT INTO events (workspace_id, type, actor_id, data, created_at) VALUES (?, ?, ?, ?, ?)`,
		ws, typ, actorArg, string(raw), l.now)
	if err != nil {
		return err
	}
	seq, err := res.LastInsertId()
	if err != nil {
		return err
	}
	l.events = append(l.events, Event{Seq: seq, WorkspaceID: ws, Type: typ, ActorID: actor, Data: raw, CreatedAt: l.now})
	return nil
}

// ---- Workspaces ----

const workspaceCols = `id, name, repo_url, canvas_url, created_at`

func scanWorkspace(row interface{ Scan(...any) error }) (*Workspace, error) {
	var w Workspace
	if err := row.Scan(&w.ID, &w.Name, &w.RepoURL, &w.CanvasURL, &w.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &w, nil
}

// CreateWorkspace creates a workspace.
func (s *Store) CreateWorkspace(ctx context.Context, name, repoURL, canvasURL string) (*Workspace, []Event, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil, fmt.Errorf("%w: workspace name is required", ErrInvalid)
	}
	w := &Workspace{ID: "ws_" + randomHex(4), Name: name, RepoURL: strings.TrimSpace(repoURL), CanvasURL: strings.TrimSpace(canvasURL), CreatedAt: s.nowMillis()}
	evs, err := s.tx(ctx, func(t *sql.Tx, log *eventLog) error {
		if _, err := t.Exec(`INSERT INTO workspaces (`+workspaceCols+`) VALUES (?, ?, ?, ?, ?)`,
			w.ID, w.Name, w.RepoURL, w.CanvasURL, w.CreatedAt); err != nil {
			return err
		}
		return log.add(w.ID, "workspace.created", 0, w)
	})
	if err != nil {
		return nil, nil, err
	}
	return w, evs, nil
}

// ListWorkspaces returns all workspaces, newest first.
func (s *Store) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+workspaceCols+` FROM workspaces ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Workspace{}
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *w)
	}
	return out, rows.Err()
}

// GetWorkspace returns a workspace by ID.
func (s *Store) GetWorkspace(ctx context.Context, id string) (*Workspace, error) {
	return scanWorkspace(s.db.QueryRowContext(ctx, `SELECT `+workspaceCols+` FROM workspaces WHERE id = ?`, id))
}

// WorkspacePatch holds optional workspace field updates.
type WorkspacePatch struct {
	Name      *string `json:"name"`
	RepoURL   *string `json:"repo_url"`
	CanvasURL *string `json:"canvas_url"`
}

// UpdateWorkspace applies a patch to a workspace.
func (s *Store) UpdateWorkspace(ctx context.Context, id string, actor int64, p WorkspacePatch) (*Workspace, []Event, error) {
	var w *Workspace
	evs, err := s.tx(ctx, func(t *sql.Tx, log *eventLog) error {
		var err error
		if w, err = scanWorkspace(t.QueryRow(`SELECT `+workspaceCols+` FROM workspaces WHERE id = ?`, id)); err != nil {
			return err
		}
		if p.Name != nil {
			if strings.TrimSpace(*p.Name) == "" {
				return fmt.Errorf("%w: workspace name cannot be empty", ErrInvalid)
			}
			w.Name = strings.TrimSpace(*p.Name)
		}
		if p.RepoURL != nil {
			w.RepoURL = strings.TrimSpace(*p.RepoURL)
		}
		if p.CanvasURL != nil {
			w.CanvasURL = strings.TrimSpace(*p.CanvasURL)
		}
		if _, err := t.Exec(`UPDATE workspaces SET name = ?, repo_url = ?, canvas_url = ? WHERE id = ?`,
			w.Name, w.RepoURL, w.CanvasURL, id); err != nil {
			return err
		}
		return log.add(id, "workspace.updated", actor, w)
	})
	if err != nil {
		return nil, nil, err
	}
	return w, evs, nil
}

// ---- Participants ----

const participantCols = `id, workspace_id, name, emoji, status, created_at, last_seen_at`

func scanParticipant(row interface{ Scan(...any) error }) (*Participant, error) {
	var p Participant
	if err := row.Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.Emoji, &p.Status, &p.CreatedAt, &p.LastSeenAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &p, nil
}

var (
	defaultNames  = []string{"Ada", "Babbage", "Curie", "Dijkstra", "Euler", "Fermat", "Gauss", "Hopper", "Hypatia", "Knuth", "Lovelace", "Noether", "Pascal", "Ramanujan", "Shannon", "Turing"}
	defaultEmojis = []string{"🦊", "🐙", "🦉", "🐝", "🐢", "🦄", "🐧", "🦖", "🐬", "🦜", "🐳", "🦔"}
)

// Join registers a participant in a workspace and returns it with its
// session token. Empty name or emoji are auto-assigned.
func (s *Store) Join(ctx context.Context, wsID, name, emoji string) (*Participant, string, []Event, error) {
	name, emoji = strings.TrimSpace(name), strings.TrimSpace(emoji)
	if strings.ContainsAny(name, " \t\n@") {
		return nil, "", nil, fmt.Errorf("%w: names cannot contain whitespace or '@' (they are used as @mentions)", ErrInvalid)
	}
	token := randomHex(16)
	var p *Participant
	evs, err := s.tx(ctx, func(t *sql.Tx, log *eventLog) error {
		if _, err := scanWorkspace(t.QueryRow(`SELECT `+workspaceCols+` FROM workspaces WHERE id = ?`, wsID)); err != nil {
			return err
		}
		var count int
		if err := t.QueryRow(`SELECT COUNT(*) FROM participants WHERE workspace_id = ?`, wsID).Scan(&count); err != nil {
			return err
		}
		if emoji == "" {
			emoji = defaultEmojis[count%len(defaultEmojis)]
		}
		if name == "" {
			var err error
			if name, err = freeName(t, wsID); err != nil {
				return err
			}
		}
		now := log.now
		res, err := t.Exec(`INSERT INTO participants (workspace_id, name, emoji, token, created_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?)`,
			wsID, name, emoji, token, now, now)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return fmt.Errorf("%w: the name %q is already taken in this workspace", ErrConflict, name)
			}
			return err
		}
		id, _ := res.LastInsertId()
		p = &Participant{ID: id, WorkspaceID: wsID, Name: name, Emoji: emoji, CreatedAt: now, LastSeenAt: now}
		return log.add(wsID, "participant.joined", id, p)
	})
	if err != nil {
		return nil, "", nil, err
	}
	return p, token, evs, nil
}

func freeName(t *sql.Tx, wsID string) (string, error) {
	for round := 1; ; round++ {
		for _, n := range defaultNames {
			if round > 1 {
				n = fmt.Sprintf("%s%d", n, round)
			}
			var exists int
			if err := t.QueryRow(`SELECT COUNT(*) FROM participants WHERE workspace_id = ? AND name = ? COLLATE NOCASE`, wsID, n).Scan(&exists); err != nil {
				return "", err
			}
			if exists == 0 {
				return n, nil
			}
		}
	}
}

// Authenticate resolves a session token to its participant.
func (s *Store) Authenticate(ctx context.Context, token string) (*Participant, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	return scanParticipant(s.db.QueryRowContext(ctx, `SELECT `+participantCols+` FROM participants WHERE token = ?`, token))
}

// Touch records that a participant was just active.
func (s *Store) Touch(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE participants SET last_seen_at = ? WHERE id = ?`, s.nowMillis(), id)
	return err
}

// ListParticipants returns a workspace's participants, most recently seen
// first.
func (s *Store) ListParticipants(ctx context.Context, wsID string) ([]Participant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+participantCols+` FROM participants WHERE workspace_id = ? ORDER BY last_seen_at DESC`, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Participant{}
	for rows.Next() {
		p, err := scanParticipant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// GetParticipant returns a participant by ID within a workspace.
func (s *Store) GetParticipant(ctx context.Context, wsID string, id int64) (*Participant, error) {
	return scanParticipant(s.db.QueryRowContext(ctx, `SELECT `+participantCols+` FROM participants WHERE workspace_id = ? AND id = ?`, wsID, id))
}

// SetStatus updates a participant's free-form status line.
func (s *Store) SetStatus(ctx context.Context, p *Participant, status string) (*Participant, []Event, error) {
	status = strings.TrimSpace(status)
	evs, err := s.tx(ctx, func(t *sql.Tx, log *eventLog) error {
		if _, err := t.Exec(`UPDATE participants SET status = ?, last_seen_at = ? WHERE id = ?`, status, log.now, p.ID); err != nil {
			return err
		}
		p.Status, p.LastSeenAt = status, log.now
		return log.add(p.WorkspaceID, "participant.updated", p.ID, p)
	})
	if err != nil {
		return nil, nil, err
	}
	return p, evs, nil
}

// ---- Chat ----

const messageCols = `id, workspace_id, author_id, parent_id, body, created_at`

func scanMessage(row interface{ Scan(...any) error }) (*Message, error) {
	var m Message
	var parent sql.NullInt64
	if err := row.Scan(&m.ID, &m.WorkspaceID, &m.AuthorID, &parent, &m.Body, &m.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if parent.Valid {
		m.ParentID = &parent.Int64
	}
	return &m, nil
}

// PostMessage posts a chat message, optionally as a reply to parentID.
func (s *Store) PostMessage(ctx context.Context, author *Participant, body string, parentID *int64) (*Message, []Event, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, nil, fmt.Errorf("%w: message body is required", ErrInvalid)
	}
	var m *Message
	evs, err := s.tx(ctx, func(t *sql.Tx, log *eventLog) error {
		if parentID != nil {
			parent, err := scanMessage(t.QueryRow(`SELECT `+messageCols+` FROM messages WHERE id = ? AND workspace_id = ?`, *parentID, author.WorkspaceID))
			if err != nil {
				return fmt.Errorf("parent message %d: %w", *parentID, err)
			}
			if parent.ParentID != nil {
				// Threads are one level deep: replying to a reply joins its thread.
				parentID = parent.ParentID
			}
		}
		res, err := t.Exec(`INSERT INTO messages (workspace_id, author_id, parent_id, body, created_at) VALUES (?, ?, ?, ?, ?)`,
			author.WorkspaceID, author.ID, parentID, body, log.now)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		m = &Message{ID: id, WorkspaceID: author.WorkspaceID, AuthorID: author.ID, ParentID: parentID, Body: body, CreatedAt: log.now}
		return log.add(author.WorkspaceID, "message.created", author.ID, m)
	})
	if err != nil {
		return nil, nil, err
	}
	return m, evs, nil
}

// ListMessages returns up to limit of the most recent messages, oldest first.
func (s *Store) ListMessages(ctx context.Context, wsID string, limit int) ([]Message, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT * FROM (SELECT `+messageCols+` FROM messages WHERE workspace_id = ? ORDER BY id DESC LIMIT ?) ORDER BY id ASC`, wsID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// ---- Events ----

// EventsAfter returns up to limit events in a workspace with seq > after.
func (s *Store) EventsAfter(ctx context.Context, wsID string, after int64, limit int) ([]Event, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT seq, workspace_id, type, COALESCE(actor_id, 0), data, created_at FROM events WHERE workspace_id = ? AND seq > ? ORDER BY seq LIMIT ?`, wsID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var data string
		if err := rows.Scan(&e.Seq, &e.WorkspaceID, &e.Type, &e.ActorID, &data, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Data = json.RawMessage(data)
		out = append(out, e)
	}
	return out, rows.Err()
}

// LatestSeq returns the newest event seq in a workspace (0 if none).
func (s *Store) LatestSeq(ctx context.Context, wsID string) (int64, error) {
	var seq int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM events WHERE workspace_id = ?`, wsID).Scan(&seq)
	return seq, err
}
