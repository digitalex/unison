package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Agent passes: a member mints a single-use pass (valid AgentPassTTL) and
// hands it to an agent in the invite prompt; the agent exchanges it when
// joining (JoinWithPass) for a participant that acts for that member (its
// sponsor), whose token is the agent's credential: it lapses after
// AgentCredentialTTL without use, and stops working if the sponsor leaves.
const (
	AgentPassTTL       = 15 * time.Minute
	AgentCredentialTTL = 90 * 24 * time.Hour
	// AgentPassPrefix marks pass strings ("up_…").
	AgentPassPrefix = "up_"
)

// MintPass creates a pass for an agent to join wsID on behalf of userID,
// who must be a member. It returns the pass (only its hash is stored) and
// its expiry. Minting records no event: a pass is a secret, not workspace
// state.
func (s *Store) MintPass(ctx context.Context, wsID string, userID int64) (string, int64, error) {
	pass := randomToken(AgentPassPrefix, 24)
	var expires int64
	_, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		if _, err := scanWorkspace(t.QueryRow(`SELECT `+workspaceCols+` FROM workspaces WHERE id = ?`, wsID)); err != nil {
			return err
		}
		if member, err := isMember(t, wsID, userID); err != nil || !member {
			if err != nil {
				return err
			}
			return fmt.Errorf("%w: only members can invite agents (join this workspace while signed in: `unison join <url> --account`)", ErrForbidden)
		}
		// Expired passes are useless: prune them as we go.
		if _, err := t.Exec(`DELETE FROM agent_passes WHERE expires_at <= ? AND used_at IS NULL`, log.now-time.Hour.Milliseconds()); err != nil {
			return err
		}
		expires = log.now + AgentPassTTL.Milliseconds()
		_, err := t.Exec(`INSERT INTO agent_passes (pass_hash, workspace_id, sponsor_user_id, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
			HashToken(pass), wsID, userID, log.now, expires)
		return err
	})
	if err != nil {
		return "", 0, err
	}
	return pass, expires, nil
}

func isMember(t *tx, wsID string, userID int64) (bool, error) {
	var n int
	err := t.QueryRow(`SELECT COUNT(*) FROM memberships WHERE workspace_id = ? AND user_id = ?`, wsID, userID).Scan(&n)
	return n > 0, err
}

// JoinWithPass exchanges a pass for a new agent participant in wsID acting
// for the pass's sponsor. The pass is consumed in the same transaction, so
// it works exactly once. Failures are ErrForbidden with a reason: unknown,
// expired, already used, for another workspace, or sponsor no longer a
// member.
func (s *Store) JoinWithPass(ctx context.Context, wsID, pass, name, emoji string) (*Participant, string, []Event, error) {
	var p *Participant
	var token string
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var id, sponsor, expires int64
		var passWS string
		var used sql.NullInt64
		err := t.QueryRow(`SELECT id, workspace_id, sponsor_user_id, expires_at, used_at FROM agent_passes WHERE pass_hash = ?`, HashToken(pass)).
			Scan(&id, &passWS, &sponsor, &expires, &used)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("%w: unknown pass; ask your human for a new invite", ErrForbidden)
		case err != nil:
			return err
		case used.Valid:
			return fmt.Errorf("%w: this pass was already used (passes work once); ask your human for a new invite", ErrForbidden)
		case expires <= log.now:
			return fmt.Errorf("%w: this pass expired (passes last %d minutes); ask your human for a new invite", ErrForbidden, int(AgentPassTTL.Minutes()))
		case passWS != wsID:
			return fmt.Errorf("%w: this pass is for another workspace (%s)", ErrForbidden, passWS)
		}
		if member, err := isMember(t, wsID, sponsor); err != nil || !member {
			if err != nil {
				return err
			}
			return fmt.Errorf("%w: the member who made this pass is no longer in the workspace; ask a member for a new invite", ErrForbidden)
		}
		p, token, err = insertParticipant(t, log, wsID, name, emoji, participantLink{
			SponsorUserID: sponsor, TokenExpiresAt: log.now + AgentCredentialTTL.Milliseconds(),
		})
		if err != nil {
			return err
		}
		res, err := t.Exec(`UPDATE agent_passes SET used_at = ?, participant_id = ? WHERE id = ? AND used_at IS NULL`, log.now, p.ID, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("%w: this pass was already used (passes work once)", ErrForbidden)
		}
		return nil
	})
	if err != nil {
		return nil, "", nil, err
	}
	return p, token, evs, nil
}

// Agent is one of a member's agents, with the workspace it works in.
type Agent struct {
	Participant
	WorkspaceName string `json:"workspace_name"`
}

// ListMyAgents returns the agents userID sponsors across all workspaces
// (revoked ones included, marked), most recently seen first.
func (s *Store) ListMyAgents(ctx context.Context, userID int64) ([]Agent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+participantCols+`,
		(SELECT name FROM workspaces w WHERE w.id = participants.workspace_id)
		FROM participants WHERE sponsor_user_id = ? ORDER BY last_seen_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Agent{}
	for rows.Next() {
		var name string
		p, err := scanParticipant(rows, &name)
		if err != nil {
			return nil, err
		}
		out = append(out, Agent{Participant: *p, WorkspaceName: name})
	}
	return out, rows.Err()
}

// RevokeAgent stops an agent's credential for good: Authenticate then
// returns ErrRevoked. Only the agent's sponsor may revoke it (agents act
// for their sponsor; other members can remove the sponsor, which stops all
// of the sponsor's agents). Revoking twice is a no-op. Records
// participant.revoked in the agent's workspace.
func (s *Store) RevokeAgent(ctx context.Context, userID, participantID int64) (*Participant, []Event, error) {
	var p *Participant
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var err error
		p, err = scanParticipant(t.QueryRow(`SELECT `+participantCols+` FROM participants WHERE id = ?`, participantID))
		if err != nil {
			return fmt.Errorf("participant %d: %w", participantID, err)
		}
		if p.Sponsor == nil {
			return fmt.Errorf("%w: participant %d is not an agent (it did not join with a pass)", ErrInvalid, participantID)
		}
		if p.Sponsor.UserID != userID {
			return fmt.Errorf("%w: only %s, who invited %s, can revoke it", ErrForbidden, p.Sponsor.DisplayName, p.Name)
		}
		if p.RevokedAt != 0 {
			return nil
		}
		if _, err := t.Exec(`UPDATE participants SET revoked_at = ? WHERE id = ?`, log.now, p.ID); err != nil {
			return err
		}
		p.RevokedAt = log.now
		var actor int64
		if me, _, err := usersParticipant(t, p.WorkspaceID, userID); err == nil {
			actor = me.ID
		}
		return log.add(p.WorkspaceID, "participant.revoked", actor, p)
	})
	if err != nil {
		return nil, nil, err
	}
	return p, evs, nil
}
