package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/digitalex/unison/internal/store"
)

// Accounts: sign in with GitHub or Google (OAuth 2.0 authorization code +
// PKCE), browser sessions in a cookie, CLI sessions via the device flow
// (`unison login`). Nothing here gates workspace access: participant tokens
// work exactly as before. The one optional gate is UNISON_CREATORS.

const (
	sessionCookie = "unison_session"
	oauthCookie   = "unison_oauth"
	// csrfHeader must accompany cookie-authenticated writes. Browsers only
	// send custom headers cross-origin after a CORS preflight, and Unison
	// answers none, so a foreign page cannot forge one.
	csrfHeader = "X-Unison-CSRF"
)

// OAuthProvider is an OAuth 2.0 identity provider. The endpoint URLs are
// fields so tests can point them at a fake provider.
type OAuthProvider struct {
	Name         string // "github" or "google" (the URL segment)
	Label        string // shown on the button
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
	UserURL      string
	EmailsURL    string // GitHub only: /user/emails
	Scopes       []string
}

// GitHubProvider returns GitHub's OAuth app endpoints.
func GitHubProvider(clientID, secret string) *OAuthProvider {
	return &OAuthProvider{
		Name: "github", Label: "GitHub", ClientID: clientID, ClientSecret: secret,
		AuthURL:   "https://github.com/login/oauth/authorize",
		TokenURL:  "https://github.com/login/oauth/access_token",
		UserURL:   "https://api.github.com/user",
		EmailsURL: "https://api.github.com/user/emails",
		Scopes:    []string{"read:user", "user:email"},
	}
}

// GoogleProvider returns Google's OpenID Connect endpoints.
func GoogleProvider(clientID, secret string) *OAuthProvider {
	return &OAuthProvider{
		Name: "google", Label: "Google", ClientID: clientID, ClientSecret: secret,
		AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL: "https://oauth2.googleapis.com/token",
		UserURL:  "https://openidconnect.googleapis.com/v1/userinfo",
		Scopes:   []string{"openid", "email", "profile"},
	}
}

// AuthConfig configures accounts. The zero value has no providers (sign-in
// hidden) and no creators allowlist.
type AuthConfig struct {
	Providers []*OAuthProvider
	// Creators, when non-empty, limits POST /api/workspaces to signed-in
	// users with one of these verified emails.
	Creators []string
	// DeviceInterval is the minimum seconds between `unison login` polls.
	DeviceInterval time.Duration
	// HTTPClient calls the providers (default: 10s timeout).
	HTTPClient *http.Client
	// Open turns off workspace privacy (UNISON_AUTH=off): reads are public
	// and anyone may join or create, as before accounts. For local
	// development without OAuth only.
	Open bool
}

// AuthConfigFromEnv reads UNISON_GITHUB_CLIENT_ID/SECRET,
// UNISON_GOOGLE_CLIENT_ID/SECRET and UNISON_CREATORS (comma-separated).
func AuthConfigFromEnv() AuthConfig {
	var cfg AuthConfig
	if id, secret := os.Getenv("UNISON_GITHUB_CLIENT_ID"), os.Getenv("UNISON_GITHUB_CLIENT_SECRET"); id != "" && secret != "" {
		cfg.Providers = append(cfg.Providers, GitHubProvider(id, secret))
	}
	if id, secret := os.Getenv("UNISON_GOOGLE_CLIENT_ID"), os.Getenv("UNISON_GOOGLE_CLIENT_SECRET"); id != "" && secret != "" {
		cfg.Providers = append(cfg.Providers, GoogleProvider(id, secret))
	}
	cfg.Open = os.Getenv("UNISON_AUTH") == "off"
	for _, e := range strings.Split(os.Getenv("UNISON_CREATORS"), ",") {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			cfg.Creators = append(cfg.Creators, e)
		}
	}
	return cfg
}

// SetAuth configures accounts; call it before serving.
func (s *Server) SetAuth(cfg AuthConfig) {
	if cfg.DeviceInterval <= 0 {
		cfg.DeviceInterval = 5 * time.Second
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	for i := range cfg.Creators {
		cfg.Creators[i] = strings.ToLower(strings.TrimSpace(cfg.Creators[i]))
	}
	s.authCfg = cfg
}

func (s *Server) authRoutes() {
	m := s.mux
	m.HandleFunc("GET /auth/{provider}/start", s.oauthStart)
	m.HandleFunc("GET /auth/{provider}/callback", s.oauthCallback)
	m.HandleFunc("GET /api/auth/providers", s.listProviders)
	m.HandleFunc("POST /api/auth/logout", s.requireUser(s.logout))
	m.HandleFunc("POST /api/auth/device", s.deviceStart)
	m.HandleFunc("POST /api/auth/device/token", s.deviceToken)
	m.HandleFunc("GET /api/auth/device/lookup", s.requireUser(s.deviceLookup))
	m.HandleFunc("POST /api/auth/device/approve", s.requireUser(s.deviceApprove))
	m.HandleFunc("POST /api/auth/app/token", s.appToken)
	m.HandleFunc("POST /api/invites/lookup", s.requireUser(s.lookupInvite))
	m.HandleFunc("GET /api/me/user", s.requireUser(s.meUser))
	m.HandleFunc("GET /api/me/sessions", s.requireUser(s.listSessions))
	m.HandleFunc("DELETE /api/me/sessions/{id}", s.requireUser(s.revokeSession))
	m.HandleFunc("POST /api/me/sessions/revoke-others", s.requireUser(s.revokeOtherSessions))
	m.HandleFunc("GET /api/me/agents", s.requireUser(s.listMyAgents))
	m.HandleFunc("POST /api/me/agents/{id}/revoke", s.requireUser(s.revokeAgent))
	m.HandleFunc("GET /api/me/devices", s.accountOnly(s.listDevices))
	m.HandleFunc("POST /api/me/devices", s.accountOnly(s.registerDevice))
	m.HandleFunc("DELETE /api/me/devices/{id}", s.accountOnly(s.deleteDevice))
}

func (s *Server) provider(name string) *OAuthProvider {
	for _, p := range s.authCfg.Providers {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// ---- Who is signed in ----

type userCtxKey struct{}

type userAuth struct {
	session  *store.Session
	user     *store.User
	byCookie bool
}

// currentUser resolves a user session from `Authorization: Bearer us_…` or
// the session cookie. Participant tokens are never user sessions.
func (s *Server) currentUser(r *http.Request) (*userAuth, error) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer "+store.UserTokenPrefix) {
		se, u, err := s.store.SessionByToken(r.Context(), strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			return nil, err
		}
		return &userAuth{session: se, user: u}, nil
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil, store.ErrNotFound
	}
	se, u, err := s.store.SessionByToken(r.Context(), c.Value)
	if err != nil {
		return nil, err
	}
	return &userAuth{session: se, user: u, byCookie: true}, nil
}

// csrfOK reports whether a cookie-authenticated request may proceed.
func csrfOK(r *http.Request, a *userAuth) bool {
	return !a.byCookie || r.Method == http.MethodGet || r.Method == http.MethodHead || r.Header.Get(csrfHeader) == "1"
}

// requireUser requires a signed-in user (and the CSRF header on
// cookie-authenticated writes).
func (s *Server) requireUser(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, ok := s.userOrError(w, r)
		if !ok {
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), userCtxKey{}, a)))
	}
}

func (s *Server) userOrError(w http.ResponseWriter, r *http.Request) (*userAuth, bool) {
	a, err := s.currentUser(r)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "not signed in (sign in on the web, or run `unison login`)")
		return nil, false
	}
	if err != nil {
		storeError(w, err)
		return nil, false
	}
	if !csrfOK(r, a) {
		writeError(w, http.StatusForbidden, "missing "+csrfHeader+" header")
		return nil, false
	}
	return a, true
}

func signedIn(r *http.Request) *userAuth { return r.Context().Value(userCtxKey{}).(*userAuth) }

// mayCreateWorkspace decides who may create a workspace: any signed-in
// user (who becomes its first member), narrowed by UNISON_CREATORS when set.
// In open mode (UNISON_AUTH=off) anyone may, as before. It returns the
// creator (nil when anonymous in open mode), or writes the error.
func (s *Server) mayCreateWorkspace(w http.ResponseWriter, r *http.Request) (*store.User, bool) {
	if s.authCfg.Open && len(s.authCfg.Creators) == 0 {
		if a, err := s.currentUser(r); err == nil && csrfOK(r, a) {
			return a.user, true
		}
		return nil, true
	}
	a, ok := s.userOrError(w, r)
	if !ok {
		return nil, false
	}
	if len(s.authCfg.Creators) == 0 {
		return a.user, true
	}
	for _, e := range a.user.VerifiedEmails() {
		if slices.Contains(s.authCfg.Creators, e) {
			return a.user, true
		}
	}
	writeError(w, http.StatusForbidden, "creating workspaces is limited to approved accounts on this server")
	return nil, false
}

// ---- Cookies ----

func isHTTPS(r *http.Request) bool { return strings.HasPrefix(requestOrigin(r), "https:") }

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", MaxAge: int(store.SessionTTL / time.Second),
		HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
}

func clearCookie(w http.ResponseWriter, r *http.Request, name, path string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: path, MaxAge: -1, HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode})
}

// ---- OAuth ----

func (s *Server) listProviders(w http.ResponseWriter, r *http.Request) {
	out := []map[string]string{}
	for _, p := range s.authCfg.Providers {
		out = append(out, map[string]string{"name": p.Name, "label": p.Label, "start_url": "/auth/" + p.Name + "/start"})
	}
	writeJSON(w, http.StatusOK, out)
}

// safeReturnTo accepts only same-origin relative paths ("/w/x?y"), so the
// login redirect cannot be used as an open redirect.
func safeReturnTo(v string) string {
	if !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") || strings.HasPrefix(v, "/\\") ||
		strings.ContainsAny(v, "\\\r\n\t") {
		return "/"
	}
	u, err := url.Parse(v)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return "/"
	}
	return v
}

func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func redirectURI(r *http.Request, provider string) string {
	return requestOrigin(r) + "/auth/" + provider + "/callback"
}

func (s *Server) oauthStart(w http.ResponseWriter, r *http.Request) {
	p := s.provider(r.PathValue("provider"))
	if p == nil {
		writeError(w, http.StatusNotFound, "sign-in with "+r.PathValue("provider")+" is not configured on this server")
		return
	}
	state, verifier := randomString(32), randomString(48)
	st := store.OAuthState{Provider: p.Name, Verifier: verifier, ReturnTo: safeReturnTo(r.URL.Query().Get("return_to"))}
	// link=1 while signed in: attach the identity to this user (profile
	// page). The intent is stored server-side with the state, so the
	// callback can't be talked into linking for anyone else. Signed out,
	// link=1 is an ordinary sign-in.
	if r.URL.Query().Get("link") == "1" {
		if a, err := s.currentUser(r); err == nil {
			st.LinkUserID = a.user.ID
		}
	}
	// app=1: a native app's sign-in (see appToken). Its PKCE challenge is
	// stored with the state; the callback redirects to the app with a code.
	if r.URL.Query().Get("app") == "1" {
		ch := r.URL.Query().Get("code_challenge")
		if !validChallenge(ch) {
			writeError(w, http.StatusBadRequest, "app sign-in needs a code_challenge: the base64url SHA-256 of a PKCE verifier")
			return
		}
		st.AppChallenge, st.LinkUserID = ch, 0
	}
	err := s.store.SaveOAuthState(r.Context(), state, st)
	if err != nil {
		storeError(w, err)
		return
	}
	// Bind the login to this browser: the callback must present this cookie.
	http.SetCookie(w, &http.Cookie{
		Name: oauthCookie, Value: state, Path: "/auth/", MaxAge: int(store.OAuthStateTTL / time.Second),
		HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
	challenge := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {p.ClientID},
		"redirect_uri":          {redirectURI(r, p.Name)},
		"scope":                 {strings.Join(p.Scopes, " ")},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
	}
	http.Redirect(w, r, p.AuthURL+"?"+q.Encode(), http.StatusFound)
}

func (s *Server) oauthCallback(w http.ResponseWriter, r *http.Request) {
	p := s.provider(r.PathValue("provider"))
	if p == nil {
		writeError(w, http.StatusNotFound, "sign-in with "+r.PathValue("provider")+" is not configured on this server")
		return
	}
	q := r.URL.Query()
	state := q.Get("state")
	c, err := r.Cookie(oauthCookie)
	clearCookie(w, r, oauthCookie, "/auth/")
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) != 1 {
		authFailed(w, http.StatusBadRequest, "This sign-in link is not valid in this browser. Please try again.")
		return
	}
	st, err := s.store.TakeOAuthState(r.Context(), state)
	if err != nil || st.Provider != p.Name {
		authFailed(w, http.StatusBadRequest, "This sign-in attempt expired. Please try again.")
		return
	}
	if e := q.Get("error"); e != "" {
		authFailed(w, http.StatusBadRequest, "Sign-in was cancelled ("+e+").")
		return
	}
	prof, err := s.fetchProfile(r.Context(), p, q.Get("code"), st.Verifier, redirectURI(r, p.Name))
	if err != nil {
		authFailed(w, http.StatusBadGateway, "Could not sign in with "+p.Label+": "+err.Error())
		return
	}
	if st.LinkUserID != 0 {
		s.finishLink(w, r, p, st, prof)
		return
	}
	u, err := s.store.LoginWithProvider(r.Context(), p.Name, *prof)
	if err != nil {
		storeError(w, err)
		return
	}
	if st.AppChallenge != "" {
		code, err := s.store.CreateAppCode(r.Context(), u.ID, st.AppChallenge)
		if err != nil {
			storeError(w, err)
			return
		}
		http.Redirect(w, r, appRedirect+"?code="+url.QueryEscape(code), http.StatusFound)
		return
	}
	token, _, err := s.store.CreateSession(r.Context(), u.ID, store.SessionBrowser, browserLabel(r.UserAgent()))
	if err != nil {
		storeError(w, err)
		return
	}
	setSessionCookie(w, r, token)
	http.Redirect(w, r, st.ReturnTo, http.StatusFound)
}

func authFailed(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Sign-in failed</title>
<body style="font:15px system-ui;margin:3rem auto;max-width:32rem;padding:0 1rem"><h1 style="font-size:1.2rem">Sign-in failed</h1><p>%s</p><p><a href="/">Back to Unison</a></p></body>`,
		html.EscapeString(msg))
}

// fetchProfile exchanges the authorization code (with the PKCE verifier)
// and reads the person's profile.
func (s *Server) fetchProfile(ctx context.Context, p *OAuthProvider, code, verifier, redirect string) (*store.Profile, error) {
	if code == "" {
		return nil, errors.New("no authorization code")
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	form := url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect},
		"client_id": {p.ClientID}, "client_secret": {p.ClientSecret}, "code_verifier": {verifier},
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := s.providerJSON(req, &tok); err != nil {
		return nil, err
	}
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("token exchange failed: %s", firstNonEmpty(tok.Error, "no access token"))
	}
	if p.EmailsURL != "" {
		return s.githubProfile(ctx, p, tok.AccessToken)
	}
	return s.oidcProfile(ctx, p, tok.AccessToken)
}

func (s *Server) providerGet(ctx context.Context, rawURL, accessToken string, out any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	return s.providerJSON(req, out)
}

func (s *Server) providerJSON(req *http.Request, out any) error {
	req.Header.Set("Accept", "application/json")
	resp, err := s.authCfg.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s returned %s", req.URL.Host, resp.Status)
	}
	return json.Unmarshal(body, out)
}

func (s *Server) githubProfile(ctx context.Context, p *OAuthProvider, accessToken string) (*store.Profile, error) {
	var gu struct {
		ID        json.Number `json:"id"`
		Login     string      `json:"login"`
		Name      string      `json:"name"`
		AvatarURL string      `json:"avatar_url"`
	}
	if err := s.providerGet(ctx, p.UserURL, accessToken, &gu); err != nil {
		return nil, err
	}
	if gu.ID == "" {
		return nil, errors.New("profile has no id")
	}
	prof := &store.Profile{Subject: gu.ID.String(), Name: firstNonEmpty(gu.Name, gu.Login), AvatarURL: gu.AvatarURL}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := s.providerGet(ctx, p.EmailsURL, accessToken, &emails); err != nil {
		return nil, err
	}
	for _, e := range emails {
		if e.Primary && e.Verified {
			prof.Email, prof.EmailVerified = e.Email, true
		}
	}
	return prof, nil
}

func (s *Server) oidcProfile(ctx context.Context, p *OAuthProvider, accessToken string) (*store.Profile, error) {
	var gu struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified any    `json:"email_verified"` // bool (sometimes "true")
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := s.providerGet(ctx, p.UserURL, accessToken, &gu); err != nil {
		return nil, err
	}
	if gu.Sub == "" {
		return nil, errors.New("profile has no subject")
	}
	verified := gu.EmailVerified == true || gu.EmailVerified == "true"
	return &store.Profile{Subject: gu.Sub, Email: gu.Email, EmailVerified: verified, Name: gu.Name, AvatarURL: gu.Picture}, nil
}

// browserLabel names a browser session from its User-Agent ("Chrome on macOS").
func browserLabel(ua string) string {
	name := "Browser"
	for _, b := range []struct{ token, name string }{
		{"Edg/", "Edge"}, {"Firefox/", "Firefox"}, {"Chrome/", "Chrome"}, {"Safari/", "Safari"},
	} {
		if strings.Contains(ua, b.token) {
			name = b.name
			break
		}
	}
	for _, o := range []struct{ token, name string }{
		{"iPhone", "iOS"}, {"iPad", "iOS"}, {"Android", "Android"}, {"Mac OS X", "macOS"},
		{"Windows", "Windows"}, {"CrOS", "ChromeOS"}, {"Linux", "Linux"},
	} {
		if strings.Contains(ua, o.token) {
			return name + " on " + o.name
		}
	}
	return name
}

// ---- Account API ----

func (s *Server) meUser(w http.ResponseWriter, r *http.Request) {
	a := signedIn(r)
	if a.byCookie {
		// The web app loads this on every visit: keep the cookie's lifetime
		// in step with the session's sliding expiry.
		c, _ := r.Cookie(sessionCookie)
		setSessionCookie(w, r, c.Value)
	}
	writeJSON(w, http.StatusOK, a.user)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	a := signedIn(r)
	err := s.store.RevokeSession(r.Context(), a.user.ID, a.session.ID)
	clearCookie(w, r, sessionCookie, "/")
	s.respond(w, http.StatusOK, map[string]any{"signed_out": true}, nil, err)
}

type sessionView struct {
	store.Session
	Current bool `json:"current"`
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	a := signedIn(r)
	list, err := s.store.ListSessions(r.Context(), a.user.ID)
	out := make([]sessionView, len(list))
	for i, se := range list {
		out[i] = sessionView{Session: se, Current: se.ID == a.session.ID}
	}
	s.respond(w, http.StatusOK, out, nil, err)
}

func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	a := signedIn(r)
	err := s.store.RevokeSession(r.Context(), a.user.ID, id)
	if err == nil && id == a.session.ID && a.byCookie {
		clearCookie(w, r, sessionCookie, "/")
	}
	s.respond(w, http.StatusOK, map[string]any{"revoked": id}, nil, err)
}

// ---- Native app sign-in ----

// appRedirect is where an app's sign-in returns, with ?code=: the iPhone
// app's URL scheme, which its ASWebAuthenticationSession listens for.
const appRedirect = "unison://auth"

// validChallenge reports whether ch looks like an S256 PKCE challenge: 43
// base64url characters (a SHA-256 hash).
func validChallenge(ch string) bool {
	b, err := base64.RawURLEncoding.DecodeString(ch)
	return err == nil && len(b) == sha256.Size
}

// appToken redeems the code from an app sign-in (/auth/{provider}/start?app=1)
// for a session, given the PKCE verifier whose hash started it.
func (s *Server) appToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code         string `json:"code"`
		CodeVerifier string `json:"code_verifier"`
		Label        string `json:"label"`
	}
	if !decode(w, r, &req) {
		return
	}
	token, err := s.store.RedeemAppCode(r.Context(), req.Code, req.CodeVerifier, firstNonEmpty(req.Label, "app"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "this sign-in code is invalid, used or expired: sign in again")
		return
	}
	s.respond(w, http.StatusOK, map[string]any{"token": token}, nil, err)
}

// ---- Device flow (`unison login`) ----

func (s *Server) deviceStart(w http.ResponseWriter, r *http.Request) {
	d, err := s.store.StartDevice(r.Context(), s.authCfg.DeviceInterval)
	if err != nil {
		storeError(w, err)
		return
	}
	verify := requestOrigin(r) + "/auth/device"
	writeJSON(w, http.StatusOK, map[string]any{
		"device_code":               d.DeviceCode,
		"user_code":                 d.UserCode,
		"verification_url":          verify,
		"verification_url_complete": verify + "?code=" + url.QueryEscape(d.UserCode),
		"interval":                  d.Interval,
		"expires_in":                d.ExpiresIn,
	})
}

func (s *Server) deviceToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceCode string `json:"device_code"`
		Label      string `json:"label"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.DeviceCode == "" {
		writeError(w, http.StatusBadRequest, "device_code is required")
		return
	}
	status, token, err := s.store.PollDevice(r.Context(), req.DeviceCode, firstNonEmpty(req.Label, "unison login"))
	out := map[string]any{"status": status}
	if token != "" {
		out["token"] = token
	}
	s.respond(w, http.StatusOK, out, nil, err)
}

func formatUserCode(code string) string {
	c := store.NormalizeUserCode(code)
	if len(c) == 8 {
		return c[:4] + "-" + c[4:]
	}
	return c
}

func (s *Server) deviceLookup(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	pending, err := s.store.PendingDeviceCode(r.Context(), code)
	s.respond(w, http.StatusOK, map[string]any{"user_code": formatUserCode(code), "pending": pending}, nil, err)
}

func (s *Server) deviceApprove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserCode string `json:"user_code"`
		Approve  bool   `json:"approve"`
	}
	if !decode(w, r, &req) {
		return
	}
	err := s.store.DecideDevice(r.Context(), signedIn(r).user.ID, req.UserCode, req.Approve)
	status := store.DeviceDenied
	if req.Approve {
		status = store.DeviceApproved
	}
	s.respond(w, http.StatusOK, map[string]any{"status": status, "user_code": formatUserCode(req.UserCode)}, nil, err)
}

// finishLink completes a link-mode OAuth flow: the browser must still be
// signed in as the user who started it, and the identity must not belong
// to someone else. No new session is created.
func (s *Server) finishLink(w http.ResponseWriter, r *http.Request, p *OAuthProvider, st *store.OAuthState, prof *store.Profile) {
	a, err := s.currentUser(r)
	if err != nil || a.user.ID != st.LinkUserID {
		authFailed(w, http.StatusBadRequest, "You signed out or switched accounts while linking "+p.Label+". Sign in again and retry from your profile.")
		return
	}
	if _, err := s.store.LinkIdentity(r.Context(), a.user.ID, p.Name, *prof); err != nil {
		if errors.Is(err, store.ErrConflict) {
			authFailed(w, http.StatusConflict, "That "+p.Label+" account is already linked to another Unison account, so it can't be linked to this one. Sign in with it to use that account instead.")
			return
		}
		storeError(w, err)
		return
	}
	http.Redirect(w, r, st.ReturnTo, http.StatusFound)
}

// revokeOtherSessions signs out every session but the caller's own.
func (s *Server) revokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	a := signedIn(r)
	n, err := s.store.RevokeOtherSessions(r.Context(), a.user.ID, a.session.ID)
	s.respond(w, http.StatusOK, map[string]any{"revoked": n}, nil, err)
}

// listMyAgents lists the agents the caller sponsors, across workspaces.
func (s *Server) listMyAgents(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListMyAgents(r.Context(), signedIn(r).user.ID)
	s.respond(w, http.StatusOK, list, nil, err)
}

// revokeAgent stops one of the caller's agents (participant id; ids are
// unique across workspaces).
func (s *Server) revokeAgent(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, evs, err := s.store.RevokeAgent(r.Context(), signedIn(r).user.ID, id)
	s.respond(w, http.StatusOK, p, evs, err)
}
