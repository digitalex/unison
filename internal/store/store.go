// Package store is Unison's persistence layer: SQLite for local use and
// tests, Postgres in production (see dialect.go).
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
	// ErrForbidden is returned when the actor may not perform an operation
	// (e.g. deleting someone else's comment).
	ErrForbidden = errors.New("forbidden")
)

// Store wraps the database.
type Store struct {
	db  db
	now func() time.Time
}

// Workspace is a shared project space.
type Workspace struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	RepoURL   string `json:"repo_url"` // The first repository's URL (legacy).
	CanvasURL string `json:"canvas_url"`
	CreatedAt int64  `json:"created_at"`
	Repos     []Repo `json:"repos"`
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
	// UserID is the account this participant is linked to (by joining while
	// signed in, or by a claim); 0 when unlinked.
	UserID int64 `json:"user_id,omitempty"`
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
	// PRs are the pull requests linked to the item (several are allowed);
	// CommentCount is how many comments it has.
	PRs          []ItemPR `json:"prs"`
	CommentCount int      `json:"comment_count"`
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

// Open opens (creating the schema if needed) the database named by dsn: a
// Postgres DSN (see IsPostgresDSN) or a SQLite file path.
func Open(dsn string) (*Store, error) {
	d, err := openDB(dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	return &Store{db: d, now: time.Now}, nil
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
func (s *Store) tx(ctx context.Context, fn func(*tx, *eventLog) error) ([]Event, error) {
	t, err := s.db.begin(ctx)
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
	tx     *tx
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
	seq, err := l.tx.insert(`INSERT INTO events (workspace_id, type, actor_id, data, created_at) VALUES (?, ?, ?, ?, ?)`, "seq",
		ws, typ, actorArg, string(raw), l.now)
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
	w.Repos = []Repo{}
	return &w, nil
}

// CreateWorkspace creates a workspace. repoURL (if set) becomes its first
// repository, followed by repos.
func (s *Store) CreateWorkspace(ctx context.Context, name, repoURL, canvasURL string, repos ...RepoInput) (*Workspace, []Event, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil, fmt.Errorf("%w: workspace name is required", ErrInvalid)
	}
	if u := strings.TrimSpace(repoURL); u != "" {
		repos = append([]RepoInput{{URL: u}}, repos...)
	}
	id := "ws_" + randomHex(4)
	var w *Workspace
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		if _, err := t.Exec(`INSERT INTO workspaces (`+workspaceCols+`) VALUES (?, ?, '', ?, ?)`,
			id, name, strings.TrimSpace(canvasURL), log.now); err != nil {
			return err
		}
		for _, in := range repos {
			if _, err := insertRepo(t, id, in, log.now); err != nil {
				return err
			}
		}
		var err error
		if w, err = getWorkspaceTx(t, id); err != nil {
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
	var list []*Workspace
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, w)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if err := s.attachRepos(ctx, list); err != nil {
		return nil, err
	}
	out := make([]Workspace, 0, len(list))
	for _, w := range list {
		out = append(out, *w)
	}
	return out, nil
}

// GetWorkspace returns a workspace by ID, with its repositories.
func (s *Store) GetWorkspace(ctx context.Context, id string) (*Workspace, error) {
	w, err := scanWorkspace(s.db.QueryRowContext(ctx, `SELECT `+workspaceCols+` FROM workspaces WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	return w, s.attachRepos(ctx, []*Workspace{w})
}

// WorkspacePatch holds optional workspace field updates. RepoURL is the
// legacy single-repository field: it edits (or adds, or with "" removes) the
// first repository.
type WorkspacePatch struct {
	Name      *string `json:"name"`
	RepoURL   *string `json:"repo_url"`
	CanvasURL *string `json:"canvas_url"`
}

// UpdateWorkspace applies a patch to a workspace.
func (s *Store) UpdateWorkspace(ctx context.Context, id string, actor int64, p WorkspacePatch) (*Workspace, []Event, error) {
	var w *Workspace
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var err error
		if w, err = getWorkspaceTx(t, id); err != nil {
			return err
		}
		if p.Name != nil {
			if strings.TrimSpace(*p.Name) == "" {
				return fmt.Errorf("%w: workspace name cannot be empty", ErrInvalid)
			}
			w.Name = strings.TrimSpace(*p.Name)
		}
		if p.CanvasURL != nil {
			w.CanvasURL = strings.TrimSpace(*p.CanvasURL)
		}
		if _, err := t.Exec(`UPDATE workspaces SET name = ?, canvas_url = ? WHERE id = ?`, w.Name, w.CanvasURL, id); err != nil {
			return err
		}
		if p.RepoURL != nil {
			if err := setPrimaryRepoURL(t, log, id, actor, *p.RepoURL); err != nil {
				return err
			}
		}
		if w, err = getWorkspaceTx(t, id); err != nil {
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

const participantCols = `id, workspace_id, name, emoji, status, created_at, last_seen_at, user_id`

func scanParticipant(row interface{ Scan(...any) error }) (*Participant, error) {
	var p Participant
	var userID sql.NullInt64
	if err := row.Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.Emoji, &p.Status, &p.CreatedAt, &p.LastSeenAt, &userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.UserID = userID.Int64
	return &p, nil
}

var (
	defaultNames  = []string{"Ada", "Babbage", "Curie", "Dijkstra", "Euler", "Fermat", "Gauss", "Hopper", "Hypatia", "Knuth", "Lovelace", "Noether", "Pascal", "Ramanujan", "Shannon", "Turing"}
	defaultEmojis = []string{"🦊", "🐙", "🦉", "🐝", "🐢", "🦄", "🐧", "🦖", "🐬", "🦜", "🐳", "🦔"}
)

// Join registers a participant in a workspace and returns it with its
// session token. Empty name or emoji are auto-assigned.
func (s *Store) Join(ctx context.Context, wsID, name, emoji string) (*Participant, string, []Event, error) {
	var p *Participant
	var token string
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var err error
		p, token, err = insertParticipant(t, log, wsID, name, emoji, 0)
		return err
	})
	if err != nil {
		return nil, "", nil, err
	}
	return p, token, evs, nil
}

// insertParticipant creates a participant (linked to userID when non-zero)
// and records participant.joined.
func insertParticipant(t *tx, log *eventLog, wsID, name, emoji string, userID int64) (*Participant, string, error) {
	name, emoji = strings.TrimSpace(name), strings.TrimSpace(emoji)
	if strings.ContainsAny(name, " \t\n@") {
		return nil, "", fmt.Errorf("%w: names cannot contain whitespace or '@' (they are used as @mentions)", ErrInvalid)
	}
	if _, err := scanWorkspace(t.QueryRow(`SELECT `+workspaceCols+` FROM workspaces WHERE id = ?`, wsID)); err != nil {
		return nil, "", err
	}
	var count int
	if err := t.QueryRow(`SELECT COUNT(*) FROM participants WHERE workspace_id = ?`, wsID).Scan(&count); err != nil {
		return nil, "", err
	}
	if emoji == "" {
		emoji = defaultEmojis[count%len(defaultEmojis)]
	}
	if name == "" {
		var err error
		if name, err = freeName(t, wsID); err != nil {
			return nil, "", err
		}
	}
	var userArg any
	if userID != 0 {
		userArg = userID
	}
	token := randomHex(16)
	now := log.now
	id, err := t.insert(`INSERT INTO participants (workspace_id, name, emoji, token, created_at, last_seen_at, user_id) VALUES (?, ?, ?, ?, ?, ?, ?)`, "id",
		wsID, name, emoji, token, now, now, userArg)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, "", fmt.Errorf("%w: the name %q is already taken in this workspace", ErrConflict, name)
		}
		return nil, "", err
	}
	p := &Participant{ID: id, WorkspaceID: wsID, Name: name, Emoji: emoji, CreatedAt: now, LastSeenAt: now, UserID: userID}
	return p, token, log.add(wsID, "participant.joined", id, p)
}

func freeName(t *tx, wsID string) (string, error) {
	for round := 1; ; round++ {
		for _, n := range defaultNames {
			if round > 1 {
				n = fmt.Sprintf("%s%d", n, round)
			}
			var exists int
			if err := t.QueryRow(`SELECT COUNT(*) FROM participants WHERE workspace_id = ? AND lower(name) = lower(?)`, wsID, n).Scan(&exists); err != nil {
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
	rows, err := s.db.QueryContext(ctx, `SELECT `+participantCols+` FROM participants WHERE workspace_id = ? ORDER BY last_seen_at DESC, id DESC`, wsID)
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
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
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
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
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
		id, err := t.insert(`INSERT INTO messages (workspace_id, author_id, parent_id, body, created_at) VALUES (?, ?, ?, ?, ?)`, "id",
			author.WorkspaceID, author.ID, parentID, body, log.now)
		if err != nil {
			return err
		}
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
	rows, err := s.db.QueryContext(ctx, `SELECT * FROM (SELECT `+messageCols+` FROM messages WHERE workspace_id = ? ORDER BY id DESC LIMIT ?) AS recent ORDER BY id ASC`, wsID, limit)
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
