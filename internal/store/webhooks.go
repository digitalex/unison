package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxPayload is how much of a webhook delivery's body is stored; longer
// bodies are cut at a UTF-8 boundary and flagged Truncated.
const MaxPayload = 256 << 10

// Webhook is an inbound webhook: requests to /hooks/{token} are recorded as
// deliveries and wake agents with the webhook's plain-text Instructions.
// Token and SigningSecret are never serialized; they are returned only by
// the dedicated authenticated endpoints.
type Webhook struct {
	ID               int64  `json:"id"`
	WorkspaceID      string `json:"workspace_id"`
	Name             string `json:"name"`
	Instructions     string `json:"instructions"`
	Enabled          bool   `json:"enabled"`
	HasSigningSecret bool   `json:"has_signing_secret"`
	CreatedBy        int64  `json:"created_by"`
	CreatedAt        int64  `json:"created_at"`
	DeliveryCount    int    `json:"delivery_count"`
	LastDeliveryAt   *int64 `json:"last_delivery_at"`

	Token         string `json:"-"`
	SigningSecret string `json:"-"`
}

// NewWebhook holds the fields for AddWebhook.
type NewWebhook struct {
	Name          string `json:"name"`
	Instructions  string `json:"instructions"`
	SigningSecret string `json:"signing_secret"`
}

// WebhookPatch holds optional webhook updates. SigningSecret "" removes it.
type WebhookPatch struct {
	Name          *string `json:"name"`
	Instructions  *string `json:"instructions"`
	Enabled       *bool   `json:"enabled"`
	SigningSecret *string `json:"signing_secret"`
}

// Delivery is one request received by a webhook. Payload and Headers are
// only filled in by GetDelivery.
type Delivery struct {
	ID          int64             `json:"id"`
	WebhookID   int64             `json:"webhook_id"`
	WorkspaceID string            `json:"workspace_id"`
	ReceivedAt  int64             `json:"received_at"`
	Source      string            `json:"source"`
	Event       string            `json:"event"`
	Summary     string            `json:"summary"`
	Size        int               `json:"size"`
	Truncated   bool              `json:"truncated"`
	Headers     map[string]string `json:"headers,omitempty"`
	Payload     *string           `json:"payload,omitempty"`
}

// IncomingDelivery is what the server parsed from a webhook request.
type IncomingDelivery struct {
	Source  string
	Event   string
	Summary string
	Headers map[string]string
	Body    []byte
}

// DeliveryEvent is the data of webhook.received: everything an agent needs
// to act, including the webhook's instructions.
type DeliveryEvent struct {
	WebhookID    int64  `json:"webhook_id"`
	WebhookName  string `json:"webhook_name"`
	Instructions string `json:"instructions"`
	DeliveryID   int64  `json:"delivery_id"`
	Source       string `json:"source"`
	Event        string `json:"event"`
	Summary      string `json:"summary"`
}

func newWebhookToken() string {
	b := make([]byte, 24) // 192 bits.
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "wh_" + base64.RawURLEncoding.EncodeToString(b)
}

const webhookCols = `id, workspace_id, name, instructions, enabled, signing_secret, token, created_by, created_at,
	(SELECT COUNT(*) FROM webhook_deliveries d WHERE d.webhook_id = webhooks.id),
	(SELECT MAX(received_at) FROM webhook_deliveries d WHERE d.webhook_id = webhooks.id)`

func scanWebhook(row interface{ Scan(...any) error }) (*Webhook, error) {
	var h Webhook
	var last sql.NullInt64
	if err := row.Scan(&h.ID, &h.WorkspaceID, &h.Name, &h.Instructions, &h.Enabled, &h.SigningSecret, &h.Token,
		&h.CreatedBy, &h.CreatedAt, &h.DeliveryCount, &last); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	h.HasSigningSecret = h.SigningSecret != ""
	if last.Valid {
		h.LastDeliveryAt = &last.Int64
	}
	return &h, nil
}

func getWebhook(t *tx, wsID string, id int64) (*Webhook, error) {
	h, err := scanWebhook(t.QueryRow(`SELECT `+webhookCols+` FROM webhooks WHERE workspace_id = ? AND id = ?`, wsID, id))
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("webhook %d: %w", id, ErrNotFound)
	}
	return h, err
}

// AddWebhook creates a webhook with a fresh token and records webhook.added.
// The returned Webhook carries the Token for the creator.
func (s *Store) AddWebhook(ctx context.Context, actor *Participant, nw NewWebhook) (*Webhook, []Event, error) {
	name := strings.TrimSpace(nw.Name)
	if name == "" {
		return nil, nil, fmt.Errorf("%w: webhook name is required", ErrInvalid)
	}
	var h *Webhook
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		id, err := t.insert(`INSERT INTO webhooks (workspace_id, name, instructions, token, signing_secret, enabled, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "id",
			actor.WorkspaceID, name, strings.TrimSpace(nw.Instructions), newWebhookToken(), strings.TrimSpace(nw.SigningSecret), true, actor.ID, log.now)
		if err != nil {
			return err
		}
		if h, err = getWebhook(t, actor.WorkspaceID, id); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "webhook.added", actor.ID, h)
	})
	if err != nil {
		return nil, nil, err
	}
	return h, evs, nil
}

// ListWebhooks returns a workspace's webhooks (tokens included in the
// struct but never serialized).
func (s *Store) ListWebhooks(ctx context.Context, wsID string) ([]Webhook, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+webhookCols+` FROM webhooks WHERE workspace_id = ? ORDER BY id`, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Webhook{}
	for rows.Next() {
		h, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *h)
	}
	return out, rows.Err()
}

// GetWebhook returns one webhook.
func (s *Store) GetWebhook(ctx context.Context, wsID string, id int64) (*Webhook, error) {
	h, err := scanWebhook(s.db.QueryRowContext(ctx, `SELECT `+webhookCols+` FROM webhooks WHERE workspace_id = ? AND id = ?`, wsID, id))
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("webhook %d: %w", id, ErrNotFound)
	}
	return h, err
}

// WebhookByToken finds the enabled webhook a receive URL belongs to.
func (s *Store) WebhookByToken(ctx context.Context, token string) (*Webhook, error) {
	if !strings.HasPrefix(token, "wh_") {
		return nil, ErrNotFound
	}
	h, err := scanWebhook(s.db.QueryRowContext(ctx, `SELECT `+webhookCols+` FROM webhooks WHERE token = ?`, token))
	if err != nil {
		return nil, err
	}
	if !h.Enabled {
		return nil, ErrNotFound
	}
	return h, nil
}

// EditWebhook updates a webhook and records webhook.updated.
func (s *Store) EditWebhook(ctx context.Context, actor *Participant, id int64, p WebhookPatch) (*Webhook, []Event, error) {
	return s.mutateWebhook(ctx, actor, id, func(t *tx, h *Webhook) error {
		if p.Name != nil {
			if h.Name = strings.TrimSpace(*p.Name); h.Name == "" {
				return fmt.Errorf("%w: webhook name cannot be empty", ErrInvalid)
			}
		}
		if p.Instructions != nil {
			h.Instructions = strings.TrimSpace(*p.Instructions)
		}
		if p.Enabled != nil {
			h.Enabled = *p.Enabled
		}
		if p.SigningSecret != nil {
			h.SigningSecret = strings.TrimSpace(*p.SigningSecret)
		}
		_, err := t.Exec(`UPDATE webhooks SET name = ?, instructions = ?, enabled = ?, signing_secret = ? WHERE id = ?`,
			h.Name, h.Instructions, h.Enabled, h.SigningSecret, h.ID)
		return err
	})
}

// RotateWebhook issues a new token (the old receive URL stops working) and
// records webhook.updated. The returned Webhook carries the new Token.
func (s *Store) RotateWebhook(ctx context.Context, actor *Participant, id int64) (*Webhook, []Event, error) {
	return s.mutateWebhook(ctx, actor, id, func(t *tx, h *Webhook) error {
		_, err := t.Exec(`UPDATE webhooks SET token = ? WHERE id = ?`, newWebhookToken(), h.ID)
		return err
	})
}

func (s *Store) mutateWebhook(ctx context.Context, actor *Participant, id int64, fn func(*tx, *Webhook) error) (*Webhook, []Event, error) {
	var h *Webhook
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var err error
		if h, err = getWebhook(t, actor.WorkspaceID, id); err != nil {
			return err
		}
		if err := fn(t, h); err != nil {
			return err
		}
		if h, err = getWebhook(t, actor.WorkspaceID, id); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "webhook.updated", actor.ID, h)
	})
	if err != nil {
		return nil, nil, err
	}
	return h, evs, nil
}

// DeleteWebhook deletes a webhook and its deliveries; records webhook.deleted.
func (s *Store) DeleteWebhook(ctx context.Context, actor *Participant, id int64) ([]Event, error) {
	return s.tx(ctx, func(t *tx, log *eventLog) error {
		h, err := getWebhook(t, actor.WorkspaceID, id)
		if err != nil {
			return err
		}
		if _, err := t.Exec(`DELETE FROM webhook_deliveries WHERE webhook_id = ?`, id); err != nil {
			return err
		}
		if _, err := t.Exec(`DELETE FROM webhooks WHERE id = ?`, id); err != nil {
			return err
		}
		return log.add(actor.WorkspaceID, "webhook.deleted", actor.ID, h)
	})
}

// storablePayload caps body at MaxPayload bytes (on a rune boundary) and
// makes it valid text for both databases (Postgres TEXT rejects NUL and
// invalid UTF-8).
func storablePayload(body []byte) (string, bool) {
	truncated := len(body) > MaxPayload
	if truncated {
		cut := MaxPayload // Back up to the start of a rune cut in half.
		for cut > MaxPayload-utf8.UTFMax && !utf8.RuneStart(body[cut]) {
			cut--
		}
		body = body[:cut]
	}
	s := strings.ToValidUTF8(string(body), "\uFFFD")
	return strings.ReplaceAll(s, "\x00", "\uFFFD"), truncated
}

// ReceiveDelivery records a delivery for h and a webhook.received event
// carrying the webhook's instructions.
func (s *Store) ReceiveDelivery(ctx context.Context, h *Webhook, in IncomingDelivery) (*Delivery, []Event, error) {
	payload, truncated := storablePayload(in.Body)
	headers, err := json.Marshal(in.Headers)
	if err != nil {
		return nil, nil, err
	}
	d := &Delivery{WebhookID: h.ID, WorkspaceID: h.WorkspaceID, Source: in.Source, Event: in.Event,
		Summary: oneLine(in.Summary, 300), Size: len(in.Body), Truncated: truncated}
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		d.ReceivedAt = log.now
		d.ID, err = t.insert(`INSERT INTO webhook_deliveries (webhook_id, workspace_id, received_at, source, event, summary, headers, payload, size, truncated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "id",
			d.WebhookID, d.WorkspaceID, d.ReceivedAt, d.Source, d.Event, d.Summary, string(headers), payload, d.Size, d.Truncated)
		if err != nil {
			return err
		}
		return log.add(h.WorkspaceID, "webhook.received", 0, DeliveryEvent{
			WebhookID: h.ID, WebhookName: h.Name, Instructions: h.Instructions,
			DeliveryID: d.ID, Source: d.Source, Event: d.Event, Summary: d.Summary,
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return d, evs, nil
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > max {
		s = string([]rune(s)[:max-1]) + "…"
	}
	return s
}

const deliveryCols = `id, webhook_id, workspace_id, received_at, source, event, summary, size, truncated`

func scanDelivery(row interface{ Scan(...any) error }, extra ...any) (*Delivery, error) {
	var d Delivery
	dest := append([]any{&d.ID, &d.WebhookID, &d.WorkspaceID, &d.ReceivedAt, &d.Source, &d.Event, &d.Summary, &d.Size, &d.Truncated}, extra...)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &d, nil
}

// ListDeliveries returns up to limit recent deliveries of a webhook, newest
// first, without payloads.
func (s *Store) ListDeliveries(ctx context.Context, wsID string, webhookID int64, limit int) ([]Delivery, error) {
	if _, err := s.GetWebhook(ctx, wsID, webhookID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+deliveryCols+` FROM webhook_deliveries WHERE workspace_id = ? AND webhook_id = ? ORDER BY id DESC LIMIT ?`,
		wsID, webhookID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Delivery{}
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// GetDelivery returns one delivery with its headers and payload.
func (s *Store) GetDelivery(ctx context.Context, wsID string, id int64) (*Delivery, error) {
	var headers, payload string
	d, err := scanDelivery(s.db.QueryRowContext(ctx, `SELECT `+deliveryCols+`, headers, payload FROM webhook_deliveries WHERE workspace_id = ? AND id = ?`, wsID, id),
		&headers, &payload)
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("delivery %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	d.Payload = &payload
	if err := json.Unmarshal([]byte(headers), &d.Headers); err != nil {
		return nil, err
	}
	return d, nil
}
