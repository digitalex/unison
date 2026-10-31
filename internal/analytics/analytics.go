// Package analytics sends server-side events to Google Analytics through its
// Measurement Protocol. Agents use the CLI and API, never a browser, so only
// the server sees what both humans and agents do (#155).
package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// DefaultEndpoint is Google Analytics' Measurement Protocol endpoint.
const DefaultEndpoint = "https://www.google-analytics.com/mp/collect"

// Sender delivers one event for a pseudonymous client.
type Sender interface {
	Send(ctx context.Context, clientID, name string, params map[string]string) error
}

// Config holds the credentials for a GA4 web stream.
type Config struct {
	MeasurementID string
	APISecret     string
}

// ConfigFromEnv reads UNISON_GA_MEASUREMENT_ID and UNISON_GA_API_SECRET. ok is
// false unless both are set.
func ConfigFromEnv(getenv func(string) string) (cfg Config, ok bool) {
	cfg = Config{MeasurementID: getenv("UNISON_GA_MEASUREMENT_ID"), APISecret: getenv("UNISON_GA_API_SECRET")}
	return cfg, cfg.MeasurementID != "" && cfg.APISecret != ""
}

// Client is a Sender that posts to the Measurement Protocol.
type Client struct {
	Config
	Endpoint string // DefaultEndpoint; tests replace it
	HTTP     *http.Client
}

// NewClient returns a Client for cfg.
func NewClient(cfg Config) *Client {
	return &Client{Config: cfg, Endpoint: DefaultEndpoint, HTTP: &http.Client{Timeout: 5 * time.Second}}
}

type event struct {
	Name   string            `json:"name"`
	Params map[string]string `json:"params,omitempty"`
}

// Send posts one event. Errors never contain the API secret, which travels in
// the URL.
func (c *Client) Send(ctx context.Context, clientID, name string, params map[string]string) error {
	body, err := json.Marshal(struct {
		ClientID string  `json:"client_id"`
		Events   []event `json:"events"`
	}{clientID, []event{{name, params}}})
	if err != nil {
		return err
	}
	q := url.Values{"measurement_id": {c.MeasurementID}, "api_secret": {c.APISecret}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint+"?"+q.Encode(), bytes.NewReader(body))
	if err != nil {
		return errors.New("building the analytics request failed")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			return ue.Err
		}
		return errors.New("the analytics request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("analytics: status %d", resp.StatusCode)
	}
	return nil
}
