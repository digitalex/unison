package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ConfigFile is the per-directory session file written by `unison join`.
const ConfigFile = ".unison.json"

// Config is a saved session. Flags and environment variables override it.
type Config struct {
	Server        string `json:"server"`
	Workspace     string `json:"workspace"`
	Token         string `json:"token,omitempty"`
	ParticipantID int64  `json:"participant_id,omitempty"`
	Name          string `json:"name,omitempty"`
	// Cursor is the last event seq `unison wait` has seen.
	Cursor int64 `json:"cursor,omitempty"`

	path string
}

// findConfig returns UNISON_CONFIG, or the nearest .unison.json walking up
// from the working directory, or "" if there is none.
func findConfig() string {
	if p := os.Getenv("UNISON_CONFIG"); p != "" {
		return p
	}
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		p := filepath.Join(dir, ConfigFile)
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func loadConfig(path string) (*Config, error) {
	c := &Config{path: path}
	if path == "" {
		return c, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, c); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return c, nil
}

func (c *Config) save() error {
	if c.path == "" {
		c.path = ConfigFile
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, append(raw, '\n'), 0o600)
}

// client talks to the Unison HTTP API.
type client struct {
	server string
	token  string
	http   *http.Client
}

// apiError is an error response from the server.
type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string { return e.Message }

func (c *client) do(method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, strings.TrimRight(c.server, "/")+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach the Unison server at %s (is it running? `unison serve`): %w", c.server, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &e) != nil || e.Error == "" {
			e.Error = strings.TrimSpace(string(raw))
		}
		return &apiError{Status: resp.StatusCode, Message: e.Error}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func newClient(server, token string) *client {
	return &client{server: server, token: token, http: &http.Client{Timeout: 10 * time.Minute}}
}

// inviteToken returns the invite token in an invite link
// (…/w/ws_123/join?invite=ui_…), or "".
func inviteToken(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Query().Get("invite")
}

// parseInvite splits a workspace URL like http://host:4600/w/ws_123 into the
// server origin and workspace ID.
func parseInvite(raw string) (server, workspace string, err error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", "", fmt.Errorf("%q is not a workspace URL like http://localhost:4600/w/ws_1234", raw)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "w" {
			return u.Scheme + "://" + u.Host, parts[i+1], nil
		}
	}
	return u.Scheme + "://" + u.Host, "", nil
}
