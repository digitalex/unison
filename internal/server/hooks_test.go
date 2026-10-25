package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func postHook(t *testing.T, url string, body []byte, headers map[string]string) int {
	t.Helper()
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWebhooksAPI(t *testing.T) {
	ts, st := newServer(t)
	ws, _, _ := st.CreateWorkspace(t.Context(), "W", "", "")
	_, token, _, _ := st.Join(t.Context(), ws.ID, "Ada", "")
	base := ts.URL + "/api/workspaces/" + ws.ID

	if status, _ := call(t, "POST", base+"/webhooks", "", `{"name":"GitHub"}`); status != http.StatusUnauthorized {
		t.Errorf("unauthenticated add = %d, want 401", status)
	}
	status, h := call(t, "POST", base+"/webhooks", token, `{"name":"GitHub","instructions":"Review PRs","signing_secret":"topsecret"}`)
	if status != http.StatusCreated || h["token"] == nil || !strings.HasPrefix(h["url"].(string), ts.URL+"/hooks/wh_") || h["has_signing_secret"] != true {
		t.Fatalf("add = %d %v", status, h)
	}
	hookToken, hookURL := h["token"].(string), h["url"].(string)
	id := strconv.Itoa(int(h["id"].(float64)))

	// Public reads never include the token or the signing secret.
	for _, path := range []string{"/webhooks", "/webhooks/" + id, "/events?after=0"} {
		if _, body := get(t, base+path); strings.Contains(body, hookToken) || strings.Contains(body, "topsecret") || strings.Contains(body, `"token"`) {
			t.Errorf("GET %s leaks a secret: %s", path, body)
		}
	}
	if status, _ := call(t, "GET", base+"/webhooks/"+id+"/secret", "", ""); status != http.StatusUnauthorized {
		t.Errorf("unauthenticated secret = %d, want 401", status)
	}
	if status, got := call(t, "GET", base+"/webhooks/"+id+"/secret", token, ""); status != http.StatusOK || got["url"] != hookURL {
		t.Errorf("secret = %d %v", status, got)
	}

	// Receiving: signature required because a signing secret is set.
	pr := []byte(`{"action":"opened","number":14,"pull_request":{"number":14,"title":"Add search"},"sender":{"login":"alex"}}`)
	gh := map[string]string{"Content-Type": "application/json", "X-GitHub-Event": "pull_request", "X-GitHub-Delivery": "abc"}
	if status := postHook(t, hookURL, pr, gh); status != http.StatusUnauthorized {
		t.Errorf("missing signature = %d, want 401", status)
	}
	gh["X-Hub-Signature-256"] = sign("wrong", pr)
	if status := postHook(t, hookURL, pr, gh); status != http.StatusUnauthorized {
		t.Errorf("bad signature = %d, want 401", status)
	}
	gh["X-Hub-Signature-256"] = sign("topsecret", pr)
	if status := postHook(t, hookURL, pr, gh); status != http.StatusAccepted {
		t.Fatalf("good signature = %d, want 202", status)
	}
	if status := postHook(t, ts.URL+"/hooks/wh_nope", pr, nil); status != http.StatusNotFound {
		t.Errorf("unknown token = %d, want 404", status)
	}
	if status := postHook(t, hookURL, bytes.Repeat([]byte("x"), maxWebhookBody+1), gh); status != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body = %d, want 413", status)
	}

	_, list := callList(t, base+"/webhooks/"+id+"/deliveries")
	if len(list) != 1 || list[0]["summary"] != `github: pull_request opened #14 "Add search" by alex` || list[0]["payload"] != nil {
		t.Fatalf("deliveries = %v", list)
	}
	did := strconv.Itoa(int(list[0]["id"].(float64)))
	if status, _ := call(t, "GET", base+"/deliveries/"+did, "", ""); status != http.StatusUnauthorized {
		t.Errorf("unauthenticated delivery = %d, want 401", status)
	}
	if status, d := call(t, "GET", base+"/deliveries/"+did, token, ""); status != http.StatusOK || d["payload"] != string(pr) {
		t.Errorf("delivery = %d %v", status, d)
	}

	// Rotating issues a new URL; the old one stops working.
	status, rot := call(t, "POST", base+"/webhooks/"+id+"/rotate", token, "")
	if status != http.StatusOK || rot["url"] == hookURL {
		t.Fatalf("rotate = %d %v", status, rot)
	}
	if status := postHook(t, hookURL, pr, gh); status != http.StatusNotFound {
		t.Errorf("old URL after rotate = %d, want 404", status)
	}
	// Disable, then delete.
	call(t, "PATCH", base+"/webhooks/"+id, token, `{"enabled":false,"signing_secret":""}`)
	if status := postHook(t, rot["url"].(string), []byte("{}"), nil); status != http.StatusNotFound {
		t.Errorf("disabled = %d, want 404", status)
	}
	if status, _ := call(t, "DELETE", base+"/webhooks/"+id, token, ""); status != http.StatusOK {
		t.Errorf("delete = %d", status)
	}
	if status, _ := call(t, "GET", base+"/deliveries/"+did, token, ""); status != http.StatusNotFound {
		t.Errorf("delivery after delete = %d, want 404", status)
	}
}

func callList(t *testing.T, url string) (int, []map[string]any) {
	t.Helper()
	status, body := get(t, url)
	var out []map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("GET %s: %v (%s)", url, err, body)
	}
	return status, out
}

func TestGitHubSummaries(t *testing.T) {
	for _, tc := range []struct{ event, body, want string }{
		{"pull_request", `{"action":"closed","pull_request":{"number":3,"title":"Fix","merged":true}}`, `pull_request merged #3 "Fix"`},
		{"push", `{"ref":"refs/heads/main","commits":[{},{}],"sender":{"login":"ada"}}`, `push main (2 commits) by ada`},
		{"push", `{"ref":"refs/heads/x","commits":[{}]}`, `push x (1 commit)`},
		{"issues", `{"action":"opened","issue":{"number":9,"title":"Bug"}}`, `issues opened #9 "Bug"`},
		{"issue_comment", `{"action":"created","issue":{"number":9,"title":"Bug"}}`, `issue_comment created on #9 "Bug"`},
		{"check_run", `{"action":"completed","check_run":{"name":"test","status":"completed","conclusion":"failure","head_branch":"main"}}`, `check_run "test" failure on main`},
		{"check_suite", `{"check_suite":{"status":"queued","app":{"name":"GitHub Actions"}}}`, `check_suite "GitHub Actions" queued`},
		{"workflow_run", `{"workflow_run":{"name":"CI","status":"completed","conclusion":"success","head_branch":"pr"}}`, `workflow_run "CI" success on pr`},
		{"release", `{"action":"published","release":{"tag_name":"v1.0","name":"One"}}`, `release published v1.0 "One"`},
		{"ping", `{"zen":"Keep it simple.","sender":{"login":"x"}}`, `ping "Keep it simple."`},
		{"star", `{"action":"created"}`, `star created`},
		{"push", `not json`, `push`},
	} {
		if got := githubSummary(tc.event, []byte(tc.body)); got != tc.want {
			t.Errorf("githubSummary(%s, %s) = %q, want %q", tc.event, tc.body, got, tc.want)
		}
	}
	in := parseDelivery(http.Header{"Content-Type": {"text/plain; charset=utf-8"}}, []byte("hi"))
	if in.Source != "generic" || in.Summary != "generic: text/plain" {
		t.Errorf("generic delivery = %+v", in)
	}
}
