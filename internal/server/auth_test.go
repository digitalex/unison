package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/digitalex/unison/internal/store"
	"github.com/digitalex/unison/internal/store/storetest"
)

// fakeProvider is a minimal OAuth provider (GitHub- and Google-shaped).
type fakeProvider struct {
	t  *testing.T
	ts *httptest.Server

	mu sync.Mutex
	// Per authorization code: the PKCE challenge and redirect_uri the
	// browser was sent with, and the profile to return.
	challenge map[string]string
	redirect  map[string]string
	profile   map[string]map[string]any // access token -> user JSON
	emails    map[string][]map[string]any
	verifiers []string // code_verifiers presented at the token endpoint
}

func newFakeProvider(t *testing.T) *fakeProvider {
	f := &fakeProvider{t: t, challenge: map[string]string{}, redirect: map[string]string{},
		profile: map[string]map[string]any{}, emails: map[string][]map[string]any{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", f.token)
	mux.HandleFunc("GET /user", f.user)
	mux.HandleFunc("GET /user/emails", f.userEmails)
	mux.HandleFunc("GET /userinfo", f.user)
	f.ts = httptest.NewServer(mux)
	t.Cleanup(f.ts.Close)
	return f
}

func (f *fakeProvider) providers() []*OAuthProvider {
	gh := GitHubProvider("gh-id", "gh-secret")
	gh.AuthURL, gh.TokenURL, gh.UserURL, gh.EmailsURL = f.ts.URL+"/authorize", f.ts.URL+"/token", f.ts.URL+"/user", f.ts.URL+"/user/emails"
	g := GoogleProvider("g-id", "g-secret")
	g.AuthURL, g.TokenURL, g.UserURL = f.ts.URL+"/authorize", f.ts.URL+"/token", f.ts.URL+"/userinfo"
	return []*OAuthProvider{gh, g}
}

// authorize plays the provider's consent screen: it remembers what the
// browser was sent with and returns a code for that person.
func (f *fakeProvider) authorize(authURL string, user map[string]any, emails []map[string]any) (code, state, redirect string) {
	u, err := url.Parse(authURL)
	if err != nil || !strings.HasPrefix(authURL, f.ts.URL+"/authorize?") {
		f.t.Fatalf("redirected to %q, want the provider", authURL)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("response_type") != "code" {
		f.t.Fatalf("authorize request lacks PKCE: %v", q)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	code = "code-" + q.Get("state")[:8]
	f.challenge[code] = q.Get("code_challenge")
	f.redirect[code] = q.Get("redirect_uri")
	f.profile["at-"+code] = user
	f.emails["at-"+code] = emails
	return code, q.Get("state"), q.Get("redirect_uri")
}

func (f *fakeProvider) token(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	f.mu.Lock()
	defer f.mu.Unlock()
	code, verifier := r.Form.Get("code"), r.Form.Get("code_verifier")
	f.verifiers = append(f.verifiers, verifier)
	sum := sha256.Sum256([]byte(verifier))
	switch {
	case !strings.HasSuffix(r.Form.Get("client_secret"), "-secret"):
		writeJSON(w, 401, map[string]string{"error": "invalid_client"})
	case f.challenge[code] == "" || base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge[code]:
		writeJSON(w, 400, map[string]string{"error": "invalid_grant (PKCE)"})
	case r.Form.Get("redirect_uri") != f.redirect[code]:
		writeJSON(w, 400, map[string]string{"error": "redirect_uri mismatch"})
	default:
		delete(f.challenge, code)
		writeJSON(w, 200, map[string]string{"access_token": "at-" + code, "token_type": "bearer"})
	}
}

func (f *fakeProvider) bearer(w http.ResponseWriter, r *http.Request) string {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if _, ok := f.profile[tok]; !ok {
		writeJSON(w, 401, map[string]string{"message": "bad token"})
		return ""
	}
	return tok
}

func (f *fakeProvider) user(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if tok := f.bearer(w, r); tok != "" {
		writeJSON(w, 200, f.profile[tok])
	}
}

func (f *fakeProvider) userEmails(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if tok := f.bearer(w, r); tok != "" {
		writeJSON(w, 200, f.emails[tok])
	}
}

type authEnv struct {
	t    *testing.T
	ts   *httptest.Server
	st   *store.Store
	fake *fakeProvider
	srv  *Server
}

func newAuthEnv(t *testing.T, creators ...string) *authEnv {
	t.Helper()
	st, err := store.Open(storetest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	fake := newFakeProvider(t)
	srv := New(st, fstest.MapFS{"index.html": {Data: []byte("<html>unison</html>")}})
	srv.SetAuth(AuthConfig{Providers: fake.providers(), Creators: creators, DeviceInterval: time.Second})
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return &authEnv{t: t, ts: ts, st: st, fake: fake, srv: srv}
}

// noRedirect is a client that returns redirects instead of following them.
var noRedirect = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

type reqOpt func(*http.Request)

func withCookie(c *http.Cookie) reqOpt { return func(r *http.Request) { r.AddCookie(c) } }
func withHeader(k, v string) reqOpt    { return func(r *http.Request) { r.Header.Set(k, v) } }
func withBearer(token string) reqOpt   { return withHeader("Authorization", "Bearer "+token) }
func withCSRF() reqOpt                 { return withHeader(csrfHeader, "1") }
func withHTTPS() reqOpt                { return withHeader("X-Forwarded-Proto", "https") }
func cookieNamed(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func (e *authEnv) do(method, path, body string, opts ...reqOpt) (*http.Response, map[string]any) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.ts.URL+path, strings.NewReader(body))
	for _, o := range opts {
		o(req)
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	json.Unmarshal(raw, &out)
	return resp, out
}

var (
	adaGitHub       = map[string]any{"id": 101, "login": "ada", "name": "Ada Lovelace", "avatar_url": "https://avatars/ada.png"}
	adaGitHubEmails = []map[string]any{
		{"email": "old@example.com", "primary": false, "verified": true},
		{"email": "Ada@Example.com", "primary": true, "verified": true},
	}
	adaGoogle = map[string]any{"sub": "g-101", "email": "ada@example.com", "email_verified": true, "name": "Ada L."}
	bobGoogle = map[string]any{"sub": "g-202", "email": "bob@example.com", "email_verified": true, "name": "Bob"}
)

// login runs the whole browser flow and returns the session cookie and the
// final redirect.
func (e *authEnv) login(provider, returnTo string, user map[string]any, emails []map[string]any, opts ...reqOpt) (*http.Cookie, string) {
	e.t.Helper()
	resp, _ := e.do("GET", "/auth/"+provider+"/start?return_to="+url.QueryEscape(returnTo), "", opts...)
	if resp.StatusCode != http.StatusFound {
		e.t.Fatalf("start = %d", resp.StatusCode)
	}
	stateCookie := cookieNamed(resp, oauthCookie)
	if stateCookie == nil || !stateCookie.HttpOnly || stateCookie.Path != "/auth/" {
		e.t.Fatalf("state cookie = %+v", stateCookie)
	}
	code, state, redirect := e.fake.authorize(resp.Header.Get("Location"), user, emails)
	if want := strings.TrimPrefix(e.ts.URL, "http:") + "/auth/" + provider + "/callback"; !strings.HasSuffix(redirect, want) {
		e.t.Errorf("redirect_uri = %q, want …%q", redirect, want)
	}
	resp, body := e.do("GET", "/auth/"+provider+"/callback?code="+code+"&state="+state, "", append(opts, withCookie(stateCookie))...)
	if resp.StatusCode != http.StatusFound {
		e.t.Fatalf("callback = %d %v", resp.StatusCode, body)
	}
	c := cookieNamed(resp, sessionCookie)
	if c == nil {
		e.t.Fatal("callback set no session cookie")
	}
	return c, resp.Header.Get("Location")
}

func TestOAuthLogin(t *testing.T) {
	e := newAuthEnv(t)
	c, loc := e.login("github", "/w/ws_1?tab=chat", adaGitHub, adaGitHubEmails)
	if loc != "/w/ws_1?tab=chat" {
		t.Errorf("redirect after login = %q", loc)
	}
	if !c.HttpOnly || c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge != int(store.SessionTTL/time.Second) {
		t.Errorf("session cookie = %+v", c)
	}
	if !strings.HasPrefix(c.Value, store.UserTokenPrefix) {
		t.Errorf("session token %q lacks the user prefix", c.Value)
	}
	resp, u := e.do("GET", "/api/me/user", "", withCookie(c))
	ids, _ := u["identities"].([]any)
	if resp.StatusCode != 200 || u["display_name"] != "Ada Lovelace" || len(ids) != 1 {
		t.Fatalf("me = %d %v", resp.StatusCode, u)
	}
	if id := ids[0].(map[string]any); id["provider"] != "github" || id["subject"] != "101" || id["email"] != "Ada@Example.com" || id["email_verified"] != true {
		t.Errorf("github identity = %v (want the primary verified email)", id)
	}
	if v := e.fake.verifiers[0]; len(v) < 43 {
		t.Errorf("PKCE verifier %q is too short", v)
	}

	// Google with the same verified email signs in to the same user.
	g, _ := e.login("google", "/", adaGoogle, nil)
	_, gu := e.do("GET", "/api/me/user", "", withCookie(g))
	if gu["id"] != u["id"] || len(gu["identities"].([]any)) != 2 {
		t.Errorf("google login = %v, want linked to user %v", gu, u["id"])
	}

	// Behind a TLS proxy the cookies are Secure.
	sc, _ := e.login("google", "/", bobGoogle, nil, withHTTPS())
	if !sc.Secure {
		t.Error("session cookie over https is not Secure")
	}

	if resp, _ := e.do("GET", "/api/me/user", ""); resp.StatusCode != 401 {
		t.Errorf("signed-out me = %d, want 401", resp.StatusCode)
	}
	resp, list := e.doList("GET", "/api/auth/providers")
	if resp.StatusCode != 200 || len(list) != 2 || list[0]["name"] != "github" || list[0]["start_url"] != "/auth/github/start" {
		t.Errorf("providers = %v", list)
	}
}

func (e *authEnv) doList(method, path string, opts ...reqOpt) (*http.Response, []map[string]any) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.ts.URL+path, nil)
	for _, o := range opts {
		o(req)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out []map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestOAuthRejects(t *testing.T) {
	e := newAuthEnv(t)
	start := func() (*http.Cookie, string, string) {
		resp, _ := e.do("GET", "/auth/github/start", "")
		code, state, _ := e.fake.authorize(resp.Header.Get("Location"), adaGitHub, adaGitHubEmails)
		return cookieNamed(resp, oauthCookie), code, state
	}
	callback := func(code, state string, opts ...reqOpt) int {
		resp, _ := e.do("GET", "/auth/github/callback?code="+code+"&state="+state, "", opts...)
		if cookieNamed(resp, sessionCookie) != nil {
			t.Errorf("callback (state %q) signed someone in", state)
		}
		return resp.StatusCode
	}

	c, code, state := start()
	if got := callback(code, state); got != 400 {
		t.Errorf("callback without the state cookie = %d, want 400 (login CSRF)", got)
	}
	other, _, _ := start()
	if got := callback(code, state, withCookie(other)); got != 400 {
		t.Errorf("callback with another login's cookie = %d, want 400", got)
	}
	if got := callback(code, "forged", withCookie(&http.Cookie{Name: oauthCookie, Value: "forged"})); got != 400 {
		t.Errorf("forged state = %d, want 400", got)
	}
	resp, _ := e.do("GET", "/auth/github/callback?error=access_denied&state="+state, "", withCookie(c))
	if resp.StatusCode != 400 {
		t.Errorf("provider error = %d", resp.StatusCode)
	}
	// The state was used up by the cancelled attempt.
	if got := callback(code, state, withCookie(c)); got != 400 {
		t.Errorf("reused state = %d, want 400", got)
	}

	// A state started for GitHub cannot finish at Google.
	c, code, state = start()
	resp, _ = e.do("GET", "/auth/google/callback?code="+code+"&state="+state, "", withCookie(c))
	if resp.StatusCode != 400 {
		t.Errorf("cross-provider callback = %d", resp.StatusCode)
	}
	if resp, _ := e.do("GET", "/auth/gitlab/start", ""); resp.StatusCode != 404 {
		t.Errorf("unconfigured provider = %d, want 404", resp.StatusCode)
	}

	// return_to must be a same-origin path.
	for in, want := range map[string]string{
		"/w/ws_1":              "/w/ws_1",
		"//evil.example":       "/",
		"/\\evil.example":      "/",
		"https://evil.example": "/",
		"javascript:alert(1)":  "/",
		"w/ws_1":               "/",
		"":                     "/",
	} {
		if got := safeReturnTo(in); got != want {
			t.Errorf("safeReturnTo(%q) = %q, want %q", in, got, want)
		}
	}
	if _, loc := e.login("github", "//evil.example/x", adaGitHub, adaGitHubEmails); loc != "/" {
		t.Errorf("open redirect: login returned to %q", loc)
	}
}

func TestSessionsAndCSRF(t *testing.T) {
	e := newAuthEnv(t)
	c, _ := e.login("github", "/", adaGitHub, adaGitHubEmails)
	c2, _ := e.login("github", "/", adaGitHub, adaGitHubEmails)

	resp, list := e.doList("GET", "/api/me/sessions", withCookie(c))
	if resp.StatusCode != 200 || len(list) != 2 {
		t.Fatalf("sessions = %d %v", resp.StatusCode, list)
	}
	var otherID string
	for _, s := range list {
		if s["kind"] != "browser" {
			t.Errorf("session kind = %v", s["kind"])
		}
		if s["current"] == false {
			otherID = jsonID(s["id"])
		}
	}

	// Cookie-authenticated writes need the CSRF header.
	if resp, _ := e.do("DELETE", "/api/me/sessions/"+otherID, "", withCookie(c)); resp.StatusCode != 403 {
		t.Errorf("revoke without CSRF header = %d, want 403", resp.StatusCode)
	}
	if resp, _ := e.do("POST", "/api/auth/logout", "", withCookie(c)); resp.StatusCode != 403 {
		t.Errorf("logout without CSRF header = %d, want 403", resp.StatusCode)
	}
	if resp, _ := e.do("DELETE", "/api/me/sessions/"+otherID, "", withCookie(c), withCSRF()); resp.StatusCode != 200 {
		t.Errorf("revoke = %d", resp.StatusCode)
	}
	if resp, _ := e.do("GET", "/api/me/user", "", withCookie(c2)); resp.StatusCode != 401 {
		t.Errorf("revoked session still works: %d", resp.StatusCode)
	}

	resp, _ = e.do("POST", "/api/auth/logout", "", withCookie(c), withCSRF())
	if cleared := cookieNamed(resp, sessionCookie); resp.StatusCode != 200 || cleared == nil || cleared.MaxAge >= 0 {
		t.Errorf("logout = %d, cookie %+v", resp.StatusCode, cleared)
	}
	if resp, _ := e.do("GET", "/api/me/user", "", withCookie(c)); resp.StatusCode != 401 {
		t.Errorf("after logout me = %d", resp.StatusCode)
	}

	// Bearer user tokens are not cookies: no CSRF header needed.
	u, _ := e.st.LoginWithProvider(t.Context(), "github", store.Profile{Subject: "9"})
	token, _, _ := e.st.CreateSession(t.Context(), u.ID, store.SessionCLI, "cli")
	if resp, _ := e.do("POST", "/api/auth/logout", "", withBearer(token)); resp.StatusCode != 200 {
		t.Errorf("bearer logout = %d", resp.StatusCode)
	}
	// Participant tokens are never user sessions.
	ws, _, _ := e.st.CreateWorkspace(t.Context(), "W", "", "")
	_, ptoken, _, _ := joinMember(t, e.st, ws.ID, "Ada", "")
	if resp, _ := e.do("GET", "/api/me/user", "", withBearer(ptoken)); resp.StatusCode != 401 {
		t.Errorf("participant token as user = %d", resp.StatusCode)
	}
	if resp, _ := e.do("GET", "/api/workspaces/"+ws.ID+"/me", "", withBearer(ptoken)); resp.StatusCode != 200 {
		t.Errorf("participant /me = %d (must keep working)", resp.StatusCode)
	}
}

func jsonID(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestDeviceFlow(t *testing.T) {
	e := newAuthEnv(t)
	c, _ := e.login("github", "/", adaGitHub, adaGitHubEmails)
	start := func() (deviceCode, userCode string) {
		resp, d := e.do("POST", "/api/auth/device", "")
		if resp.StatusCode != 200 || d["verification_url"] != e.ts.URL+"/auth/device" || d["interval"] != 1.0 || d["expires_in"] != 600.0 {
			t.Fatalf("device start = %d %v", resp.StatusCode, d)
		}
		userCode = d["user_code"].(string)
		if d["verification_url_complete"] != e.ts.URL+"/auth/device?code="+userCode {
			t.Errorf("verification_url_complete = %v", d["verification_url_complete"])
		}
		return d["device_code"].(string), userCode
	}
	poll := func(deviceCode string) (string, string) {
		_, out := e.do("POST", "/api/auth/device/token", `{"device_code":"`+deviceCode+`","label":"test laptop"}`)
		tok, _ := out["token"].(string)
		return out["status"].(string), tok
	}

	dc, uc := start()
	if st, _ := poll(dc); st != "pending" {
		t.Errorf("first poll = %s", st)
	}
	if st, _ := poll(dc); st != "slow_down" {
		t.Errorf("fast poll = %s, want slow_down", st)
	}
	if resp, _ := e.do("POST", "/api/auth/device/approve", `{"user_code":"`+uc+`","approve":true}`); resp.StatusCode != 401 {
		t.Errorf("anonymous approve = %d", resp.StatusCode)
	}
	if resp, _ := e.do("POST", "/api/auth/device/approve", `{"user_code":"`+uc+`","approve":true}`, withCookie(c)); resp.StatusCode != 403 {
		t.Errorf("approve without CSRF header = %d", resp.StatusCode)
	}
	resp, look := e.do("GET", "/api/auth/device/lookup?code="+strings.ToLower(strings.ReplaceAll(uc, "-", "")), "", withCookie(c))
	if resp.StatusCode != 200 || look["pending"] != true || look["user_code"] != uc {
		t.Errorf("lookup = %d %v", resp.StatusCode, look)
	}
	if resp, _ := e.do("POST", "/api/auth/device/approve", `{"user_code":"`+uc+`","approve":true}`, withCookie(c), withCSRF()); resp.StatusCode != 200 {
		t.Fatalf("approve = %d", resp.StatusCode)
	}
	time.Sleep(1100 * time.Millisecond)
	st, token := poll(dc)
	if st != "approved" || !strings.HasPrefix(token, store.UserTokenPrefix) {
		t.Fatalf("approved poll = %s %q", st, token)
	}
	if resp, u := e.do("GET", "/api/me/user", "", withBearer(token)); resp.StatusCode != 200 || u["display_name"] != "Ada Lovelace" {
		t.Errorf("CLI token me = %d %v", resp.StatusCode, u)
	}
	if st, tok := poll(dc); st != "expired" || tok != "" {
		t.Errorf("poll after the token was issued = %s %q (single use)", st, tok)
	}
	_, sessions := e.doList("GET", "/api/me/sessions", withBearer(token))
	if len(sessions) != 2 || sessions[0]["kind"] != "cli" || sessions[0]["label"] != "test laptop" || sessions[0]["current"] != true {
		t.Errorf("sessions after device login = %v", sessions)
	}

	dc, uc = start()
	e.do("POST", "/api/auth/device/approve", `{"user_code":"`+uc+`","approve":false}`, withCookie(c), withCSRF())
	if st, _ := poll(dc); st != "denied" {
		t.Errorf("denied poll = %s", st)
	}
	if resp, look := e.do("GET", "/api/auth/device/lookup?code="+uc, "", withCookie(c)); resp.StatusCode != 200 || look["pending"] != false {
		t.Errorf("lookup of a decided code = %v", look)
	}

	dc, uc = start()
	e.st.SetClock(func() time.Time { return time.Now().Add(store.DeviceCodeTTL + time.Minute) })
	if st, _ := poll(dc); st != "expired" {
		t.Errorf("late poll = %s", st)
	}
	if resp, _ := e.do("POST", "/api/auth/device/approve", `{"user_code":"`+uc+`","approve":true}`, withCookie(c), withCSRF()); resp.StatusCode != 404 {
		t.Errorf("approving an expired code = %d, want 404", resp.StatusCode)
	}
}

func TestCreatorsAllowlist(t *testing.T) {
	const body = `{"name":"W"}`
	// Without UNISON_CREATORS any signed-in user may create; nobody else.
	open := newAuthEnv(t)
	if resp, _ := open.do("POST", "/api/workspaces", body); resp.StatusCode != 401 {
		t.Errorf("anonymous create = %d, want 401", resp.StatusCode)
	}
	anyone, _ := open.login("google", "/", bobGoogle, nil)
	if resp, _ := open.do("POST", "/api/workspaces", body, withCookie(anyone), withCSRF()); resp.StatusCode != 201 {
		t.Errorf("signed-in create without UNISON_CREATORS = %d, want 201", resp.StatusCode)
	}

	e := newAuthEnv(t, "Ada@example.com")
	ada, _ := e.login("google", "/", adaGoogle, nil)
	bob, _ := e.login("google", "/", bobGoogle, nil)
	for name, tc := range map[string]struct {
		opts []reqOpt
		want int
	}{
		"anonymous":         {nil, 401},
		"ada without CSRF":  {[]reqOpt{withCookie(ada)}, 403},
		"bob (not listed)":  {[]reqOpt{withCookie(bob), withCSRF()}, 403},
		"ada":               {[]reqOpt{withCookie(ada), withCSRF()}, 201},
		"participant token": {[]reqOpt{withBearer("0123456789abcdef0123456789abcdef")}, 401},
	} {
		if resp, out := e.do("POST", "/api/workspaces", body, tc.opts...); resp.StatusCode != tc.want {
			t.Errorf("%s: create = %d %v, want %d", name, resp.StatusCode, out, tc.want)
		}
	}
	// An unverified email on the list does not count.
	u, _ := e.st.LoginWithProvider(t.Context(), "github", store.Profile{Subject: "x", Email: "ada@example.com"})
	token, _, _ := e.st.CreateSession(t.Context(), u.ID, store.SessionCLI, "")
	if resp, _ := e.do("POST", "/api/workspaces", body, withBearer(token)); resp.StatusCode != 403 {
		t.Errorf("unverified listed email: create = %d, want 403", resp.StatusCode)
	}
}
