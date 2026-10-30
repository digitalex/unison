package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeApple is Apple's token endpoint: it checks the signed client secret
// and answers a code with an id_token carrying the claims registered for it.
type fakeApple struct {
	t      *testing.T
	ts     *httptest.Server
	pub    *ecdsa.PublicKey
	claims map[string]map[string]any // by code
}

func newFakeApple(t *testing.T, pub *ecdsa.PublicKey) *fakeApple {
	f := &fakeApple{t: t, pub: pub, claims: map[string]map[string]any{}}
	f.ts = httptest.NewServer(http.HandlerFunc(f.token))
	t.Cleanup(f.ts.Close)
	return f
}

func (f *fakeApple) token(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	clientID := r.PostForm.Get("client_id")
	if err := f.checkSecret(r.PostForm.Get("client_secret"), clientID); err != "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client: " + err})
		return
	}
	claims, ok := f.claims[r.PostForm.Get("code")]
	if !ok || r.PostForm.Get("code_verifier") != "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
		return
	}
	b, _ := json.Marshal(claims)
	idToken := "eyJhbGciOiJSUzI1NiJ9." + base64.RawURLEncoding.EncodeToString(b) + ".sig"
	json.NewEncoder(w).Encode(map[string]string{"access_token": "at", "id_token": idToken})
}

// checkSecret verifies the client secret JWT, returning what's wrong.
func (f *fakeApple) checkSecret(jwt, clientID string) string {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return "not a JWT"
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	h := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if len(sig) != 64 || !ecdsa.Verify(f.pub, h[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		return "bad signature"
	}
	var hdr, c map[string]any
	hb, _ := base64.RawURLEncoding.DecodeString(parts[0])
	cb, _ := base64.RawURLEncoding.DecodeString(parts[1])
	json.Unmarshal(hb, &hdr)
	json.Unmarshal(cb, &c)
	exp, _ := c["exp"].(float64)
	switch {
	case hdr["alg"] != "ES256" || hdr["kid"] != "KEY123":
		return "bad header"
	case c["iss"] != "TEAM123" || c["aud"] != appleIssuer || c["sub"] != clientID:
		return "bad claims"
	case time.Unix(int64(exp), 0).Before(time.Now()):
		return "expired"
	}
	return ""
}

func newAppleEnv(t *testing.T) (*authEnv, *fakeApple) {
	t.Helper()
	e := newAuthEnv(t)
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(k)
	// As an env var would hold it: newlines escaped.
	keyPEM := strings.ReplaceAll(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), "\n", `\n`)
	p, err := AppleProvider("com.example.web", "TEAM123", "KEY123", keyPEM, "com.example.app")
	if err != nil {
		t.Fatal(err)
	}
	fa := newFakeApple(t, &k.PublicKey)
	p.TokenURL = fa.ts.URL
	e.srv.SetAuth(AuthConfig{Providers: append(e.fake.providers(), p), DeviceInterval: time.Second})
	return e, fa
}

func TestAppleProviderRejectsBadKeys(t *testing.T) {
	if _, err := AppleProvider("id", "team", "kid", "not a key", ""); err == nil {
		t.Error("a non-PEM key was accepted")
	}
}

func TestAppleLogin(t *testing.T) {
	e, fa := newAppleEnv(t)
	adaClaims := map[string]any{"iss": appleIssuer, "aud": "com.example.web", "sub": "apple-ada", "email": "ada@example.com", "email_verified": "true"}

	// login runs the browser flow: start, then Apple's form POST back.
	login := func(code, user string) (*http.Response, map[string]any) {
		t.Helper()
		resp, _ := e.do("GET", "/auth/apple/start?return_to=/w/x", "", withHTTPS())
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("start = %d", resp.StatusCode)
		}
		loc, _ := url.Parse(resp.Header.Get("Location"))
		q := loc.Query()
		if q.Get("response_mode") != "form_post" || q.Get("code_challenge") != "" || q.Get("scope") != "name email" {
			t.Errorf("authorize query = %v", q)
		}
		state := cookieNamed(resp, oauthCookie)
		if state == nil || state.SameSite != http.SameSiteNoneMode || !state.Secure {
			t.Fatalf("state cookie = %+v, want SameSite=None; Secure", state)
		}
		form := url.Values{"code": {code}, "state": {q.Get("state")}}
		if user != "" {
			form.Set("user", user)
		}
		return e.do("POST", "/auth/apple/callback", form.Encode(), withHTTPS(), withCookie(state),
			withHeader("Content-Type", "application/x-www-form-urlencoded"))
	}

	// First sign-in: Apple sends the name, once.
	fa.claims["c1"] = adaClaims
	resp, _ := login("c1", `{"name":{"firstName":"Ada","lastName":"Lovelace"},"email":"ada@example.com"}`)
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/w/x" {
		t.Fatalf("callback = %d → %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	c := cookieNamed(resp, sessionCookie)
	if c == nil {
		t.Fatal("no session cookie")
	}
	_, me := e.do("GET", "/api/me/user", "", withCookie(c))
	if me["display_name"] != "Ada Lovelace" {
		t.Errorf("display_name = %v", me["display_name"])
	}
	ids, _ := me["identities"].([]any)
	if len(ids) != 1 || ids[0].(map[string]any)["provider"] != "apple" || ids[0].(map[string]any)["email_verified"] != true {
		t.Errorf("identities = %v", ids)
	}

	// Later sign-ins have no name, and keep the one we have.
	fa.claims["c2"] = adaClaims
	resp, _ = login("c2", "")
	_, me = e.do("GET", "/api/me/user", "", withCookie(cookieNamed(resp, sessionCookie)))
	if me["display_name"] != "Ada Lovelace" {
		t.Errorf("after a second sign-in, display_name = %v", me["display_name"])
	}

	// An id_token for another client is refused.
	fa.claims["c3"] = map[string]any{"iss": appleIssuer, "aud": "com.other", "sub": "apple-eve"}
	if resp, _ := login("c3", ""); resp.StatusCode != http.StatusBadGateway {
		t.Errorf("foreign audience: callback = %d", resp.StatusCode)
	}
	// So is an unknown code, and a callback without the state cookie.
	if resp, _ := login("nope", ""); resp.StatusCode != http.StatusBadGateway {
		t.Errorf("unknown code: callback = %d", resp.StatusCode)
	}
	fa.claims["c4"] = adaClaims
	resp, _ = e.do("GET", "/auth/apple/start", "", withHTTPS())
	loc, _ := url.Parse(resp.Header.Get("Location"))
	resp, _ = e.do("POST", "/auth/apple/callback", url.Values{"code": {"c4"}, "state": {loc.Query().Get("state")}}.Encode(),
		withHTTPS(), withHeader("Content-Type", "application/x-www-form-urlencoded"))
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("no state cookie: callback = %d", resp.StatusCode)
	}

	// It's listed with the other providers.
	_, list := e.doList("GET", "/api/auth/providers")
	if len(list) != 3 || list[2]["name"] != "apple" || list[2]["label"] != "Apple" {
		t.Errorf("providers = %v", list)
	}
}

func TestAppleNative(t *testing.T) {
	e, fa := newAppleEnv(t)
	// The app's codes are for the App ID, not the Services ID.
	fa.claims["n1"] = map[string]any{"iss": appleIssuer, "aud": "com.example.app", "sub": "apple-ada", "email": "x@privaterelay.appleid.com", "email_verified": true}
	resp, out := e.do("POST", "/api/auth/apple/native", `{"code":"n1","given_name":"Ada","family_name":"Lovelace","label":"iPhone"}`)
	tok, _ := out["token"].(string)
	if resp.StatusCode != http.StatusOK || tok == "" {
		t.Fatalf("native = %d %v", resp.StatusCode, out)
	}
	_, me := e.do("GET", "/api/me/user", "", withBearer(tok))
	if me["display_name"] != "Ada Lovelace" {
		t.Errorf("display_name = %v", me["display_name"])
	}
	_, sessions := e.doList("GET", "/api/me/sessions", withBearer(tok))
	if len(sessions) != 1 || sessions[0]["kind"] != "app" || sessions[0]["label"] != "iPhone" {
		t.Errorf("sessions = %v", sessions)
	}

	// The same Apple ID on the web is the same account.
	fa.claims["w1"] = map[string]any{"iss": appleIssuer, "aud": "com.example.web", "sub": "apple-ada"}
	resp, _ = e.do("GET", "/auth/apple/start", "", withHTTPS())
	loc, _ := url.Parse(resp.Header.Get("Location"))
	resp, _ = e.do("POST", "/auth/apple/callback", url.Values{"code": {"w1"}, "state": {loc.Query().Get("state")}}.Encode(),
		withHTTPS(), withCookie(cookieNamed(resp, oauthCookie)), withHeader("Content-Type", "application/x-www-form-urlencoded"))
	_, web := e.do("GET", "/api/me/user", "", withCookie(cookieNamed(resp, sessionCookie)))
	if web["id"] == nil || web["id"] != me["id"] {
		t.Errorf("web account %v, app account %v", web["id"], me["id"])
	}

	// A web code (Services ID audience) can't be redeemed by the app.
	fa.claims["n2"] = map[string]any{"iss": appleIssuer, "aud": "com.example.web", "sub": "apple-eve"}
	if resp, _ := e.do("POST", "/api/auth/apple/native", `{"code":"n2"}`); resp.StatusCode != http.StatusBadGateway {
		t.Errorf("web code redeemed natively: %d", resp.StatusCode)
	}
	// Without Apple configured, it's not found.
	e.srv.SetAuth(AuthConfig{Providers: e.fake.providers()})
	if resp, _ := e.do("POST", "/api/auth/apple/native", `{"code":"n1"}`); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unconfigured = %d", resp.StatusCode)
	}
}
