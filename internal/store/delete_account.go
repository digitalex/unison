package store

import (
	"context"
	"fmt"
	"strings"
)

// DeletedName is what a deleted account's participants are called (#135).
const DeletedName = "DeletedUser"

// AccountDeletion is what DeleteAccount did.
type AccountDeletion struct {
	// Workspaces it deleted: those the account was the only member of.
	DeletedWorkspaces []string `json:"deleted_workspaces"`
	// Workspaces it left, whose content stays under DeletedName.
	LeftWorkspaces []string `json:"left_workspaces"`
}

// DeleteAccount deletes a user's account for good (#135; App Store
// guideline 5.1.1(v)). A workspace whose only member is the user goes with
// it. In shared workspaces the user's messages, items and comments stay
// for the others, but their participant becomes DeletedName and stops
// working, and so do the agents they invited. Their sign-ins, sessions,
// devices and pending codes are deleted. The users row is kept, emptied,
// only because shared history still refers to it.
func (s *Store) DeleteAccount(ctx context.Context, userID int64) (*AccountDeletion, []Event, error) {
	out := &AccountDeletion{DeletedWorkspaces: []string{}, LeftWorkspaces: []string{}}
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var n int
		if err := t.QueryRow(`SELECT COUNT(*) FROM users WHERE id = ?`, userID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("user %d: %w", userID, ErrNotFound)
		}
		// Every workspace the user is in: as a member, or through a
		// participant or an agent of theirs (a member who was removed).
		ws, err := queryStrings(t, `SELECT workspace_id FROM memberships WHERE user_id = ?
			UNION SELECT workspace_id FROM participants WHERE user_id = ? OR sponsor_user_id = ?`, userID, userID, userID)
		if err != nil {
			return err
		}
		for _, id := range ws {
			var others int
			if err := t.QueryRow(`SELECT COUNT(*) FROM memberships WHERE workspace_id = ? AND user_id <> ?`, id, userID).Scan(&others); err != nil {
				return err
			}
			if others == 0 {
				if err := deleteWorkspace(t, id); err != nil {
					return err
				}
				out.DeletedWorkspaces = append(out.DeletedWorkspaces, id)
				continue
			}
			if err := leaveForGood(t, log, id, userID); err != nil {
				return err
			}
			out.LeftWorkspaces = append(out.LeftWorkspaces, id)
		}
		for _, q := range []string{
			`DELETE FROM devices WHERE user_id = ?`,
			`DELETE FROM app_codes WHERE user_id = ?`,
			`DELETE FROM device_codes WHERE user_id = ?`,
			`DELETE FROM oauth_states WHERE link_user_id = ?`,
			`DELETE FROM sessions WHERE user_id = ?`,
			`DELETE FROM user_identities WHERE user_id = ?`,
			`DELETE FROM agent_passes WHERE sponsor_user_id = ? AND participant_id IS NULL`,
			`UPDATE workspace_invites SET revoked_at = ?, email = NULL WHERE created_by = ? AND revoked_at IS NULL`,
			`UPDATE users SET display_name = '', avatar_url = '' WHERE id = ?`,
		} {
			args := []any{userID}
			if strings.Contains(q, "revoked_at = ?") {
				args = []any{log.now, userID}
			}
			if _, err := t.Exec(q, args...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return out, evs, nil
}

// leaveForGood takes a deleted account out of a shared workspace: no
// longer a member, and its participant and agents renamed DeletedName
// (numbered when taken) and revoked, so only the content stays.
func leaveForGood(t *tx, log *eventLog, wsID string, userID int64) error {
	name, err := displayName(t, userID)
	if err != nil {
		return err
	}
	res, err := t.Exec(`DELETE FROM memberships WHERE workspace_id = ? AND user_id = ?`, wsID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		if err := log.add(wsID, "member.removed", 0, memberEvent{UserID: userID, DisplayName: name}); err != nil {
			return err
		}
	}
	ids, err := queryInts(t, `SELECT id FROM participants WHERE workspace_id = ? AND (user_id = ? OR sponsor_user_id = ?) ORDER BY id`,
		wsID, userID, userID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		p, err := scanParticipant(t.QueryRow(`SELECT `+participantCols+` FROM participants WHERE id = ?`, id))
		if err != nil {
			return err
		}
		newName, err := numberedName(t, wsID, DeletedName)
		if err != nil {
			return err
		}
		// A fresh random token, so an old copy of it can't sign in.
		if _, err := t.Exec(`UPDATE participants SET name = ?, emoji = '👤', status = '', token = ?,
			revoked_at = COALESCE(revoked_at, ?), revoke_reason = 'account deleted' WHERE id = ?`,
			newName, randomToken("deleted_", 32), log.now, id); err != nil {
			return err
		}
		p.Name, p.Emoji, p.Status, p.RevokeReason = newName, "👤", "", "account deleted"
		if p.RevokedAt == 0 {
			p.RevokedAt = log.now
		}
		if err := log.add(wsID, "participant.revoked", 0, p); err != nil {
			return err
		}
	}
	return nil
}

// numberedName is base, or "base2", "base3"… if participants already use it.
func numberedName(t *tx, wsID, base string) (string, error) {
	for i := 1; ; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s%d", base, i)
		}
		var n int
		if err := t.QueryRow(`SELECT COUNT(*) FROM participants WHERE workspace_id = ? AND lower(name) = lower(?)`, wsID, name).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return name, nil
		}
	}
}

func queryStrings(t *tx, q string, args ...any) ([]string, error) {
	rows, err := t.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func queryInts(t *tx, q string, args ...any) ([]int64, error) {
	rows, err := t.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
