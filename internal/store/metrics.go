package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Metric directions: whether a metric should go up or down.
const (
	DirectionIncrease = "increase"
	DirectionDecrease = "decrease"
)

// Metric is a quantity the workspace is trying to optimize. Adding one also
// adds a backlog item (ItemID) for the work of actually tracking it.
type Metric struct {
	ID          int64    `json:"id"`
	WorkspaceID string   `json:"workspace_id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Direction   string   `json:"direction"`
	Unit        string   `json:"unit"`
	Target      *float64 `json:"target"`
	ItemID      *int64   `json:"item_id"`
	CreatedBy   int64    `json:"created_by"`
	CreatedAt   int64    `json:"created_at"`
}

// NewMetric holds the fields for AddMetric.
type NewMetric struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Direction   string   `json:"direction"` // "" means increase.
	Unit        string   `json:"unit"`
	Target      *float64 `json:"target"`
}

// MetricPatch holds optional metric field updates.
type MetricPatch struct {
	Name        *string       `json:"name"`
	Description *string       `json:"description"`
	Direction   *string       `json:"direction"`
	Unit        *string       `json:"unit"`
	Target      OptionalFloat `json:"target"`
}

// OptionalFloat distinguishes an absent JSON field (Set false) from an
// explicit null (Set true, Value nil), so a patch can clear a value.
type OptionalFloat struct {
	Set   bool
	Value *float64
}

// UnmarshalJSON implements json.Unmarshaler.
func (o *OptionalFloat) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	o.Value = &f
	return nil
}

// FormatTarget renders a metric's target with its unit ("" if none).
func (m *Metric) FormatTarget() string {
	if m.Target == nil {
		return ""
	}
	s := strconv.FormatFloat(*m.Target, 'f', -1, 64)
	if m.Unit != "" {
		s += " " + m.Unit
	}
	return s
}

const metricCols = `id, workspace_id, name, description, direction, unit, target, item_id, created_by, created_at`

func scanMetric(row interface{ Scan(...any) error }) (*Metric, error) {
	var m Metric
	var target sql.NullFloat64
	var item sql.NullInt64
	if err := row.Scan(&m.ID, &m.WorkspaceID, &m.Name, &m.Description, &m.Direction, &m.Unit, &target, &item, &m.CreatedBy, &m.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if target.Valid {
		m.Target = &target.Float64
	}
	if item.Valid {
		m.ItemID = &item.Int64
	}
	return &m, nil
}

func getMetric(q interface {
	QueryRow(string, ...any) *sql.Row
}, wsID string, id int64) (*Metric, error) {
	m, err := scanMetric(q.QueryRow(`SELECT `+metricCols+` FROM metrics WHERE workspace_id = ? AND id = ?`, wsID, id))
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("metric %d: %w", id, ErrNotFound)
	}
	return m, err
}

func normalizeDirection(d string) (string, error) {
	switch d = strings.ToLower(strings.TrimSpace(d)); d {
	case "":
		return DirectionIncrease, nil
	case DirectionIncrease, DirectionDecrease:
		return d, nil
	}
	return "", fmt.Errorf("%w: direction must be %q or %q, not %q", ErrInvalid, DirectionIncrease, DirectionDecrease, d)
}

func metricNameConflict(err error, name string) error {
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: a metric named %q already exists in this workspace", ErrConflict, name)
	}
	return err
}

// trackingItemBody describes the backlog work created with a metric.
func trackingItemBody(m *Metric) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Make sure the metric %q is actually measured and recorded: decide how it is computed, "+
		"automate collecting it where possible, and record its values regularly so progress is visible.\n\n", m.Name)
	fmt.Fprintf(&b, "Goal: %s it", m.Direction)
	if t := m.FormatTarget(); t != "" {
		fmt.Fprintf(&b, " (target: %s)", t)
	} else if m.Unit != "" {
		fmt.Fprintf(&b, " (unit: %s)", m.Unit)
	}
	b.WriteString(".")
	if m.Description != "" {
		fmt.Fprintf(&b, "\n\n%s", m.Description)
	}
	return b.String()
}

// AddMetric adds a metric and, in the same transaction, a backlog item at
// the bottom of the queue for the work of tracking it. It records
// backlog.added and metric.added.
func (s *Store) AddMetric(ctx context.Context, actor *Participant, nm NewMetric) (*Metric, []Event, error) {
	name := strings.TrimSpace(nm.Name)
	if name == "" {
		return nil, nil, fmt.Errorf("%w: metric name is required", ErrInvalid)
	}
	direction, err := normalizeDirection(nm.Direction)
	if err != nil {
		return nil, nil, err
	}
	m := &Metric{
		WorkspaceID: actor.WorkspaceID, Name: name, Description: strings.TrimSpace(nm.Description),
		Direction: direction, Unit: strings.TrimSpace(nm.Unit), Target: nm.Target, CreatedBy: actor.ID,
	}
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		m.CreatedAt = log.now
		id, err := t.insert(`INSERT INTO metrics (workspace_id, name, description, direction, unit, target, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "id",
			m.WorkspaceID, m.Name, m.Description, m.Direction, m.Unit, m.Target, m.CreatedBy, m.CreatedAt)
		if err != nil {
			return metricNameConflict(err, name)
		}
		m.ID = id
		it, err := addItem(t, log, actor, "Track metric: "+m.Name, trackingItemBody(m), 0)
		if err != nil {
			return err
		}
		if _, err := t.Exec(`UPDATE metrics SET item_id = ? WHERE id = ?`, it.ID, m.ID); err != nil {
			return err
		}
		m.ItemID = &it.ID
		return log.add(m.WorkspaceID, "metric.added", actor.ID, m)
	})
	if err != nil {
		return nil, nil, err
	}
	return m, evs, nil
}

// ListMetrics returns a workspace's metrics, oldest first.
func (s *Store) ListMetrics(ctx context.Context, wsID string) ([]Metric, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+metricCols+` FROM metrics WHERE workspace_id = ? ORDER BY id`, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Metric{}
	for rows.Next() {
		m, err := scanMetric(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// GetMetric returns one metric.
func (s *Store) GetMetric(ctx context.Context, wsID string, id int64) (*Metric, error) {
	m, err := scanMetric(s.db.QueryRowContext(ctx, `SELECT `+metricCols+` FROM metrics WHERE workspace_id = ? AND id = ?`, wsID, id))
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("metric %d: %w", id, ErrNotFound)
	}
	return m, err
}

// EditMetric applies a patch to a metric. Its tracking item is unchanged.
func (s *Store) EditMetric(ctx context.Context, actor *Participant, id int64, p MetricPatch) (*Metric, []Event, error) {
	var m *Metric
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var err error
		if m, err = getMetric(t, actor.WorkspaceID, id); err != nil {
			return err
		}
		if p.Name != nil {
			if m.Name = strings.TrimSpace(*p.Name); m.Name == "" {
				return fmt.Errorf("%w: metric name cannot be empty", ErrInvalid)
			}
		}
		if p.Description != nil {
			m.Description = strings.TrimSpace(*p.Description)
		}
		if p.Direction != nil {
			if m.Direction, err = normalizeDirection(*p.Direction); err != nil {
				return err
			}
		}
		if p.Unit != nil {
			m.Unit = strings.TrimSpace(*p.Unit)
		}
		if p.Target.Set {
			m.Target = p.Target.Value
		}
		if _, err := t.Exec(`UPDATE metrics SET name = ?, description = ?, direction = ?, unit = ?, target = ? WHERE id = ?`,
			m.Name, m.Description, m.Direction, m.Unit, m.Target, m.ID); err != nil {
			return metricNameConflict(err, m.Name)
		}
		return log.add(m.WorkspaceID, "metric.updated", actor.ID, m)
	})
	if err != nil {
		return nil, nil, err
	}
	return m, evs, nil
}

// DeleteMetric removes a metric. Its tracking backlog item is kept.
func (s *Store) DeleteMetric(ctx context.Context, actor *Participant, id int64) ([]Event, error) {
	return s.tx(ctx, func(t *tx, log *eventLog) error {
		m, err := getMetric(t, actor.WorkspaceID, id)
		if err != nil {
			return err
		}
		if _, err := t.Exec(`DELETE FROM metrics WHERE id = ?`, id); err != nil {
			return err
		}
		return log.add(m.WorkspaceID, "metric.deleted", actor.ID, m)
	})
}

// unlinkMetrics clears the tracking-item link of metrics pointing at a
// backlog item that is being deleted, recording metric.updated for each.
func unlinkMetrics(t *tx, log *eventLog, actor *Participant, itemID int64) error {
	rows, err := t.Query(`SELECT `+metricCols+` FROM metrics WHERE workspace_id = ? AND item_id = ?`, actor.WorkspaceID, itemID)
	if err != nil {
		return err
	}
	var linked []*Metric
	for rows.Next() {
		m, err := scanMetric(rows)
		if err != nil {
			rows.Close()
			return err
		}
		linked = append(linked, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, m := range linked {
		if _, err := t.Exec(`UPDATE metrics SET item_id = NULL WHERE id = ?`, m.ID); err != nil {
			return err
		}
		m.ItemID = nil
		if err := log.add(m.WorkspaceID, "metric.updated", actor.ID, m); err != nil {
			return err
		}
	}
	return nil
}
