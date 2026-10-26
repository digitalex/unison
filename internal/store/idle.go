package store

import (
	"context"
	"fmt"
	"time"
)

// Idle agents are logged out: an agent (a participant that joined with a
// pass) with no activity for AgentIdleTimeout loses its credential, as if
// its sponsor had revoked it, with RevokeReason "idle". What counts as
// activity is up to the caller of MarkActive (the server: any request
// except listening for events). Humans are never logged out this way.
const (
	AgentIdleTimeout = 6 * time.Hour
	// activityResolution throttles MarkActive's writes: at most one per
	// agent per minute.
	activityResolution = time.Minute
	revokedIdle        = "idle"
)

func idleError(p *Participant) error {
	return fmt.Errorf("%w: %w: logged out after %d hours without any activity; ask %s for a new pass",
		ErrRevoked, ErrIdle, int(AgentIdleTimeout.Hours()), p.Sponsor.DisplayName)
}

// checkIdle enforces AgentIdleTimeout for an agent being authenticated: it
// logs out an agent idle too long (recording participant.logged_out) and
// returns the error. An agent not tracked yet (LastActiveAt 0, from before
// tracking) starts its clock now instead.
func (s *Store) checkIdle(ctx context.Context, p *Participant) error {
	if p.Sponsor == nil {
		return nil
	}
	now := s.nowMillis()
	if p.LastActiveAt == 0 {
		p.LastActiveAt = now
		_, err := s.db.ExecContext(ctx, `UPDATE participants SET last_active_at = ? WHERE id = ? AND last_active_at IS NULL`, now, p.ID)
		return err
	}
	if now-p.LastActiveAt <= AgentIdleTimeout.Milliseconds() {
		return nil
	}
	_, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		res, err := t.Exec(`UPDATE participants SET revoked_at = ?, revoke_reason = ? WHERE id = ? AND revoked_at IS NULL`, log.now, revokedIdle, p.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil // Someone else got there first.
		}
		p.RevokedAt, p.RevokeReason = log.now, revokedIdle
		return log.add(p.WorkspaceID, "participant.logged_out", 0, p)
	})
	if err != nil {
		return err
	}
	return idleError(p)
}

// MarkActive records activity by an agent, keeping it logged in. Writes are
// throttled to one a minute; humans aren't tracked.
func (s *Store) MarkActive(ctx context.Context, p *Participant) error {
	now := s.nowMillis()
	if p.Sponsor == nil || now-p.LastActiveAt < activityResolution.Milliseconds() {
		return nil
	}
	p.LastActiveAt = now
	_, err := s.db.ExecContext(ctx, `UPDATE participants SET last_active_at = ? WHERE id = ?`, now, p.ID)
	return err
}
