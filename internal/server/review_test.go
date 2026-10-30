package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/digitalex/unison/internal/store"
	"github.com/digitalex/unison/internal/store/storetest"
)

func TestReviewSignIn(t *testing.T) {
	st, err := store.Open(storetest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := New(st, fstest.MapFS{"index.html": {Data: []byte("x")}})
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	url := ts.URL + "/api/auth/review"

	// Off without a code.
	if code, _ := call(t, "POST", url, "", `{"code":""}`); code != http.StatusNotFound {
		t.Errorf("without UNISON_REVIEW_CODE = %d, want 404", code)
	}

	srv.SetAuth(AuthConfig{ReviewCode: "open-sesame-123"})
	srv.reviewFails.retry = 200 * time.Millisecond
	if code, _ := call(t, "POST", url, "", `{"code":"guess"}`); code != http.StatusUnauthorized {
		t.Errorf("wrong code = %d, want 401", code)
	}
	if code, _ := call(t, "POST", url, "", `{"code":"open-sesame-123"}`); code != http.StatusTooManyRequests {
		t.Errorf("right code right after a wrong one = %d, want 429", code)
	}
	time.Sleep(250 * time.Millisecond)

	signIn := func() string {
		t.Helper()
		code, out := call(t, "POST", url, "", `{"code":" open-sesame-123 ","label":"Review iPhone"}`)
		tok, _ := out["token"].(string)
		if code != http.StatusOK || !strings.HasPrefix(tok, store.UserTokenPrefix) {
			t.Fatalf("right code = %d %v", code, out)
		}
		return tok
	}
	tok := signIn()
	if code, me := call(t, "GET", ts.URL+"/api/me/user", tok, ""); code != http.StatusOK || me["display_name"] != "App Review" {
		t.Errorf("me = %d %v", code, me)
	}
	_, body := getAs(t, ts.URL+"/api/workspaces", tok)
	if strings.Count(body, `"name":"Demo workspace"`) != 1 {
		t.Fatalf("workspaces = %s, want the one demo workspace", body)
	}
	wsID := body[strings.Index(body, `"id":"`)+6:]
	wsID = wsID[:strings.Index(wsID, `"`)]
	for path, want := range map[string]string{"/backlog": "Write the release notes", "/messages": "Welcome to Unison", "/schedules": "crash reports"} {
		if code, body := getAs(t, ts.URL+"/api/workspaces/"+wsID+path, tok); code != http.StatusOK || !strings.Contains(body, want) {
			t.Errorf("GET %s = %d %s, want %q", path, code, body, want)
		}
	}

	// Signing in again reuses the account and its workspace.
	again := signIn()
	if again == tok {
		t.Error("a second sign-in should be a new session")
	}
	if _, body := getAs(t, ts.URL+"/api/workspaces", again); strings.Count(body, `"name":"Demo workspace"`) != 1 {
		t.Errorf("after signing in again: %s", body)
	}
}
