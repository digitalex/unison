package push

import (
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
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const deviceToken = "a0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf"

func testKey(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return key, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

// verifyJWT checks an ES256 token's signature with pub and returns its
// header and claims.
func verifyJWT(t *testing.T, jwt string, pub *ecdsa.PublicKey) (header, claims map[string]any) {
	t.Helper()
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt has %d parts", len(parts))
	}
	enc := base64.RawURLEncoding
	sig, err := enc.DecodeString(parts[2])
	if err != nil || len(sig) != 64 {
		t.Fatalf("signature: %d bytes, %v", len(sig), err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(pub, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("signature does not verify")
	}
	for i, out := range []*map[string]any{&header, &claims} {
		raw, err := enc.DecodeString(parts[i])
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatal(err)
		}
	}
	return header, claims
}

func TestJWT(t *testing.T) {
	key, keyPEM := testKey(t)
	c, err := NewClient(Config{KeyPEM: keyPEM, KeyID: "KEY123", TeamID: "TEAM456", Topic: "app"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	c.now = func() time.Time { return now }
	jwt, err := c.token()
	if err != nil {
		t.Fatal(err)
	}
	header, claims := verifyJWT(t, jwt, &key.PublicKey)
	if header["alg"] != "ES256" || header["kid"] != "KEY123" || claims["iss"] != "TEAM456" || claims["iat"] != float64(now.Unix()) {
		t.Errorf("header %v claims %v", header, claims)
	}

	now = now.Add(49 * time.Minute)
	if again, _ := c.token(); again != jwt {
		t.Error("token was not reused within 50 minutes")
	}
	now = now.Add(2 * time.Minute)
	if again, _ := c.token(); again == jwt {
		t.Error("token was not refreshed after 50 minutes")
	}
}

func TestNewClientRejectsBadKeys(t *testing.T) {
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(p384)
	p384PEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	for _, k := range []string{"", "not a pem", "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n", p384PEM} {
		if _, err := NewClient(Config{KeyPEM: k}); err == nil {
			t.Errorf("NewClient(%q) succeeded", k)
		}
	}
}

func TestConfigFromEnv(t *testing.T) {
	env := map[string]string{"APNS_KEY": "k", "APNS_KEY_ID": "i", "APNS_TEAM_ID": "t", "APNS_TOPIC": "app", "APNS_SANDBOX": "1"}
	cfg, ok := ConfigFromEnv(func(k string) string { return env[k] })
	if !ok || !cfg.Sandbox || cfg.Topic != "app" {
		t.Errorf("ConfigFromEnv = %+v, %v", cfg, ok)
	}
	delete(env, "APNS_TOPIC")
	if _, ok := ConfigFromEnv(func(k string) string { return env[k] }); ok {
		t.Error("enabled without APNS_TOPIC")
	}
}

func TestPayload(t *testing.T) {
	raw, err := Notification{Title: "Ada needs you", Body: "review #12", WorkspaceID: "ws1", MessageID: 7, ParentID: 3, Kind: "action"}.Payload()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"aps":{"alert":{"body":"review #12","title":"Ada needs you"},"sound":"default","thread-id":"ws1"},"kind":"action","message_id":7,"parent_id":3,"workspace_id":"ws1"}`
	if string(raw) != want {
		t.Errorf("payload\n got %s\nwant %s", raw, want)
	}
}

// apnsServer is a fake APNs endpoint answering with status and reason.
func apnsServer(t *testing.T, status int, reason string, got *http.Request, body *[]byte) (*httptest.Server, *Client, *ecdsa.PrivateKey) {
	t.Helper()
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*got = *r.Clone(context.Background())
		*body, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
		if reason != "" {
			json.NewEncoder(w).Encode(map[string]string{"reason": reason})
		}
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	t.Cleanup(ts.Close)
	key, keyPEM := testKey(t)
	c, err := NewClient(Config{KeyPEM: keyPEM, KeyID: "K", TeamID: "T", Topic: "com.example.unison", Sandbox: true})
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != SandboxURL {
		t.Errorf("sandbox BaseURL = %s", c.BaseURL)
	}
	c.BaseURL, c.HTTP = ts.URL, ts.Client()
	return ts, c, key
}

func TestSend(t *testing.T) {
	var got http.Request
	var body []byte
	_, c, key := apnsServer(t, http.StatusOK, "", &got, &body)
	n := Notification{Title: "t", Body: "b", WorkspaceID: "ws1", ItemID: 4, CommentID: 9, Kind: "comment_mention"}
	if err := c.Send(context.Background(), deviceToken, n); err != nil {
		t.Fatal(err)
	}
	if got.Method != http.MethodPost || got.URL.Path != "/3/device/"+deviceToken || got.ProtoMajor != 2 {
		t.Errorf("request %s %s %s", got.Method, got.URL.Path, got.Proto)
	}
	if got.Header.Get("apns-topic") != "com.example.unison" || got.Header.Get("apns-push-type") != "alert" {
		t.Errorf("headers %v", got.Header)
	}
	jwt, ok := strings.CutPrefix(got.Header.Get("authorization"), "bearer ")
	if !ok {
		t.Fatalf("authorization = %q", got.Header.Get("authorization"))
	}
	verifyJWT(t, jwt, &key.PublicKey)
	if want, _ := n.Payload(); string(body) != string(want) {
		t.Errorf("body %s, want %s", body, want)
	}
}

func TestSendErrors(t *testing.T) {
	for _, tc := range []struct {
		status       int
		reason       string
		unregistered bool
	}{
		{http.StatusGone, "Unregistered", true},
		{http.StatusBadRequest, "BadDeviceToken", true},
		{http.StatusBadRequest, "DeviceTokenNotForTopic", true},
		{http.StatusBadRequest, "PayloadTooLarge", false},
		{http.StatusTooManyRequests, "TooManyRequests", false},
		{http.StatusForbidden, "ExpiredProviderToken", false},
	} {
		var got http.Request
		var body []byte
		_, c, _ := apnsServer(t, tc.status, tc.reason, &got, &body)
		err := c.Send(context.Background(), deviceToken, Notification{Kind: "mention"})
		if err == nil || errors.Is(err, ErrUnregistered) != tc.unregistered {
			t.Errorf("%d %s: err = %v, want unregistered=%v", tc.status, tc.reason, err, tc.unregistered)
		}
		if err != nil && strings.Contains(err.Error(), deviceToken) {
			t.Errorf("error leaks the device token: %v", err)
		}
		if tc.reason == "ExpiredProviderToken" && c.jwt != "" {
			t.Error("ExpiredProviderToken did not drop the cached token")
		}
	}
}

// TestSendNetworkErrorHidesToken: a transport error must not quote the
// request URL (which contains the token).
func TestSendNetworkErrorHidesToken(t *testing.T) {
	_, keyPEM := testKey(t)
	c, _ := NewClient(Config{KeyPEM: keyPEM, KeyID: "K", TeamID: "T", Topic: "app"})
	c.BaseURL = "https://127.0.0.1:1"
	err := c.Send(context.Background(), deviceToken, Notification{})
	if err == nil || strings.Contains(err.Error(), deviceToken) {
		t.Errorf("err = %v", err)
	}
}
