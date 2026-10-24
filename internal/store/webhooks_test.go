package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestWebhookLifecycle(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()
	h, evs, err := s.AddWebhook(ctx, a, NewWebhook{Name: " GitHub ", Instructions: "Review new PRs.", SigningSecret: "s3cret"})
	if err != nil {
		t.Fatal(err)
	}
	if h.Name != "GitHub" || !h.Enabled || !h.HasSigningSecret || !strings.HasPrefix(h.Token, "wh_") || len(h.Token) < 30 {
		t.Fatalf("webhook = %+v", h)
	}
	if len(evs) != 1 || evs[0].Type != "webhook.added" || strings.Contains(string(evs[0].Data), h.Token) || strings.Contains(string(evs[0].Data), "s3cret") {
		t.Errorf("webhook.added leaks secrets: %s", evs[0].Data)
	}
	if _, _, err := s.AddWebhook(ctx, a, NewWebhook{Name: " "}); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty name err = %v", err)
	}
	if got, err := s.WebhookByToken(ctx, h.Token); err != nil || got.ID != h.ID || got.SigningSecret != "s3cret" {
		t.Errorf("WebhookByToken = %+v, %v", got, err)
	}

	// Rotating invalidates the old token.
	old := h.Token
	rotated, _, err := s.RotateWebhook(ctx, a, h.ID)
	if err != nil || rotated.Token == old {
		t.Fatalf("rotate = %+v, %v", rotated, err)
	}
	if _, err := s.WebhookByToken(ctx, old); !errors.Is(err, ErrNotFound) {
		t.Errorf("old token err = %v, want ErrNotFound", err)
	}

	// Disabled webhooks don't receive.
	off := false
	if _, _, err := s.EditWebhook(ctx, a, h.ID, WebhookPatch{Enabled: &off, SigningSecret: ptr("")}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WebhookByToken(ctx, rotated.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("disabled webhook err = %v, want ErrNotFound", err)
	}
	got, _ := s.GetWebhook(ctx, a.WorkspaceID, h.ID)
	if got.Enabled || got.HasSigningSecret {
		t.Errorf("after edit = %+v", got)
	}
	data, _ := json.Marshal(got)
	if strings.Contains(string(data), rotated.Token) || strings.Contains(string(data), "token") {
		t.Errorf("webhook JSON leaks the token: %s", data)
	}
}

func TestDeliveries(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()
	h, _, _ := s.AddWebhook(ctx, a, NewWebhook{Name: "CI", Instructions: "Fix failing builds."})
	d, evs, err := s.ReceiveDelivery(ctx, h, IncomingDelivery{
		Source: "github", Event: "workflow_run", Summary: "github: workflow_run \"CI\" failure\non main",
		Headers: map[string]string{"X-GitHub-Event": "workflow_run"}, Body: []byte(`{"ok":false}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Summary != `github: workflow_run "CI" failure on main` || len(evs) != 1 || evs[0].Type != "webhook.received" {
		t.Fatalf("delivery = %+v, events = %+v", d, evs)
	}
	var ev DeliveryEvent
	json.Unmarshal(evs[0].Data, &ev)
	if ev.Instructions != "Fix failing builds." || ev.DeliveryID != d.ID || ev.WebhookName != "CI" {
		t.Errorf("webhook.received data = %+v", ev)
	}

	// Oversized, non-UTF-8 and NUL-containing bodies are stored safely.
	big := []byte(strings.Repeat("é", MaxPayload)) // 2 bytes per rune: cut mid-rune.
	big = append(big, 0x00, 0xff)
	d2, _, err := s.ReceiveDelivery(ctx, h, IncomingDelivery{Source: "generic", Summary: "generic: text/plain", Headers: map[string]string{}, Body: big})
	if err != nil {
		t.Fatal(err)
	}
	full, err := s.GetDelivery(ctx, a.WorkspaceID, d2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !full.Truncated || full.Size != len(big) || len(*full.Payload) > MaxPayload || !utf8.ValidString(*full.Payload) {
		t.Errorf("truncated delivery: truncated=%v size=%d stored=%d valid=%v", full.Truncated, full.Size, len(*full.Payload), utf8.ValidString(*full.Payload))
	}
	weird, _, err := s.ReceiveDelivery(ctx, h, IncomingDelivery{Source: "generic", Summary: "generic: x", Headers: map[string]string{}, Body: []byte("a\x00b\xffc")})
	if err != nil {
		t.Fatalf("NUL/invalid UTF-8 body: %v", err)
	}
	if got, _ := s.GetDelivery(ctx, a.WorkspaceID, weird.ID); *got.Payload != "a\uFFFDb\uFFFDc" || got.Truncated {
		t.Errorf("sanitized payload = %q", *got.Payload)
	}

	list, _ := s.ListDeliveries(ctx, a.WorkspaceID, h.ID, 0)
	if len(list) != 3 || list[0].ID != weird.ID || list[0].Payload != nil {
		t.Errorf("ListDeliveries = %+v", list)
	}
	hook, _ := s.GetWebhook(ctx, a.WorkspaceID, h.ID)
	if hook.DeliveryCount != 3 || hook.LastDeliveryAt == nil {
		t.Errorf("hook stats = %+v", hook)
	}
	if got, _ := s.GetDelivery(ctx, a.WorkspaceID, d.ID); got.Headers["X-GitHub-Event"] != "workflow_run" || *got.Payload != `{"ok":false}` {
		t.Errorf("GetDelivery = %+v", got)
	}

	// Deleting the webhook deletes its deliveries.
	if evs, err := s.DeleteWebhook(ctx, a, h.ID); err != nil || evs[0].Type != "webhook.deleted" {
		t.Fatalf("DeleteWebhook = %+v, %v", evs, err)
	}
	if _, err := s.GetDelivery(ctx, a.WorkspaceID, d.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("delivery after delete err = %v, want ErrNotFound", err)
	}
}
