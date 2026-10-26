package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

// linkFlow runs /auth/{provider}/start?link=1 (with opts, e.g. the session
// cookie) through the fake provider for user, and returns the callback.
func (e *authEnv) linkFlow(provider string, user map[string]any, start, callback []reqOpt) *http.Response {
	e.t.Helper()
	resp, _ := e.do("GET", "/auth/"+provider+"/start?link=1&return_to="+url.QueryEscape("/me"), "", start...)
	stateCookie := cookieNamed(resp, oauthCookie)
	code, state, _ := e.fake.authorize(resp.Header.Get("Location"), user, nil)
	resp, _ = e.do("GET", "/auth/"+provider+"/callback?code="+code+"&state="+state, "", append(callback, withCookie(stateCookie))...)
	return resp
}

func identities(t *testing.T, e *authEnv, c *http.Cookie) []any {
	t.Helper()
	_, u := e.do("GET", "/api/me/user", "", withCookie(c))
	ids, _ := u["identities"].([]any)
	return ids
}

func TestLinkMode(t *testing.T) {
	e := newAuthEnv(t)
	ada, _ := e.login("github", "/", adaGitHub, adaGitHubEmails)
	adaWork := map[string]any{"sub": "g-ada-work", "email": "ada@work.example", "email_verified": true, "name": "Ada (work)"}

	// Signed in: the Google identity attaches to Ada; no new session.
	resp := e.linkFlow("google", adaWork, []reqOpt{withCookie(ada)}, []reqOpt{withCookie(ada)})
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/me" || cookieNamed(resp, sessionCookie) != nil {
		t.Fatalf("link callback = %d %q, session cookie %v", resp.StatusCode, resp.Header.Get("Location"), cookieNamed(resp, sessionCookie))
	}
	if ids := identities(t, e, ada); len(ids) != 2 || ids[1].(map[string]any)["subject"] != "g-ada-work" {
		t.Fatalf("identities after link = %v", ids)
	}

	// An identity that belongs to someone else is refused.
	bob, _ := e.login("google", "/", bobGoogle, nil)
	resp = e.linkFlow("google", bobGoogle, []reqOpt{withCookie(ada)}, []reqOpt{withCookie(ada)})
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("linking Bob's identity = %d, want 409", resp.StatusCode)
	}
	if len(identities(t, e, ada)) != 2 || len(identities(t, e, bob)) != 1 {
		t.Error("a refused link changed someone's identities")
	}

	// Started signed in, finished signed out (or as someone else): refused.
	resp = e.linkFlow("google", map[string]any{"sub": "g-x", "name": "X"}, []reqOpt{withCookie(ada)}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("link finished without the session = %d, want 400", resp.StatusCode)
	}
	resp = e.linkFlow("google", map[string]any{"sub": "g-y", "name": "Y"}, []reqOpt{withCookie(ada)}, []reqOpt{withCookie(bob)})
	if resp.StatusCode != http.StatusBadRequest || len(identities(t, e, bob)) != 1 {
		t.Errorf("link finished as another user = %d", resp.StatusCode)
	}

	// Signed out, link=1 is an ordinary sign-in.
	resp = e.linkFlow("google", map[string]any{"sub": "g-new", "name": "New"}, nil, nil)
	if c := cookieNamed(resp, sessionCookie); resp.StatusCode != http.StatusFound || c == nil {
		t.Errorf("signed-out link=1 = %d, session %v (want a normal sign-in)", resp.StatusCode, c)
	}
}

func TestProfileAPI(t *testing.T) {
	e := newAuthEnv(t)
	ctx := t.Context()
	ada, _ := e.login("github", "/", adaGitHub, adaGitHubEmails)
	e.login("github", "/", adaGitHub, adaGitHubEmails) // a second browser
	_, me := e.do("GET", "/api/me/user", "", withCookie(ada))
	adaID := int64(me["id"].(float64))
	cli, _, _ := e.st.CreateSession(ctx, adaID, store.SessionCLI, "unison on laptop")

	// Sessions: sign out everywhere else.
	if resp, _ := e.do("POST", "/api/me/sessions/revoke-others", "", withCookie(ada)); resp.StatusCode != 403 {
		t.Errorf("revoke-others without CSRF = %d", resp.StatusCode)
	}
	if resp, out := e.do("POST", "/api/me/sessions/revoke-others", "", withCookie(ada), withCSRF()); resp.StatusCode != 200 || out["revoked"] != 2.0 {
		t.Errorf("revoke-others = %d %v", resp.StatusCode, out)
	}
	if resp, list := e.doList("GET", "/api/me/sessions", withCookie(ada)); resp.StatusCode != 200 || len(list) != 1 || list[0]["current"] != true {
		t.Errorf("sessions after revoke-others = %v", list)
	}
	if resp, _ := e.do("GET", "/api/me/user", "", withBearer(cli)); resp.StatusCode != 401 {
		t.Errorf("the CLI session survived revoke-others: %d", resp.StatusCode)
	}

	// Agents across workspaces.
	ws, _, _ := e.st.CreateWorkspace(ctx, "Unison", "", "")
	e.st.JoinAsUser(ctx, ws.ID, adaID, "Ada", "")
	bob, _ := e.st.LoginWithProvider(ctx, "github", store.Profile{Subject: "bob", Name: "Bob"})
	e.st.JoinAsUser(ctx, ws.ID, bob.ID, "Bob", "")
	bobTok, _, _ := e.st.CreateSession(ctx, bob.ID, store.SessionCLI, "")
	pass, _, _ := e.st.MintPass(ctx, ws.ID, adaID)
	agent, agentTok, _, _ := e.st.JoinWithPass(ctx, ws.ID, pass, "Quill", "🪶")
	revoke := "/api/me/agents/" + jsonID(agent.ID) + "/revoke"

	if resp, _ := e.do("GET", "/api/me/agents", ""); resp.StatusCode != 401 {
		t.Errorf("anonymous agents = %d", resp.StatusCode)
	}
	if resp, _ := e.do("GET", "/api/me/agents", "", withBearer(agentTok)); resp.StatusCode != 401 {
		t.Errorf("agents with a participant token = %d", resp.StatusCode)
	}
	resp, list := e.doList("GET", "/api/me/agents", withCookie(ada))
	if resp.StatusCode != 200 || len(list) != 1 || list[0]["name"] != "Quill" || list[0]["workspace_name"] != "Unison" || list[0]["token_expires_at"] == nil {
		t.Fatalf("my agents = %d %v", resp.StatusCode, list)
	}
	if _, theirs := e.doList("GET", "/api/me/agents", withBearer(bobTok)); len(theirs) != 0 {
		t.Errorf("Bob sees Ada's agents: %v", theirs)
	}

	for name, tc := range map[string]struct {
		opts []reqOpt
		want int
	}{
		"anonymous":            {nil, 401},
		"the agent itself":     {[]reqOpt{withBearer(agentTok)}, 401},
		"another member (Bob)": {[]reqOpt{withBearer(bobTok)}, 403},
		"no CSRF header":       {[]reqOpt{withCookie(ada)}, 403},
	} {
		if resp, out := e.do("POST", revoke, "", tc.opts...); resp.StatusCode != tc.want {
			t.Errorf("revoke (%s) = %d %v, want %d", name, resp.StatusCode, out, tc.want)
		}
	}
	if resp, _ := e.do("POST", "/api/me/agents/9999/revoke", "", withCookie(ada), withCSRF()); resp.StatusCode != 404 {
		t.Errorf("revoke unknown = %d", resp.StatusCode)
	}
	resp, p := e.do("POST", revoke, "", withCookie(ada), withCSRF())
	if resp.StatusCode != 200 || p["revoked_at"] == nil {
		t.Fatalf("revoke = %d %v", resp.StatusCode, p)
	}
	resp, out := e.do("POST", "/api/workspaces/"+ws.ID+"/messages", `{"body":"hi"}`, withBearer(agentTok))
	if resp.StatusCode != 401 || !strings.Contains(out["error"].(string), "Ada Lovelace revoked this agent credential") {
		t.Errorf("revoked agent = %d %v", resp.StatusCode, out)
	}
	_, evs := e.do("GET", "/api/workspaces/"+ws.ID+"/events?after=0", "", withCookie(ada))
	found := false
	for _, ev := range evs["events"].([]any) {
		found = found || ev.(map[string]any)["type"] == "participant.revoked"
	}
	if !found {
		t.Error("no participant.revoked event")
	}
	if _, list := e.doList("GET", "/api/me/agents", withCookie(ada)); len(list) != 1 || list[0]["revoked_at"] == nil {
		t.Errorf("revoked agent should stay listed, marked: %v", list)
	}
}
