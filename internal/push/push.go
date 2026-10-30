// Package push sends notifications to iOS devices through the Apple Push
// Notification service (APNs) HTTP/2 API, authenticating with a token-based
// (.p8) provider key.
package push

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// APNs endpoints.
const (
	ProductionURL = "https://api.push.apple.com"
	SandboxURL    = "https://api.sandbox.push.apple.com"
)

// jwtTTL is how long a provider token is reused. APNs rejects tokens older
// than an hour and throttles refreshing more often than every 20 minutes.
const jwtTTL = 50 * time.Minute

// ErrUnregistered means APNs will never deliver to the device token again
// (the app was uninstalled, or the token is for another app or
// environment); the device should be forgotten.
var ErrUnregistered = errors.New("device token is no longer valid")

// Notification is an alert about a workspace post. Zero IDs are omitted
// from the payload.
type Notification struct {
	Title       string
	Body        string
	WorkspaceID string
	MessageID   int64
	ParentID    int64 // thread root, for replies
	ItemID      int64
	CommentID   int64
	Kind        string // "action", "mention", "comment_mention" or KindRead
	// ReadUpTo is, for KindRead, the newest chat message the account's
	// participant in WorkspaceID has read.
	ReadUpTo int64
}

// KindRead is a silent background push (#122): the user has read chat up to
// ReadUpTo elsewhere, so the app can clear the notifications that covers.
const KindRead = "read"

// Payload returns the APNs JSON payload for n.
func (n Notification) Payload() ([]byte, error) {
	if n.Kind == KindRead {
		return json.Marshal(map[string]any{
			"aps":          map[string]any{"content-available": 1},
			"workspace_id": n.WorkspaceID,
			"kind":         n.Kind,
			"read_up_to":   n.ReadUpTo,
		})
	}
	p := map[string]any{
		"aps": map[string]any{
			"alert":     map[string]string{"title": n.Title, "body": n.Body},
			"sound":     "default",
			"thread-id": n.WorkspaceID,
		},
		"workspace_id": n.WorkspaceID,
		"kind":         n.Kind,
	}
	for key, id := range map[string]int64{"message_id": n.MessageID, "parent_id": n.ParentID, "item_id": n.ItemID, "comment_id": n.CommentID} {
		if id != 0 {
			p[key] = id
		}
	}
	return json.Marshal(p)
}

// Sender delivers a notification to one device token.
type Sender interface {
	Send(ctx context.Context, deviceToken string, n Notification) error
}

// Config configures a Client.
type Config struct {
	KeyPEM  string // the .p8 provider key (PKCS#8 PEM)
	KeyID   string
	TeamID  string
	Topic   string // the app's bundle ID
	Sandbox bool
}

// ConfigFromEnv reads APNS_KEY, APNS_KEY_ID, APNS_TEAM_ID, APNS_TOPIC and
// APNS_SANDBOX=1 through getenv. ok is false unless all four required
// variables are set.
func ConfigFromEnv(getenv func(string) string) (cfg Config, ok bool) {
	cfg = Config{KeyPEM: getenv("APNS_KEY"), KeyID: getenv("APNS_KEY_ID"), TeamID: getenv("APNS_TEAM_ID"),
		Topic: getenv("APNS_TOPIC"), Sandbox: getenv("APNS_SANDBOX") == "1"}
	return cfg, cfg.KeyPEM != "" && cfg.KeyID != "" && cfg.TeamID != "" && cfg.Topic != ""
}

// Client sends notifications to APNs.
type Client struct {
	// BaseURL is the APNs endpoint (ProductionURL or SandboxURL).
	BaseURL string
	// HTTP sends the requests; net/http speaks HTTP/2 to APNs over TLS.
	HTTP *http.Client

	cfg Config
	key *ecdsa.PrivateKey
	now func() time.Time

	mu    sync.Mutex
	jwt   string
	jwtAt time.Time
}

// NewClient returns a Client for cfg, or an error if the key is unusable.
func NewClient(cfg Config) (*Client, error) {
	key, err := parseKey(cfg.KeyPEM)
	if err != nil {
		return nil, err
	}
	base := ProductionURL
	if cfg.Sandbox {
		base = SandboxURL
	}
	return &Client{BaseURL: base, HTTP: &http.Client{}, cfg: cfg, key: key, now: time.Now}, nil
}

func parseKey(keyPEM string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(keyPEM))
	if block == nil {
		return nil, errors.New("APNS_KEY: no PEM block found")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("APNS_KEY: %w", err)
	}
	ec, ok := k.(*ecdsa.PrivateKey)
	if !ok || ec.Curve != elliptic.P256() {
		return nil, errors.New("APNS_KEY: not a P-256 EC key")
	}
	return ec, nil
}

// token returns the cached provider token, signing a new one when it is
// older than jwtTTL.
func (c *Client) token() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if c.jwt != "" && now.Sub(c.jwtAt) < jwtTTL {
		return c.jwt, nil
	}
	jwt, err := signJWT(c.key, c.cfg.KeyID, c.cfg.TeamID, now)
	if err != nil {
		return "", err
	}
	c.jwt, c.jwtAt = jwt, now
	return jwt, nil
}

func (c *Client) resetToken() {
	c.mu.Lock()
	c.jwt = ""
	c.mu.Unlock()
}

// signJWT makes an ES256 provider token: header {alg, kid}, claims {iss,
// iat}, and the signature as the raw 32-byte r and s.
func signJWT(key *ecdsa.PrivateKey, keyID, teamID string, now time.Time) (string, error) {
	enc := base64.RawURLEncoding
	header, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": keyID})
	claims, _ := json.Marshal(map[string]any{"iss": teamID, "iat": now.Unix()})
	signing := enc.EncodeToString(header) + "." + enc.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signing + "." + enc.EncodeToString(sig), nil
}

// Send delivers n to deviceToken. It returns ErrUnregistered when APNs says
// the token is dead. Errors never include the token.
func (c *Client) Send(ctx context.Context, deviceToken string, n Notification) error {
	body, err := n.Payload()
	if err != nil {
		return err
	}
	jwt, err := c.token()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/3/device/"+url.PathEscape(deviceToken), bytes.NewReader(body))
	if err != nil {
		return errors.New("apns: building request failed")
	}
	req.Header.Set("apns-topic", c.cfg.Topic)
	if n.Kind == KindRead {
		// Background pushes must be type background, priority 5.
		req.Header.Set("apns-push-type", "background")
		req.Header.Set("apns-priority", "5")
	} else {
		req.Header.Set("apns-push-type", "alert")
	}
	req.Header.Set("authorization", "bearer "+jwt)
	req.Header.Set("content-type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		// A *url.Error would quote the URL, and with it the token.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return fmt.Errorf("apns: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	var reply struct {
		Reason string `json:"reason"`
	}
	json.NewDecoder(resp.Body).Decode(&reply)
	switch {
	case resp.StatusCode == http.StatusGone,
		resp.StatusCode == http.StatusBadRequest && (reply.Reason == "BadDeviceToken" || reply.Reason == "DeviceTokenNotForTopic"):
		return fmt.Errorf("apns: %d %s: %w", resp.StatusCode, reply.Reason, ErrUnregistered)
	case resp.StatusCode == http.StatusForbidden && reply.Reason == "ExpiredProviderToken":
		c.resetToken()
	}
	return fmt.Errorf("apns: %d %s", resp.StatusCode, reply.Reason)
}
