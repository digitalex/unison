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
	"slices"
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
	// ErrRevoked is returned by Authenticate for a valid token that no longer
	// works: an agent credential idle past its expiry, or whose sponsor left.
	ErrRevoked = errors.New("credential no longer valid")
	// ErrIdle is returned (along with ErrRevoked) for an agent logged out
	// after AgentIdleTimeout without activity.
	ErrIdle = errors.New("logged out for inactivity")
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
	// Sponsor is set for agents that joined with a pass: the member who
	// minted it, whom the agent acts for.
	Sponsor *Sponsor `json:"sponsor,omitempty"`
	// TokenExpiresAt is when a pass-joined agent's token lapses unless used
	// (each use pushes it AgentCredentialTTL ahead); 0 means no expiry.
	TokenExpiresAt int64 `json:"token_expires_at,omitempty"`
	// RevokedAt is when an agent's credential was revoked (by its sponsor,
	// or RevokeReason "idle": logged out after AgentIdleTimeout without
	// activity); 0 if not.
	RevokedAt    int64  `json:"revoked_at,omitempty"`
	RevokeReason string `json:"revoke_reason,omitempty"`
	// LastActiveAt is an agent's last activity (see MarkActive); 0 for
	// humans, and for agents not seen since this was introduced.
	LastActiveAt int64 `json:"last_active_at,omitempty"`
}

// Sponsor is the member an agent acts for.
type Sponsor struct {
	UserID      int64  `json:"user_id"`
	DisplayName string `json:"display_name"`
}

// Message is a chat message. ParentID is set for replies in a thread.
type Message struct {
	ID          int64  `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	AuthorID    int64  `json:"author_id"`
	ParentID    *int64 `json:"parent_id,omitempty"`
	Body        string `json:"body"`
	CreatedAt   int64  `json:"created_at"`
	// ItemID ties the message to a backlog item of the same workspace (a
	// status update about it, say); Item summarizes that item.
	ItemID *int64       `json:"item_id,omitempty"`
	Item   *MessageItem `json:"item,omitempty"`
	// Reactions are the emoji on the message, in ReactionEmoji order.
	Reactions []Reaction `json:"reactions,omitempty"`
	// ActionRequired marks a message that asks the participants it
	// @mentions to act (#49): ActionFor are they, DoneBy those of them
	// whose account has marked it done (see SetDone).
	ActionRequired bool    `json:"action_required,omitempty"`
	ActionFor      []int64 `json:"action_for,omitempty"`
	DoneBy         []int64 `json:"done_by,omitempty"`
}

// NewMessage is a chat message to post: a reply in ParentID's thread,
// about backlog item ItemID, asking the participants it @mentions to act
// (ActionRequired); all optional but the body.
type NewMessage struct {
	Body           string
	ParentID       *int64
	ItemID         *int64
	ActionRequired bool
}

// MessageItem is the backlog item a message is about, in brief.
type MessageItem struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
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
	// ScheduleID is the schedule this item is a run of (#112), if any.
	ScheduleID *int64 `json:"schedule_id,omitempty"`
	// PRs are the pull requests linked to the item (several are allowed);
	// CommentCount is how many comments it has, MessageCount how many chat
	// messages are about it.
	PRs          []ItemPR `json:"prs"`
	CommentCount int      `json:"comment_count"`
	MessageCount int      `json:"message_count"`
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
	return s.createWorkspace(ctx, 0, name, repoURL, canvasURL, repos...)
}

// CreateWorkspaceFor creates a workspace whose creator (a signed-in user)
// becomes its first member, in the same transaction.
func (s *Store) CreateWorkspaceFor(ctx context.Context, creatorID int64, name, repoURL, canvasURL string, repos ...RepoInput) (*Workspace, []Event, error) {
	return s.createWorkspace(ctx, creatorID, name, repoURL, canvasURL, repos...)
}

func (s *Store) createWorkspace(ctx context.Context, creatorID int64, name, repoURL, canvasURL string, repos ...RepoInput) (*Workspace, []Event, error) {
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
		if err := log.add(w.ID, "workspace.created", 0, w); err != nil {
			return err
		}
		if creatorID == 0 {
			return nil
		}
		return ensureMembership(t, log, w.ID, creatorID, 0)
	})
	if err != nil {
		return nil, nil, err
	}
	return w, evs, nil
}

// ListWorkspaces returns all workspaces, newest first.
func (s *Store) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	return s.listWorkspaces(ctx, `SELECT `+workspaceCols+` FROM workspaces ORDER BY created_at DESC`)
}

// ListWorkspacesFor returns the workspaces userID is a member of, newest
// first.
func (s *Store) ListWorkspacesFor(ctx context.Context, userID int64) ([]Workspace, error) {
	return s.listWorkspaces(ctx, `SELECT `+workspaceCols+` FROM workspaces
		WHERE id IN (SELECT workspace_id FROM memberships WHERE user_id = ?) ORDER BY created_at DESC`, userID)
}

func (s *Store) listWorkspaces(ctx context.Context, q string, args ...any) ([]Workspace, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
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

// DeleteWorkspace permanently deletes a workspace and all its associated
// data (messages, backlog items, comments, PRs, webhooks, passes, invites,
// participants, events). Global user accounts are untouched.
func (s *Store) DeleteWorkspace(ctx context.Context, id string) error {
	_, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var name string
		if err := t.QueryRow(`SELECT name FROM workspaces WHERE id = ?`, id).Scan(&name); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		for _, q := range []string{
			`DELETE FROM webhook_deliveries WHERE workspace_id = ?`,
			`DELETE FROM webhooks WHERE workspace_id = ?`,
			`DELETE FROM workspace_repos WHERE workspace_id = ?`,
			`DELETE FROM schedules WHERE workspace_id = ?`,
			`DELETE FROM item_comments WHERE workspace_id = ?`,
			`DELETE FROM item_prs WHERE workspace_id = ?`,
			`DELETE FROM reactions WHERE workspace_id = ?`,
			`DELETE FROM message_actions WHERE workspace_id = ?`,
			`UPDATE messages SET parent_id = NULL WHERE workspace_id = ?`,
			`DELETE FROM messages WHERE workspace_id = ?`,
			`DELETE FROM items WHERE workspace_id = ?`,
			`DELETE FROM agent_passes WHERE workspace_id = ?`,
			`DELETE FROM workspace_invites WHERE workspace_id = ?`,
			`DELETE FROM memberships WHERE workspace_id = ?`,
			`DELETE FROM participants WHERE workspace_id = ?`,
			`DELETE FROM events WHERE workspace_id = ?`,
			`DELETE FROM workspaces WHERE id = ?`,
		} {
			if _, err := t.Exec(q, id); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// ---- Participants ----

// participantCols are selected FROM participants (unaliased).
const participantCols = `id, workspace_id, name, emoji, status, created_at, last_seen_at, user_id, sponsor_user_id,
	(SELECT display_name FROM users WHERE users.id = participants.sponsor_user_id), token_expires_at, revoked_at,
	revoke_reason, last_active_at`

// scanParticipant scans participantCols, then any extra columns into extra.
func scanParticipant(row interface{ Scan(...any) error }, extra ...any) (*Participant, error) {
	var p Participant
	var userID, sponsorID, expires, revoked, active sql.NullInt64
	var sponsorName, reason sql.NullString
	dest := append([]any{&p.ID, &p.WorkspaceID, &p.Name, &p.Emoji, &p.Status, &p.CreatedAt, &p.LastSeenAt, &userID, &sponsorID, &sponsorName, &expires, &revoked,
		&reason, &active}, extra...)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.UserID, p.TokenExpiresAt, p.RevokedAt = userID.Int64, expires.Int64, revoked.Int64
	p.RevokeReason, p.LastActiveAt = reason.String, active.Int64
	if sponsorID.Valid {
		p.Sponsor = &Sponsor{UserID: sponsorID.Int64, DisplayName: sponsorName.String}
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
	var p *Participant
	var token string
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var err error
		p, token, err = insertParticipant(t, log, wsID, name, emoji, participantLink{})
		return err
	})
	if err != nil {
		return nil, "", nil, err
	}
	return p, token, evs, nil
}

// participantLink says whom a new participant belongs to: an account
// (UserID), or, for an agent, the member it acts for (SponsorUserID, with a
// sliding token expiry). The zero value is a plain, unlinked participant.
type participantLink struct {
	UserID, SponsorUserID, TokenExpiresAt int64
}

func nullable(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// insertParticipant creates a participant and records participant.joined.
func insertParticipant(t *tx, log *eventLog, wsID, name, emoji string, link participantLink) (*Participant, string, error) {
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
	var sponsor *Sponsor
	if link.SponsorUserID != 0 {
		sponsorName, err := displayName(t, link.SponsorUserID)
		if err != nil {
			return nil, "", err
		}
		sponsor = &Sponsor{UserID: link.SponsorUserID, DisplayName: sponsorName}
	}
	token := randomHex(16)
	now := log.now
	var lastActive int64 // Agents start active; humans aren't tracked.
	if link.SponsorUserID != 0 {
		lastActive = now
	}
	id, err := t.insert(`INSERT INTO participants (workspace_id, name, emoji, token, created_at, last_seen_at, user_id, sponsor_user_id, token_expires_at, last_active_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "id",
		wsID, name, emoji, token, now, now, nullable(link.UserID), nullable(link.SponsorUserID), nullable(link.TokenExpiresAt), nullable(lastActive))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, "", fmt.Errorf("%w: the name %q is already taken in this workspace", ErrConflict, name)
		}
		return nil, "", err
	}
	p := &Participant{ID: id, WorkspaceID: wsID, Name: name, Emoji: emoji, CreatedAt: now, LastSeenAt: now,
		UserID: link.UserID, Sponsor: sponsor, TokenExpiresAt: link.TokenExpiresAt, LastActiveAt: lastActive}
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

// Authenticate resolves a session token to its participant. Unknown tokens
// are ErrNotFound. Agents that joined with a pass are ErrRevoked once their
// token has lapsed (AgentCredentialTTL without use) or their sponsor is no
// longer a member; other participants never expire.
func (s *Store) Authenticate(ctx context.Context, token string) (*Participant, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	var sponsorMember bool
	p, err := scanParticipant(s.db.QueryRowContext(ctx, `SELECT `+participantCols+`,
		sponsor_user_id IS NULL OR EXISTS (SELECT 1 FROM memberships m WHERE m.workspace_id = participants.workspace_id AND m.user_id = participants.sponsor_user_id)
		FROM participants WHERE token = ?`, token), &sponsorMember)
	if err != nil {
		return nil, err
	}
	if p.RevokedAt != 0 && p.RevokeReason == revokedIdle {
		return nil, idleError(p)
	}
	if p.RevokedAt != 0 {
		return nil, fmt.Errorf("%w: %s revoked this agent credential; ask them for a new invite if you should keep working here",
			ErrRevoked, p.Sponsor.DisplayName)
	}
	if err := s.checkIdle(ctx, p); err != nil {
		return nil, err
	}
	if p.TokenExpiresAt != 0 && p.TokenExpiresAt <= s.nowMillis() {
		return nil, fmt.Errorf("%w: this agent credential expired after %d days without use; ask your human for a new invite (`unison pass`)",
			ErrRevoked, int(AgentCredentialTTL.Hours()/24))
	}
	if !sponsorMember {
		return nil, fmt.Errorf("%w: %s, who invited this agent, is no longer a member of the workspace; ask a member for a new invite",
			ErrRevoked, p.Sponsor.DisplayName)
	}
	return p, nil
}

// Touch records that a participant was just active. For agents it also
// slides the credential's expiry (callers touch at most every few seconds).
func (s *Store) Touch(ctx context.Context, id int64) error {
	now := s.nowMillis()
	_, err := s.db.ExecContext(ctx, `UPDATE participants SET last_seen_at = ?,
		token_expires_at = CASE WHEN token_expires_at IS NULL THEN NULL ELSE CAST(? AS BIGINT) END WHERE id = ?`,
		now, now+AgentCredentialTTL.Milliseconds(), id)
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

// selectMessages selects messages (alias m) with the item each is about.
const selectMessages = `SELECT m.id, m.workspace_id, m.author_id, m.parent_id, m.body, m.created_at, m.item_id, i.title, i.status
	FROM messages m LEFT JOIN items i ON i.id = m.item_id`

func scanMessage(row interface{ Scan(...any) error }) (*Message, error) {
	var m Message
	var parent, item sql.NullInt64
	var title, status sql.NullString
	if err := row.Scan(&m.ID, &m.WorkspaceID, &m.AuthorID, &parent, &m.Body, &m.CreatedAt, &item, &title, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if parent.Valid {
		m.ParentID = &parent.Int64
	}
	if item.Valid {
		m.ItemID = &item.Int64
		m.Item = &MessageItem{ID: item.Int64, Title: title.String, Status: status.String}
	}
	return &m, nil
}

func scanMessages(rows *sql.Rows, err error) ([]Message, error) {
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

// PostMessage posts a chat message, optionally as a reply to parentID and
// about backlog item itemID: Post without asking anyone to act.
func (s *Store) PostMessage(ctx context.Context, author *Participant, body string, parentID, itemID *int64) (*Message, []Event, error) {
	return s.Post(ctx, author, NewMessage{Body: body, ParentID: parentID, ItemID: itemID})
}

// Post posts a chat message, optionally as a reply in a thread, about a
// backlog item (which must be in the author's workspace), or asking the
// participants it @mentions to act. Replies inherit no item: each message
// says what it is about. A reply marks its thread's root done for the
// author's account, if the root asked them to act.
func (s *Store) Post(ctx context.Context, author *Participant, msg NewMessage) (*Message, []Event, error) {
	body, parentID, itemID := strings.TrimSpace(msg.Body), msg.ParentID, msg.ItemID
	if body == "" {
		return nil, nil, fmt.Errorf("%w: message body is required", ErrInvalid)
	}
	var m *Message
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		if parentID != nil {
			parent, err := scanMessage(t.QueryRow(selectMessages+` WHERE m.id = ? AND m.workspace_id = ?`, *parentID, author.WorkspaceID))
			if err != nil {
				return fmt.Errorf("parent message %d: %w", *parentID, err)
			}
			if parent.ParentID != nil {
				// Threads are one level deep: replying to a reply joins its thread.
				parentID = parent.ParentID
			}
		}
		var about *MessageItem
		if itemID != nil {
			it, err := getItem(t, author.WorkspaceID, *itemID)
			if errors.Is(err, ErrNotFound) {
				return fmt.Errorf("%w: there is no backlog item #%d in this workspace", ErrInvalid, *itemID)
			} else if err != nil {
				return err
			}
			about = &MessageItem{ID: it.ID, Title: it.Title, Status: it.Status}
		}
		id, err := t.insert(`INSERT INTO messages (workspace_id, author_id, parent_id, body, created_at, item_id) VALUES (?, ?, ?, ?, ?, ?)`, "id",
			author.WorkspaceID, author.ID, parentID, body, log.now, itemID)
		if err != nil {
			return err
		}
		m = &Message{ID: id, WorkspaceID: author.WorkspaceID, AuthorID: author.ID, ParentID: parentID, Body: body, CreatedAt: log.now, ItemID: itemID, Item: about}
		if msg.ActionRequired {
			if err := addActions(t, m); err != nil {
				return err
			}
		}
		if parentID != nil {
			if err := markDoneIfAsked(t, author, *parentID, log.now); err != nil {
				return err
			}
		}
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
	return s.withDetails(ctx, wsID)(scanMessages(s.db.QueryContext(ctx, `SELECT * FROM (`+selectMessages+` WHERE m.workspace_id = ? ORDER BY m.id DESC LIMIT ?) AS recent ORDER BY id ASC`, wsID, limit)))
}

// withDetails returns a function that fills in the reactions and actions
// of the messages it is given (passing errors through).
func (s *Store) withDetails(ctx context.Context, wsID string) func([]Message, error) ([]Message, error) {
	return func(msgs []Message, err error) ([]Message, error) {
		if err != nil {
			return nil, err
		}
		query := func(q string, args ...any) (*sql.Rows, error) { return s.db.QueryContext(ctx, q, args...) }
		return msgs, attachDetails(query, wsID, msgs)
	}
}

// ItemMessages returns the chat messages about a backlog item, oldest
// first, or ErrNotFound when the workspace has no such item.
func (s *Store) ItemMessages(ctx context.Context, wsID string, itemID int64) ([]Message, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE workspace_id = ? AND id = ?`, wsID, itemID).Scan(&n); err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, ErrNotFound
	}
	return s.withDetails(ctx, wsID)(scanMessages(s.db.QueryContext(ctx, selectMessages+` WHERE m.workspace_id = ? AND m.item_id = ? ORDER BY m.id`, wsID, itemID)))
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
	return scanEvents(rows)
}

// ActivityTypes are the events chat shows as one-line activity updates
// (#93, #97): backlog items added, assigned (not renewed), closed and
// reopened, and people joining and leaving.
var ActivityTypes = []string{"backlog.added", "backlog.assigned", "backlog.closed", "backlog.reopened", "participant.joined", "member.removed"}

// RecentActivity returns the workspace's latest activity events (see
// ActivityTypes), oldest first: at most limit of them (200 by default).
func (s *Store) RecentActivity(ctx context.Context, wsID string, limit int) ([]Event, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	args := []any{wsID}
	for _, t := range ActivityTypes {
		args = append(args, t)
	}
	args = append(args, limit)
	marks := strings.TrimSuffix(strings.Repeat("?, ", len(ActivityTypes)), ", ")
	rows, err := s.db.QueryContext(ctx, `SELECT seq, workspace_id, type, COALESCE(actor_id, 0), data, created_at FROM events
		WHERE workspace_id = ? AND type IN (`+marks+`) ORDER BY seq DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	out, err := scanEvents(rows)
	slices.Reverse(out)
	return out, err
}

func scanEvents(rows *sql.Rows) ([]Event, error) {
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
