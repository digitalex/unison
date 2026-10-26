package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Invites let people (humans; agents use passes) become members. A member
// invites by email (anyone who signs in with that verified email may join,
// once) or by link (a revocable, multi-use token that expires). Every
// member may invite and revoke: members are equal.

// Invite kinds.
const (
	InviteEmail = "email"
	InviteLink  = "link"
)

const (
	// InviteLinkTTL is how long an invite link works by default.
	InviteLinkTTL = 7 * 24 * time.Hour
	// InviteLinkMaxTTL bounds the expiry a member may choose.
	InviteLinkMaxTTL = 90 * 24 * time.Hour
	// InviteTokenPrefix marks invite-link tokens ("ui_…").
	InviteTokenPrefix = "ui_"
)

// Invite is a pending (or used, revoked, expired) workspace invite. The
// link token is never stored, only its hash.
type Invite struct {
	ID          int64  `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Kind        string `json:"kind"`
	Email       string `json:"email,omitempty"`
	CreatedBy   int64  `json:"created_by"`
	CreatorName string `json:"created_by_name"`
	CreatedAt   int64  `json:"created_at"`
	ExpiresAt   int64  `json:"expires_at,omitempty"`
	Uses        int64  `json:"uses"`
	UsedAt      int64  `json:"used_at,omitempty"`
	RevokedAt   int64  `json:"revoked_at,omitempty"`
}

// Active reports whether the invite can still be used at time now.
func (i *Invite) Active(now int64) bool {
	return i.RevokedAt == 0 && i.UsedAt == 0 && (i.ExpiresAt == 0 || i.ExpiresAt > now)
}

const inviteCols = `id, workspace_id, email, token_hash IS NOT NULL, created_by,
	(SELECT display_name FROM users WHERE users.id = workspace_invites.created_by), created_at, expires_at, uses, used_at, revoked_at`

func scanInvite(row interface{ Scan(...any) error }) (*Invite, error) {
	var i Invite
	var email, creator sql.NullString
	var link bool
	var expires, used, revoked sql.NullInt64
	if err := row.Scan(&i.ID, &i.WorkspaceID, &email, &link, &i.CreatedBy, &creator, &i.CreatedAt, &expires, &i.Uses, &used, &revoked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	i.Email, i.CreatorName = email.String, creator.String
	i.ExpiresAt, i.UsedAt, i.RevokedAt = expires.Int64, used.Int64, revoked.Int64
	i.Kind = InviteEmail
	if link {
		i.Kind = InviteLink
	}
	return &i, nil
}

func requireMember(t *tx, wsID string, userID int64, what string) error {
	if _, err := scanWorkspace(t.QueryRow(`SELECT `+workspaceCols+` FROM workspaces WHERE id = ?`, wsID)); err != nil {
		return err
	}
	member, err := isMember(t, wsID, userID)
	if err != nil {
		return err
	}
	if !member {
		return fmt.Errorf("%w: only members can %s", ErrForbidden, what)
	}
	return nil
}

func inviteActor(t *tx, wsID string, userID int64) int64 {
	if p, _, err := usersParticipant(t, wsID, userID); err == nil {
		return p.ID
	}
	return 0
}

// inviteEvent is the (secret-free) data of invite.* events.
func inviteEvent(i *Invite) map[string]any {
	return map[string]any{"id": i.ID, "kind": i.Kind, "email": i.Email, "expires_at": i.ExpiresAt}
}

// CreateEmailInvite invites an email address to the workspace. An address
// that already has an active invite gets that one back.
func (s *Store) CreateEmailInvite(ctx context.Context, wsID string, byUserID int64, email string) (*Invite, []Event, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") || strings.ContainsAny(email, " \t\r\n") {
		return nil, nil, fmt.Errorf("%w: %q is not an email address", ErrInvalid, email)
	}
	var inv *Invite
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		if err := requireMember(t, wsID, byUserID, "invite people"); err != nil {
			return err
		}
		existing, err := scanInvite(t.QueryRow(`SELECT `+inviteCols+` FROM workspace_invites
			WHERE workspace_id = ? AND email = ? AND used_at IS NULL AND revoked_at IS NULL ORDER BY id DESC LIMIT 1`, wsID, email))
		if err == nil {
			inv = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		id, err := t.insert(`INSERT INTO workspace_invites (workspace_id, email, created_by, created_at) VALUES (?, ?, ?, ?)`, "id",
			wsID, email, byUserID, log.now)
		if err != nil {
			return err
		}
		if inv, err = scanInvite(t.QueryRow(`SELECT `+inviteCols+` FROM workspace_invites WHERE id = ?`, id)); err != nil {
			return err
		}
		return log.add(wsID, "invite.created", inviteActor(t, wsID, byUserID), inviteEvent(inv))
	})
	if err != nil {
		return nil, nil, err
	}
	return inv, evs, nil
}

// CreateLinkInvite creates an invite link token (returned once; only its
// hash is stored) that anyone signed in may use until it expires (ttl;
// InviteLinkTTL when 0) or is revoked.
func (s *Store) CreateLinkInvite(ctx context.Context, wsID string, byUserID int64, ttl time.Duration) (*Invite, string, []Event, error) {
	if ttl == 0 {
		ttl = InviteLinkTTL
	}
	if ttl < time.Minute || ttl > InviteLinkMaxTTL {
		return nil, "", nil, fmt.Errorf("%w: invite links expire after 1 minute to %d days", ErrInvalid, int(InviteLinkMaxTTL.Hours()/24))
	}
	token := randomToken(InviteTokenPrefix, 24)
	var inv *Invite
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		if err := requireMember(t, wsID, byUserID, "invite people"); err != nil {
			return err
		}
		id, err := t.insert(`INSERT INTO workspace_invites (workspace_id, token_hash, created_by, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`, "id",
			wsID, HashToken(token), byUserID, log.now, log.now+ttl.Milliseconds())
		if err != nil {
			return err
		}
		if inv, err = scanInvite(t.QueryRow(`SELECT `+inviteCols+` FROM workspace_invites WHERE id = ?`, id)); err != nil {
			return err
		}
		return log.add(wsID, "invite.created", inviteActor(t, wsID, byUserID), inviteEvent(inv))
	})
	if err != nil {
		return nil, "", nil, err
	}
	return inv, token, evs, nil
}

// ListInvites returns the workspace's active invites, newest first.
func (s *Store) ListInvites(ctx context.Context, wsID string) ([]Invite, error) {
	now := s.nowMillis()
	rows, err := s.db.QueryContext(ctx, `SELECT `+inviteCols+` FROM workspace_invites
		WHERE workspace_id = ? AND used_at IS NULL AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?) ORDER BY id DESC`, wsID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		i, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *i)
	}
	return out, rows.Err()
}

// RevokeInvite stops an invite; any member may revoke any invite.
func (s *Store) RevokeInvite(ctx context.Context, wsID string, byUserID, id int64) (*Invite, []Event, error) {
	var inv *Invite
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		if err := requireMember(t, wsID, byUserID, "revoke invites"); err != nil {
			return err
		}
		var err error
		inv, err = scanInvite(t.QueryRow(`SELECT `+inviteCols+` FROM workspace_invites WHERE id = ? AND workspace_id = ?`, id, wsID))
		if err != nil {
			return fmt.Errorf("invite %d: %w", id, err)
		}
		if inv.RevokedAt != 0 {
			return nil
		}
		if _, err := t.Exec(`UPDATE workspace_invites SET revoked_at = ? WHERE id = ?`, log.now, id); err != nil {
			return err
		}
		inv.RevokedAt = log.now
		return log.add(wsID, "invite.revoked", inviteActor(t, wsID, byUserID), inviteEvent(inv))
	})
	if err != nil {
		return nil, nil, err
	}
	return inv, evs, nil
}

// InviteTarget describes an invite link's workspace, for the join page.
type InviteTarget struct {
	WorkspaceID   string `json:"workspace_id"`
	WorkspaceName string `json:"workspace_name"`
	ExpiresAt     int64  `json:"expires_at"`
}

// LookupInviteLink returns the workspace an active invite link is for, or
// ErrNotFound (unknown, revoked or expired: callers can't tell which).
func (s *Store) LookupInviteLink(ctx context.Context, token string) (*InviteTarget, error) {
	var l InviteTarget
	var expires sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT i.workspace_id, w.name, i.expires_at FROM workspace_invites i JOIN workspaces w ON w.id = i.workspace_id
		WHERE i.token_hash = ? AND i.revoked_at IS NULL AND (i.expires_at IS NULL OR i.expires_at > ?)`, HashToken(token), s.nowMillis()).
		Scan(&l.WorkspaceID, &l.WorkspaceName, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: this invite link is not valid (revoked or expired); ask a member for a new one", ErrNotFound)
	}
	l.ExpiresAt = expires.Int64
	return &l, err
}

// acceptInvite lets a signed-in user who is not a member in: with an active
// invite link token for the workspace, or an active email invite matching
// one of the user's verified emails (which it uses up). Otherwise
// ErrForbidden.
func acceptInvite(t *tx, log *eventLog, wsID string, userID int64, token string) error {
	if token != "" {
		var id int64
		err := t.QueryRow(`SELECT id FROM workspace_invites WHERE token_hash = ? AND workspace_id = ? AND revoked_at IS NULL
			AND (expires_at IS NULL OR expires_at > ?)`, HashToken(token), wsID, log.now).Scan(&id)
		if err == nil {
			_, err = t.Exec(`UPDATE workspace_invites SET uses = uses + 1 WHERE id = ?`, id)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		return fmt.Errorf("%w: this invite link is not valid for this workspace (revoked or expired); ask a member for a new one", ErrForbidden)
	}
	rows, err := t.Query(`SELECT lower(email) FROM user_identities WHERE user_id = ? AND email_verified = ? AND email <> ''`, userID, true)
	if err != nil {
		return err
	}
	var emails []any
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			rows.Close()
			return err
		}
		emails = append(emails, e)
	}
	rows.Close()
	if len(emails) > 0 {
		marks := strings.TrimSuffix(strings.Repeat("?, ", len(emails)), ", ")
		inv, err := scanInvite(t.QueryRow(`SELECT `+inviteCols+` FROM workspace_invites WHERE workspace_id = ? AND email IN (`+marks+`)
			AND used_at IS NULL AND revoked_at IS NULL ORDER BY id LIMIT 1`, append([]any{wsID}, emails...)...))
		if err == nil {
			if _, err := t.Exec(`UPDATE workspace_invites SET used_at = ?, uses = uses + 1 WHERE id = ?`, log.now, inv.ID); err != nil {
				return err
			}
			inv.UsedAt = log.now
			return log.add(wsID, "invite.used", 0, inviteEvent(inv))
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	return fmt.Errorf("%w: you're not a member of this workspace; ask a member for an invite (by link, or to one of your verified emails)", ErrForbidden)
}

// JoinWorkspace is the enforced join for signed-in humans: members join (or
// get their existing participant back, as JoinAsUser); anyone else needs an
// invite (see acceptInvite). A removed member is not a member, so they need
// a fresh invite too.
func (s *Store) JoinWorkspace(ctx context.Context, wsID string, userID int64, name, emoji, inviteToken string) (p *Participant, token string, existing bool, evs []Event, err error) {
	evs, err = s.tx(ctx, func(t *tx, log *eventLog) error {
		if _, err := scanWorkspace(t.QueryRow(`SELECT `+workspaceCols+` FROM workspaces WHERE id = ?`, wsID)); err != nil {
			return err
		}
		member, err := isMember(t, wsID, userID)
		if err != nil {
			return err
		}
		if !member {
			if err := acceptInvite(t, log, wsID, userID, inviteToken); err != nil {
				return err
			}
		}
		p, token, existing, err = joinAsUser(t, log, wsID, userID, name, emoji)
		return err
	})
	if err != nil {
		return nil, "", false, nil, err
	}
	return p, token, existing, evs, nil
}

// CheckPass reports whether pass would let an agent join wsID now, without
// using it up (the doorway shows workspace details to pass holders).
func (s *Store) CheckPass(ctx context.Context, wsID, pass string) error {
	var expires int64
	var used sql.NullInt64
	var sponsor int64
	err := s.db.QueryRowContext(ctx, `SELECT expires_at, used_at, sponsor_user_id FROM agent_passes WHERE pass_hash = ? AND workspace_id = ?`,
		HashToken(pass), wsID).Scan(&expires, &used, &sponsor)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (used.Valid || expires <= s.nowMillis())) {
		return fmt.Errorf("%w: this pass is not valid for this workspace (used, expired or unknown); ask your human for a new invite", ErrForbidden)
	}
	if err != nil {
		return err
	}
	if member, err := s.IsMember(ctx, wsID, sponsor); err != nil || !member {
		if err != nil {
			return err
		}
		return fmt.Errorf("%w: the member who made this pass is no longer in the workspace; ask a member for a new invite", ErrForbidden)
	}
	return nil
}
