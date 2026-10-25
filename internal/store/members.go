package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Membership: the accounts that belong to a workspace. Every member is equal
// (no roles). A member's per-workspace name and emoji are those of their
// participants, which they link by joining while signed in or by claiming.
// Nothing is enforced yet: participants without accounts work as before.

// Member is an account in a workspace with its linked participants, most
// recently active first (the first is the one shown for the person).
type Member struct {
	UserID       int64         `json:"user_id"`
	DisplayName  string        `json:"display_name"`
	AvatarURL    string        `json:"avatar_url"`
	JoinedAt     int64         `json:"joined_at"`
	Participants []Participant `json:"participants"`
}

// Members lists a workspace's members, and the participants not linked to
// any current member (agents, people who never signed in, and participants
// of removed members, kept for history).
type Members struct {
	Members  []Member      `json:"members"`
	Unlinked []Participant `json:"unlinked"`
}

// memberEvent is the data of member.added and member.removed.
type memberEvent struct {
	UserID      int64  `json:"user_id"`
	DisplayName string `json:"display_name"`
}

func displayName(t *tx, userID int64) (string, error) {
	var name string
	err := t.QueryRow(`SELECT display_name FROM users WHERE id = ?`, userID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("user %d: %w", userID, ErrNotFound)
	}
	return name, err
}

// ensureMembership makes userID a member of wsID (recording member.added
// the first time).
func ensureMembership(t *tx, log *eventLog, wsID string, userID, actor int64) error {
	var n int
	if err := t.QueryRow(`SELECT COUNT(*) FROM memberships WHERE workspace_id = ? AND user_id = ?`, wsID, userID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	name, err := displayName(t, userID)
	if err != nil {
		return err
	}
	if _, err := t.Exec(`INSERT INTO memberships (workspace_id, user_id, created_at) VALUES (?, ?, ?)`, wsID, userID, log.now); err != nil {
		return err
	}
	return log.add(wsID, "member.added", actor, memberEvent{UserID: userID, DisplayName: name})
}

// usersParticipant returns the user's most recently active participant in
// the workspace, with its token, or ErrNotFound.
func usersParticipant(t *tx, wsID string, userID int64) (*Participant, string, error) {
	var token string
	var p Participant
	err := t.QueryRow(`SELECT id, name, emoji, status, created_at, last_seen_at, token FROM participants
		WHERE workspace_id = ? AND user_id = ? ORDER BY last_seen_at DESC, id DESC LIMIT 1`, wsID, userID).
		Scan(&p.ID, &p.Name, &p.Emoji, &p.Status, &p.CreatedAt, &p.LastSeenAt, &token)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	p.WorkspaceID, p.UserID = wsID, userID
	return &p, token, nil
}

// JoinAsUser joins a signed-in user. If the user already has a participant
// in the workspace (the most recently active one), it is returned with its
// existing token instead of creating a duplicate (existing = true);
// otherwise a new participant is created linked to the user. Either way the
// user is (or becomes) a member.
func (s *Store) JoinAsUser(ctx context.Context, wsID string, userID int64, name, emoji string) (p *Participant, token string, existing bool, evs []Event, err error) {
	evs, err = s.tx(ctx, func(t *tx, log *eventLog) error {
		p, token, err = usersParticipant(t, wsID, userID)
		existing = err == nil
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if !existing {
			if p, token, err = insertParticipant(t, log, wsID, name, emoji, participantLink{UserID: userID}); err != nil {
				return err
			}
		}
		return ensureMembership(t, log, wsID, userID, p.ID)
	})
	if err != nil {
		return nil, "", false, nil, err
	}
	return p, token, existing, evs, nil
}

// Claim links the participant holding participantToken (proof of
// possession) to the user and makes the user a member. Claiming several
// participants into one user is fine; each keeps its own name in history.
// Claiming a participant already linked to the user is a no-op; one linked
// to someone else is ErrConflict.
func (s *Store) Claim(ctx context.Context, wsID string, userID int64, participantToken string) (*Participant, []Event, error) {
	var p *Participant
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var err error
		p, err = scanParticipant(t.QueryRow(`SELECT `+participantCols+` FROM participants WHERE token = ?`, participantToken))
		if participantToken == "" || errors.Is(err, ErrNotFound) || (err == nil && p.WorkspaceID != wsID) {
			return fmt.Errorf("%w: that participant token is not valid in this workspace (run claim where you joined)", ErrForbidden)
		}
		if err != nil {
			return err
		}
		switch p.UserID {
		case userID:
			return ensureMembership(t, log, wsID, userID, p.ID)
		case 0:
		default:
			return fmt.Errorf("%w: %s is already linked to another account", ErrConflict, p.Name)
		}
		name, err := displayName(t, userID)
		if err != nil {
			return err
		}
		if _, err := t.Exec(`UPDATE participants SET user_id = ? WHERE id = ?`, userID, p.ID); err != nil {
			return err
		}
		p.UserID = userID
		if err := log.add(wsID, "participant.claimed", p.ID, map[string]any{"participant": p, "user_id": userID, "display_name": name}); err != nil {
			return err
		}
		return ensureMembership(t, log, wsID, userID, p.ID)
	})
	if err != nil {
		return nil, nil, err
	}
	return p, evs, nil
}

// ListMembers returns the workspace's members (oldest first) and the
// participants not linked to a current member.
func (s *Store) ListMembers(ctx context.Context, wsID string) (*Members, error) {
	if _, err := s.GetWorkspace(ctx, wsID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT u.id, u.display_name, u.avatar_url, m.created_at FROM memberships m
		JOIN users u ON u.id = m.user_id WHERE m.workspace_id = ? ORDER BY m.created_at, m.id`, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &Members{Members: []Member{}, Unlinked: []Participant{}}
	index := map[int64]int{}
	for rows.Next() {
		m := Member{Participants: []Participant{}}
		if err := rows.Scan(&m.UserID, &m.DisplayName, &m.AvatarURL, &m.JoinedAt); err != nil {
			return nil, err
		}
		index[m.UserID] = len(out.Members)
		out.Members = append(out.Members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	participants, err := s.ListParticipants(ctx, wsID)
	if err != nil {
		return nil, err
	}
	for _, p := range participants {
		if i, ok := index[p.UserID]; ok && p.UserID != 0 {
			out.Members[i].Participants = append(out.Members[i].Participants, p)
		} else {
			out.Unlinked = append(out.Unlinked, p)
		}
	}
	return out, nil
}

// IsMember reports whether the user is a member of the workspace.
func (s *Store) IsMember(ctx context.Context, wsID string, userID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memberships WHERE workspace_id = ? AND user_id = ?`, wsID, userID).Scan(&n)
	return n > 0, err
}

// RemoveMember removes userID from the workspace's members; any member may
// remove any member (including themselves). Their participants stay, still
// linked, so history keeps its authors.
func (s *Store) RemoveMember(ctx context.Context, wsID string, byUserID, userID int64) ([]Event, error) {
	return s.tx(ctx, func(t *tx, log *eventLog) error {
		var n int
		if err := t.QueryRow(`SELECT COUNT(*) FROM memberships WHERE workspace_id = ? AND user_id = ?`, wsID, byUserID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("%w: only members can remove members (join or claim a participant first)", ErrForbidden)
		}
		name, err := displayName(t, userID)
		if err != nil {
			return err
		}
		res, err := t.Exec(`DELETE FROM memberships WHERE workspace_id = ? AND user_id = ?`, wsID, userID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("user %d is not a member: %w", userID, ErrNotFound)
		}
		var actor int64
		if p, _, err := usersParticipant(t, wsID, byUserID); err == nil {
			actor = p.ID
		}
		return log.add(wsID, "member.removed", actor, memberEvent{UserID: userID, DisplayName: name})
	})
}
