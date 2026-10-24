package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/digitalex/unison/internal/store"
)

// Accounts: `unison login` signs a person in with the device flow and keeps
// the user token per server in a credentials file. Workspace commands keep
// using their participant token from .unison.json; only workspace creation
// also presents the user token (servers may limit creation to accounts).

// credential is one server's saved sign-in.
type credential struct {
	Token string `json:"token"`
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
}

type credentials struct {
	Servers map[string]credential `json:"servers"`
	path    string
}

// credentialsPath is UNISON_CREDENTIALS, or credentials.json in the user
// config directory (e.g. ~/.config/unison/).
func credentialsPath() (string, error) {
	if p := os.Getenv("UNISON_CREDENTIALS"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "unison", "credentials.json"), nil
}

func loadCredentials() (*credentials, error) {
	path, err := credentialsPath()
	if err != nil {
		return nil, err
	}
	c := &credentials{Servers: map[string]credential{}, path: path}
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
	if c.Servers == nil {
		c.Servers = map[string]credential{}
	}
	return c, nil
}

func (c *credentials) save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// Write then rename, so the file is never world-readable or half-written.
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// serverKey normalizes a server URL to its origin ("https://host:port").
func serverKey(server string) string {
	u, err := url.Parse(strings.TrimSpace(server))
	if err != nil || u.Host == "" {
		return strings.TrimRight(strings.ToLower(server), "/")
	}
	host := strings.ToLower(u.Host)
	scheme := strings.ToLower(u.Scheme)
	host = strings.TrimSuffix(host, map[string]string{"http": ":80", "https": ":443"}[scheme])
	return scheme + "://" + host
}

// userToken returns the saved user token for the current server, or "".
func (a *app) userToken() string {
	c, err := loadCredentials()
	if err != nil {
		return ""
	}
	return c.Servers[serverKey(a.server)].Token
}

func (a *app) login(args []string) error {
	if _, err := parseFlags(flag.NewFlagSet("login", flag.ContinueOnError), args); err != nil {
		return err
	}
	c := newClient(a.server, "")
	var d struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURL         string `json:"verification_url"`
		VerificationURLComplete string `json:"verification_url_complete"`
		Interval                int    `json:"interval"`
		ExpiresIn               int    `json:"expires_in"`
	}
	if err := c.do("POST", "/api/auth/device", nil, &d); err != nil {
		return err
	}
	a.progress("To sign in to %s, open\n\n    %s\n\nand confirm the code %s\n\n", serverKey(a.server), d.VerificationURLComplete, d.UserCode)
	if a.interactive() && os.Getenv("UNISON_NO_BROWSER") == "" {
		openBrowser(d.VerificationURLComplete)
	}
	token, err := pollDevice(c, d.DeviceCode, time.Duration(d.Interval)*time.Second, time.Duration(d.ExpiresIn)*time.Second)
	if err != nil {
		return err
	}
	var u store.User
	if err := newClient(a.server, token).do("GET", "/api/me/user", nil, &u); err != nil {
		return err
	}
	creds, err := loadCredentials()
	if err != nil {
		return err
	}
	cred := credential{Token: token, Name: u.DisplayName}
	if emails := u.VerifiedEmails(); len(emails) > 0 {
		cred.Email = emails[0]
	}
	creds.Servers[serverKey(a.server)] = cred
	if err := creds.save(); err != nil {
		return err
	}
	if a.jsonOut {
		raw, _ := json.Marshal(map[string]any{"server": serverKey(a.server), "user": u})
		return a.printJSON(raw)
	}
	a.printf("Signed in as %s. Token saved to %s.\n", userLabel(&u), creds.path)
	return nil
}

// progress prints progress for humans (stdout, or nothing in --json mode).
func (a *app) progress(format string, args ...any) {
	if !a.jsonOut {
		a.printf(format, args...)
	}
}

// interactive reports whether stdout is a terminal.
func (a *app) interactive() bool {
	f, ok := a.out.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	if cmd.Start() == nil {
		go cmd.Wait()
	}
}

// pollDevice polls until the code is approved (returning the user token),
// denied or expired, slowing down when the server asks to.
func pollDevice(c *client, deviceCode string, interval, expiresIn time.Duration) (string, error) {
	interval = max(interval, time.Second)
	deadline := time.Now().Add(expiresIn)
	for time.Now().Before(deadline) {
		time.Sleep(interval)
		var resp struct {
			Status string `json:"status"`
			Token  string `json:"token"`
		}
		if err := c.do("POST", "/api/auth/device/token", map[string]string{"device_code": deviceCode, "label": deviceLabel()}, &resp); err != nil {
			return "", err
		}
		switch resp.Status {
		case store.DeviceApproved:
			return resp.Token, nil
		case store.DeviceSlowDown:
			interval += 5 * time.Second
		case store.DeviceDenied:
			return "", errors.New("sign-in was denied in the browser")
		case store.DeviceExpired:
			return "", errors.New("the code expired; run `unison login` again")
		}
	}
	return "", errors.New("the code expired; run `unison login` again")
}

// deviceLabel names the CLI session ("unison on laptop").
func deviceLabel() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return "unison on " + h
	}
	return "unison login"
}

func userLabel(u *store.User) string {
	label := firstNonEmpty(u.DisplayName, fmt.Sprintf("user %d", u.ID))
	if emails := u.VerifiedEmails(); len(emails) > 0 {
		label += " <" + emails[0] + ">"
	}
	return label
}

func (a *app) logout(args []string) error {
	if _, err := parseFlags(flag.NewFlagSet("logout", flag.ContinueOnError), args); err != nil {
		return err
	}
	creds, err := loadCredentials()
	if err != nil {
		return err
	}
	key := serverKey(a.server)
	cred, ok := creds.Servers[key]
	if !ok {
		return fmt.Errorf("not signed in to %s", key)
	}
	// Revoke the session; a token the server no longer knows is fine.
	err = newClient(a.server, cred.Token).do("POST", "/api/auth/logout", nil, nil)
	var apiErr *apiError
	if err != nil && !(errors.As(err, &apiErr) && apiErr.Status == 401) {
		return err
	}
	delete(creds.Servers, key)
	if err := creds.save(); err != nil {
		return err
	}
	a.progress("Signed out of %s.\n", key)
	return nil
}

func (a *app) whoamiUser() error {
	token := a.userToken()
	if token == "" {
		return fmt.Errorf("not signed in to %s: run `unison login`", serverKey(a.server))
	}
	var raw json.RawMessage
	if err := newClient(a.server, token).do("GET", "/api/me/user", nil, &raw); err != nil {
		return err
	}
	if a.jsonOut {
		return a.printJSON(raw)
	}
	var u store.User
	if err := json.Unmarshal(raw, &u); err != nil {
		return err
	}
	a.printf("%s (user %d) on %s\n", firstNonEmpty(u.DisplayName, "(no name)"), u.ID, serverKey(a.server))
	for _, id := range u.Identities {
		email := id.Email
		if email != "" && !id.EmailVerified {
			email += " (unverified)"
		}
		a.printf("  %-7s %s\n", id.Provider, firstNonEmpty(email, id.Subject))
	}
	return nil
}
