package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Schedule is a recurring task: a backlog item that comes due daily, or
// weekly on a weekday, at 00:00 server time (#111). Due runs become backlog
// items at the top of the backlog (#112).
type Schedule struct {
	ID          int64  `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	Cadence     string `json:"cadence"`           // "daily" or "weekly"
	Weekday     string `json:"weekday,omitempty"` // "mon" … "sun", for weekly
	CreatedBy   int64  `json:"created_by"`
	CreatedAt   int64  `json:"created_at"`
	NextDueAt   int64  `json:"next_due_at"`
}

// ScheduleInput holds the fields for adding a schedule.
type ScheduleInput struct {
	Title   string `json:"title"`
	Body    string `json:"body"`
	Cadence string `json:"cadence"`
	Weekday string `json:"weekday"`
}

// SchedulePatch holds optional schedule field updates. Changing the cadence
// or weekday moves the next run to the new cadence's next boundary.
type SchedulePatch struct {
	Title   *string `json:"title"`
	Body    *string `json:"body"`
	Cadence *string `json:"cadence"`
	Weekday *string `json:"weekday"`
}

const (
	CadenceDaily  = "daily"
	CadenceWeekly = "weekly"
)

var weekdays = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// checkCadence validates a cadence and weekday, returning the weekday in its
// canonical three-letter form ("" for daily).
func checkCadence(cadence, weekday string) (string, error) {
	weekday = strings.ToLower(strings.TrimSpace(weekday))
	switch cadence {
	case CadenceDaily:
		if weekday != "" {
			return "", fmt.Errorf("%w: a daily schedule has no weekday", ErrInvalid)
		}
		return "", nil
	case CadenceWeekly:
		if len(weekday) >= 3 {
			for _, d := range weekdays {
				if strings.HasPrefix(weekday, d) {
					return d, nil
				}
			}
		}
		return "", fmt.Errorf("%w: a weekly schedule needs a weekday (mon … sun), got %q", ErrInvalid, weekday)
	}
	return "", fmt.Errorf("%w: unknown cadence %q (want daily or weekly)", ErrInvalid, cadence)
}

// nextDue is the first 00:00 strictly after now (in now's location, the
// server's time zone) that falls on the cadence: any day, or the weekday.
func nextDue(now time.Time, cadence, weekday string) time.Time {
	y, m, d := now.Date()
	t := time.Date(y, m, d+1, 0, 0, 0, 0, now.Location())
	if cadence == CadenceWeekly {
		for weekdays[t.Weekday()] != weekday {
			t = t.AddDate(0, 0, 1)
		}
	}
	return t
}

const scheduleCols = `id, workspace_id, title, body, cadence, weekday, created_by, created_at, next_due_at`

func scanSchedules(rows *sql.Rows) ([]Schedule, error) {
	defer rows.Close()
	out := []Schedule{}
	for rows.Next() {
		var sc Schedule
		if err := rows.Scan(&sc.ID, &sc.WorkspaceID, &sc.Title, &sc.Body, &sc.Cadence, &sc.Weekday,
			&sc.CreatedBy, &sc.CreatedAt, &sc.NextDueAt); err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

func getSchedule(t *tx, wsID string, id int64) (*Schedule, error) {
	rows, err := t.Query(`SELECT `+scheduleCols+` FROM schedules WHERE workspace_id = ? AND id = ?`, wsID, id)
	if err != nil {
		return nil, err
	}
	list, err := scanSchedules(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("schedule %d: %w", id, ErrNotFound)
	}
	return &list[0], nil
}

// ListSchedules returns a workspace's schedules, soonest due first.
func (s *Store) ListSchedules(ctx context.Context, wsID string) ([]Schedule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+scheduleCols+` FROM schedules WHERE workspace_id = ? ORDER BY next_due_at, id`, wsID)
	if err != nil {
		return nil, err
	}
	return scanSchedules(rows)
}

// AddSchedule adds a recurring task to the actor's workspace and records
// schedule.added. Its first run is due at the cadence's next boundary.
func (s *Store) AddSchedule(ctx context.Context, actor *Participant, in ScheduleInput) (*Schedule, []Event, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, nil, fmt.Errorf("%w: schedule title is required", ErrInvalid)
	}
	weekday, err := checkCadence(in.Cadence, in.Weekday)
	if err != nil {
		return nil, nil, err
	}
	sc := &Schedule{WorkspaceID: actor.WorkspaceID, Title: title, Body: in.Body, Cadence: in.Cadence, Weekday: weekday, CreatedBy: actor.ID}
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		sc.CreatedAt = log.now
		sc.NextDueAt = nextDue(s.now(), sc.Cadence, sc.Weekday).UnixMilli()
		var err error
		if sc.ID, err = t.insert(`INSERT INTO schedules (workspace_id, title, body, cadence, weekday, created_by, created_at, next_due_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "id",
			sc.WorkspaceID, sc.Title, sc.Body, sc.Cadence, sc.Weekday, sc.CreatedBy, sc.CreatedAt, sc.NextDueAt); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "schedule.added", actor.ID, sc)
	})
	if err != nil {
		return nil, nil, err
	}
	return sc, evs, nil
}

// EditSchedule changes a schedule and records schedule.updated.
func (s *Store) EditSchedule(ctx context.Context, actor *Participant, id int64, p SchedulePatch) (*Schedule, []Event, error) {
	var sc *Schedule
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var err error
		if sc, err = getSchedule(t, actor.WorkspaceID, id); err != nil {
			return err
		}
		if p.Title != nil {
			if sc.Title = strings.TrimSpace(*p.Title); sc.Title == "" {
				return fmt.Errorf("%w: schedule title is required", ErrInvalid)
			}
		}
		if p.Body != nil {
			sc.Body = *p.Body
		}
		if p.Cadence != nil || p.Weekday != nil {
			cadence, weekday := sc.Cadence, sc.Weekday
			if p.Cadence != nil {
				cadence = *p.Cadence
				if cadence == CadenceDaily && p.Weekday == nil {
					weekday = ""
				}
			}
			if p.Weekday != nil {
				weekday = *p.Weekday
			}
			if weekday, err = checkCadence(cadence, weekday); err != nil {
				return err
			}
			if cadence != sc.Cadence || weekday != sc.Weekday {
				sc.Cadence, sc.Weekday = cadence, weekday
				sc.NextDueAt = nextDue(s.now(), cadence, weekday).UnixMilli()
			}
		}
		if _, err := t.Exec(`UPDATE schedules SET title = ?, body = ?, cadence = ?, weekday = ?, next_due_at = ? WHERE id = ?`,
			sc.Title, sc.Body, sc.Cadence, sc.Weekday, sc.NextDueAt, sc.ID); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "schedule.updated", actor.ID, sc)
	})
	if err != nil {
		return nil, nil, err
	}
	return sc, evs, nil
}

// RemoveSchedule deletes a schedule and records schedule.removed. Items it
// already created stay.
func (s *Store) RemoveSchedule(ctx context.Context, actor *Participant, id int64) ([]Event, error) {
	return s.tx(ctx, func(t *tx, log *eventLog) error {
		sc, err := getSchedule(t, actor.WorkspaceID, id)
		if err != nil {
			return err
		}
		if _, err := t.Exec(`DELETE FROM schedules WHERE id = ?`, sc.ID); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "schedule.removed", actor.ID, sc)
	})
}
