package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitalex/unison/internal/push"
	"github.com/digitalex/unison/internal/store"
	"github.com/digitalex/unison/internal/store/storetest"
)

const (
	adaPhone  = "a0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf"
	adaTablet = "c0c1c2c3c4c5c6c7c8c9cacbcccdcecfd0d1d2d3d4d5d6d7d8d9dadbdcdddedf"
	bobPhone  = "e0e1e2e3e4e5e6e7e8e9eaebecedeeeff0f1f2f3f4f5f6f7f8f9fafbfcfdfeff"
)

type sent struct {
	token string
	n     push.Notification
}

// fakePusher records what it is asked to send.
type fakePusher struct {
	mu   sync.Mutex
	sent []sent
}

func (f *fakePusher) Send(_ context.Context, token string, n push.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sent{token, n})
	return nil
}

// take returns and clears what was sent, sorted by token.
func (f *fakePusher) take() []sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.sent
	f.sent = nil
	slices.SortFunc(out, func(a, b sent) int { return strings.Compare(a.token, b.token) })
	return out
}

// pushEnv: workspace W with members Ada (phone and tablet registered) and
// Bob (phone), and Bob's agent Wren.
type pushEnv struct {
	t                *testing.T
	ts               *httptest.Server
	st               *store.Store
	srv              *Server
	ws               string
	ada, bob, wren   *store.Participant
	adaTok, bobTok   string // participant tokens
	wrenTok          string
	adaUser, bobUser string // account (us_) tokens
}

func newPushEnv(t *testing.T, pusher push.Sender) *pushEnv {
	t.Helper()
	ctx := t.Context()
	st, err := store.Open(storetest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := New(st, nil)
	srv.SetPusher(pusher)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	e := &pushEnv{t: t, ts: ts, st: st, srv: srv}
	ws, _, _ := st.CreateWorkspace(ctx, "W", "", "")
	e.ws = ws.ID
	member := func(name string) (*store.Participant, string, string, int64) {
		u, err := st.LoginWithProvider(ctx, "test", store.Profile{Subject: name, Name: name})
		if err != nil {
			t.Fatal(err)
		}
		p, tok, _, _, err := st.JoinAsUser(ctx, ws.ID, u.ID, name, "")
		if err != nil {
			t.Fatal(err)
		}
		userTok, _, err := st.CreateSession(ctx, u.ID, store.SessionCLI, "app")
		if err != nil {
			t.Fatal(err)
		}
		return p, tok, userTok, u.ID
	}
	var bobID int64
	e.ada, e.adaTok, e.adaUser, _ = member("Ada")
	e.bob, e.bobTok, e.bobUser, bobID = member("Bob")
	pass, _, _ := st.MintPass(ctx, ws.ID, bobID)
	if e.wren, e.wrenTok, _, err = st.JoinWithPass(ctx, ws.ID, pass, "Wren", ""); err != nil {
		t.Fatal(err)
	}
	e.register(e.adaUser, adaPhone, "Ada's iPhone")
	e.register(e.adaUser, adaTablet, "Ada's iPad")
	e.register(e.bobUser, bobPhone, "Bob's iPhone")
	return e
}

func (e *pushEnv) register(userTok, apnsToken, label string) map[string]any {
	e.t.Helper()
	code, d := call(e.t, "POST", e.ts.URL+"/api/me/devices", userTok, fmt.Sprintf(`{"apns_token":%q,"label":%q}`, apnsToken, label))
	if code != http.StatusCreated {
		e.t.Fatalf("register = %d %v", code, d)
	}
	return d
}

// post calls a workspace endpoint and waits for its pushes.
func (e *pushEnv) post(path, token, body string) map[string]any {
	e.t.Helper()
	code, out := call(e.t, "POST", e.ts.URL+"/api/workspaces/"+e.ws+path, token, body)
	if code != http.StatusCreated && code != http.StatusOK {
		e.t.Fatalf("POST %s = %d %v", path, code, out)
	}
	e.srv.waitPushes()
	return out
}

func id(v any) int64 { return int64(v.(float64)) }

func TestDeviceAPI(t *testing.T) {
	e := newPushEnv(t, nil)
	url := e.ts.URL + "/api/me/devices"

	if code, _ := call(t, "GET", url, "", ""); code != http.StatusUnauthorized {
		t.Errorf("signed out = %d, want 401", code)
	}
	for _, m := range []string{"GET", "POST", "DELETE"} {
		u := url
		if m == "DELETE" {
			u += "/1"
		}
		if code, _ := call(t, m, u, e.wrenTok, `{"apns_token":"`+adaPhone+`"}`); code != http.StatusForbidden {
			t.Errorf("agent %s = %d, want 403", m, code)
		}
		if code, _ := call(t, m, u, e.adaTok, `{"apns_token":"`+adaPhone+`"}`); code != http.StatusForbidden {
			t.Errorf("member participant token %s = %d, want 403", m, code)
		}
	}

	d := e.register(e.adaUser, adaPhone, "Ada's iPhone again")
	if d["apns_token"] != adaPhone || d["label"] != "Ada's iPhone again" || d["created_at"] == nil || d["id"] == nil {
		t.Errorf("registered device = %v", d)
	}
	if code, body := call(t, "POST", url, e.adaUser, `{"apns_token":"zz"}`); code != http.StatusBadRequest {
		t.Errorf("bad token = %d %v", code, body)
	}
	var list []store.Device
	_, raw := getAs(t, url, e.adaUser)
	json.Unmarshal([]byte(raw), &list)
	if len(list) != 2 || list[0].APNSToken != adaPhone || list[1].APNSToken != adaTablet {
		t.Fatalf("ada's devices = %+v", list)
	}

	bobs, _ := e.st.ListDevices(t.Context(), e.bob.UserID)
	if code, _ := call(t, "DELETE", fmt.Sprintf("%s/%d", url, bobs[0].ID), e.adaUser, ""); code != http.StatusNotFound {
		t.Errorf("deleting bob's device = %d, want 404", code)
	}
	if code, _ := call(t, "DELETE", fmt.Sprintf("%s/%d", url, list[1].ID), e.adaUser, ""); code != http.StatusOK {
		t.Errorf("deleting own device = %d", code)
	}

	// Registering Bob's token from Ada's account moves it to Ada.
	e.register(e.adaUser, bobPhone, "handed down")
	if bobs, _ := e.st.ListDevices(t.Context(), e.bob.UserID); len(bobs) != 0 {
		t.Errorf("bob still has %+v", bobs)
	}

	// Signing out removes the devices that session registered.
	if code, _ := call(t, "POST", e.ts.URL+"/api/auth/logout", e.adaUser, ""); code != http.StatusOK {
		t.Fatalf("logout = %d", code)
	}
	if left, _ := e.st.ListDevices(t.Context(), e.ada.UserID); len(left) != 0 {
		t.Errorf("after sign-out ada has %+v", left)
	}
}

// TestDeviceAPICookie: the web session works too, with the CSRF header on
// writes.
func TestDeviceAPICookie(t *testing.T) {
	e := newAuthEnv(t)
	c, _ := e.login("github", "/", adaGitHub, adaGitHubEmails)
	body := `{"apns_token":"` + adaPhone + `","label":"web"}`
	if resp, _ := e.do("POST", "/api/me/devices", body, withCookie(c)); resp.StatusCode != http.StatusForbidden {
		t.Errorf("without CSRF = %d, want 403", resp.StatusCode)
	}
	resp, d := e.do("POST", "/api/me/devices", body, withCookie(c), withCSRF())
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("with CSRF = %d %v", resp.StatusCode, d)
	}
	if resp, _ := e.do("DELETE", fmt.Sprintf("/api/me/devices/%d", id(d["id"])), "", withCookie(c), withCSRF()); resp.StatusCode != http.StatusOK {
		t.Errorf("delete = %d", resp.StatusCode)
	}
}

func TestPushTriggers(t *testing.T) {
	fake := &fakePusher{}
	e := newPushEnv(t, fake)
	adaBoth := []string{adaPhone, adaTablet}
	expect := func(what string, wantTokens []string, want push.Notification) {
		t.Helper()
		got := fake.take()
		var tokens []string
		for _, s := range got {
			tokens = append(tokens, s.token)
			if s.n != want {
				t.Errorf("%s: sent %+v, want %+v", what, s.n, want)
			}
		}
		if !slices.Equal(tokens, wantTokens) {
			t.Errorf("%s: sent to %v, want %v", what, tokens, wantTokens)
		}
	}

	m := e.post("/messages", e.bobTok, `{"body":"hi @ada, see this"}`)
	expect("mention", adaBoth, push.Notification{Title: "Bob mentioned you", Body: "hi @ada, see this",
		WorkspaceID: e.ws, MessageID: id(m["id"]), Kind: "mention"})

	a := e.post("/messages", e.bobTok, `{"body":"@Ada please review","action_required":true}`)
	expect("action (and mention): one push per device", adaBoth, push.Notification{Title: "Bob needs you", Body: "@Ada please review",
		WorkspaceID: e.ws, MessageID: id(a["id"]), Kind: "action"})

	item := e.post("/backlog", e.adaTok, `{"title":"Ship push"}`)
	r := e.post("/messages", e.adaTok, fmt.Sprintf(`{"body":"on it @Bob","parent_id":%d,"item_id":%d}`, id(a["id"]), id(item["id"])))
	expect("reply about an item", []string{bobPhone}, push.Notification{Title: "Ada mentioned you", Body: "on it @Bob",
		WorkspaceID: e.ws, MessageID: id(r["id"]), ParentID: id(a["id"]), ItemID: id(item["id"]), Kind: "mention"})

	c := e.post(fmt.Sprintf("/backlog/%d/comments", id(item["id"])), e.bobTok, `{"body":"@Ada @Bob ok"}`)
	expect("comment mention, not the author", adaBoth, push.Notification{Title: "Bob mentioned you", Body: "@Ada @Bob ok",
		WorkspaceID: e.ws, ItemID: id(item["id"]), CommentID: id(c["id"]), Kind: "comment_mention"})

	e.post("/messages", e.adaTok, `{"body":"note to self @Ada"}`)
	expect("own mention", nil, push.Notification{})
	e.post("/messages", e.bobTok, `{"body":"no mentions"}`)
	expect("no mention", nil, push.Notification{})

	w := e.post("/messages", e.wrenTok, `{"body":"@Ada @Bob done"}`)
	expect("agent author: its sponsor is notified too", []string{adaPhone, adaTablet, bobPhone}, push.Notification{Title: "Wren mentioned you",
		Body: "@Ada @Bob done", WorkspaceID: e.ws, MessageID: id(w["id"]), Kind: "mention"})

	long := strings.Repeat("é", 300)
	e.post("/messages", e.bobTok, `{"body":"@Ada `+long+`"}`)
	if got := fake.take(); len(got) != 2 || len([]rune(got[0].n.Body)) != pushBodyRunes || !strings.HasSuffix(got[0].n.Body, "…") {
		t.Errorf("long body sent as %q", got[0].n.Body)
	}

	if _, _, _, err := joinMember(t, e.st, e.ws, "Cy", ""); err != nil {
		t.Fatal(err)
	}
	e.srv.waitPushes()
	expect("join", nil, push.Notification{})
}

// TestPushPrunesUnregistered: with the real APNs client against a fake
// APNs, a device APNs reports gone (410) is forgotten; one that takes the
// push is marked used.
func TestPushPrunesUnregistered(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	apns := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		if strings.HasSuffix(r.URL.Path, adaPhone) {
			w.WriteHeader(http.StatusGone)
			w.Write([]byte(`{"reason":"Unregistered"}`))
		}
	}))
	apns.EnableHTTP2 = true
	apns.StartTLS()
	t.Cleanup(apns.Close)
	client, err := push.NewClient(push.Config{KeyPEM: testAPNsKey(t), KeyID: "K", TeamID: "T", Topic: "app"})
	if err != nil {
		t.Fatal(err)
	}
	client.BaseURL, client.HTTP = apns.URL, apns.Client()
	e := newPushEnv(t, client)

	e.post("/messages", e.bobTok, `{"body":"@Ada ping"}`)
	if len(paths) != 2 {
		t.Fatalf("APNs got %v", paths)
	}
	left, _ := e.st.ListDevices(t.Context(), e.ada.UserID)
	if len(left) != 1 || left[0].APNSToken != adaTablet || left[0].LastUsedAt == 0 {
		t.Errorf("ada's devices after a 410 = %+v, want only the tablet, marked used", left)
	}
}

func testAPNsKey(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

type slowPusher struct {
	delay time.Duration
	fake  fakePusher
}

func (s *slowPusher) Send(ctx context.Context, token string, n push.Notification) error {
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.fake.Send(ctx, token, n)
}

func TestConcurrentPush(t *testing.T) {
	slow := &slowPusher{delay: 100 * time.Millisecond}
	e := newPushEnv(t, slow)

	dev3 := "d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3"
	dev4 := "d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4"
	e.register(e.adaUser, dev3, "Ada's Watch")
	e.register(e.adaUser, dev4, "Ada's Mac")

	start := time.Now()
	m := e.post("/messages", e.bobTok, `{"body":"hi @ada, concurrent test"}`)
	duration := time.Since(start)

	_ = m
	got := slow.fake.take()
	if len(got) != 4 {
		t.Fatalf("expected 4 pushes, got %d", len(got))
	}

	if duration >= 300*time.Millisecond {
		t.Errorf("delivery took %v, expected < 300ms (indicating concurrent execution)", duration)
	}
}
