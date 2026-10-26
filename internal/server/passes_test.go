package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/digitalex/unison/internal/store"
)

func TestPassesAPI(t *testing.T) {
	e := newAuthEnv(t)
	ctx := t.Context()
	ws, _, _ := e.st.CreateWorkspace(ctx, "Unison", "", "")
	other, _, _ := e.st.CreateWorkspace(ctx, "Other", "", "")
	base := "/api/workspaces/" + ws.ID
	_, legacyTok, _, _ := e.st.Join(ctx, ws.ID, "Old", "")
	ada, _ := e.login("github", "/", adaGitHub, adaGitHubEmails)
	bob, _ := e.login("google", "/", bobGoogle, nil)
	e.memberJoins(ada, ws.ID, "Ada")

	for name, tc := range map[string]struct {
		opts []reqOpt
		want int
	}{
		"anonymous":        {nil, 401},
		"participant only": {[]reqOpt{withBearer(legacyTok)}, 401},
		"no CSRF header":   {[]reqOpt{withCookie(ada)}, 403},
		"not a member":     {[]reqOpt{withCookie(bob), withCSRF()}, 403},
	} {
		if resp, out := e.do("POST", base+"/passes", "", tc.opts...); resp.StatusCode != tc.want {
			t.Errorf("mint (%s) = %d %v, want %d", name, resp.StatusCode, out, tc.want)
		}
	}
	mint := func() string {
		t.Helper()
		resp, out := e.do("POST", base+"/passes", "", withCookie(ada), withCSRF())
		pass, _ := out["pass"].(string)
		prompt, _ := out["invite_prompt"].(string)
		if resp.StatusCode != 201 || !strings.HasPrefix(pass, store.AgentPassPrefix) || out["expires_at"] == nil ||
			!strings.Contains(prompt, "unison join "+e.ts.URL+"/w/"+ws.ID+" --pass "+pass) ||
			!strings.Contains(prompt, "expires in 15 minutes") || !strings.Contains(prompt, "agent of Ada Lovelace") ||
			!strings.Contains(prompt, e.ts.URL+"/w/"+ws.ID+"/agent.md?pass="+pass) {
			t.Fatalf("mint = %d %v", resp.StatusCode, out)
		}
		return pass
	}

	pass := mint()
	resp, joined := e.do("POST", base+"/join", `{"pass":"`+pass+`","name":"Quill","emoji":"🪶"}`)
	agent, _ := joined["participant"].(map[string]any)
	sponsor, _ := agent["sponsor"].(map[string]any)
	if resp.StatusCode != 201 || sponsor["display_name"] != "Ada Lovelace" || agent["user_id"] != nil || agent["token_expires_at"] == nil {
		t.Fatalf("join with pass = %d %v", resp.StatusCode, joined)
	}
	agentTok := joined["token"].(string)
	if resp, out := e.do("POST", base+"/join", `{"pass":"`+pass+`","name":"Again"}`); resp.StatusCode != 403 || !strings.Contains(out["error"].(string), "already used") {
		t.Errorf("reused pass = %d %v", resp.StatusCode, out)
	}
	if resp, out := e.do("POST", "/api/workspaces/"+other.ID+"/join", `{"pass":"`+mint()+`"}`); resp.StatusCode != 403 || !strings.Contains(out["error"].(string), "another workspace") {
		t.Errorf("pass for another workspace = %d %v", resp.StatusCode, out)
	}
	// A pass joins as an agent even in a signed-in browser: never as the human.
	_, viaBrowser := e.do("POST", base+"/join", `{"pass":"`+mint()+`","name":"Lark"}`, withCookie(ada), withCSRF())
	if p := viaBrowser["participant"].(map[string]any); p["user_id"] != nil || p["sponsor"] == nil {
		t.Errorf("pass join with a cookie = %v", viaBrowser)
	}

	// The agent works, and shows as an agent of its sponsor.
	if resp, _ := e.do("POST", base+"/messages", `{"body":"hello"}`, withBearer(agentTok)); resp.StatusCode != 201 {
		t.Errorf("agent message = %d", resp.StatusCode)
	}
	_, me := e.do("GET", base+"/me", "", withBearer(agentTok))
	if s, _ := me["sponsor"].(map[string]any); s["display_name"] != "Ada Lovelace" || me["user"] != nil {
		t.Errorf("agent /me = %v", me)
	}

	// Bob becomes a member and removes Ada: her agents stop working, with
	// a clear message; everyone else is unaffected.
	bobTok := e.memberJoins(bob, ws.ID, "Bob")
	_, adaUser := e.do("GET", "/api/me/user", "", withCookie(ada))
	if resp, _ := e.do("DELETE", base+"/members/"+jsonID(adaUser["id"]), "", withCookie(bob), withCSRF()); resp.StatusCode != 200 {
		t.Fatalf("remove sponsor = %d", resp.StatusCode)
	}
	resp, out := e.do("POST", base+"/messages", `{"body":"still here?"}`, withBearer(agentTok))
	if resp.StatusCode != 401 || !strings.Contains(out["error"].(string), "Ada Lovelace, who invited this agent, is no longer a member") {
		t.Errorf("agent of a removed sponsor = %d %v", resp.StatusCode, out)
	}
	if resp, _ := e.do("POST", base+"/messages", `{"body":"hi"}`, withBearer(bobTok)); resp.StatusCode != 201 {
		t.Errorf("a member after another member's removal = %d", resp.StatusCode)
	}
}

func TestAgentCredentialExpiryAPI(t *testing.T) {
	e := newAuthEnv(t)
	ctx := t.Context()
	ws, _, _ := e.st.CreateWorkspace(ctx, "W", "", "")
	u, _ := e.st.LoginWithProvider(ctx, "github", store.Profile{Subject: "1", Name: "Ada"})
	e.st.JoinAsUser(ctx, ws.ID, u.ID, "Ada", "")
	_, legacyTok, _, _ := e.st.Join(ctx, ws.ID, "Old", "")
	pass, _, _ := e.st.MintPass(ctx, ws.ID, u.ID)
	_, agentTok, _, _ := e.st.JoinWithPass(ctx, ws.ID, pass, "Quill", "")
	me := "/api/workspaces/" + ws.ID + "/me"

	e.st.SetClock(func() time.Time { return time.Now().Add(store.AgentCredentialTTL + time.Hour) })
	resp, out := e.do("GET", me, "", withBearer(agentTok))
	if resp.StatusCode != 401 || !strings.Contains(out["error"].(string), "ask your human for a new invite") {
		t.Errorf("idle agent = %d %v", resp.StatusCode, out)
	}
	if resp, out := e.do("GET", me, "", withBearer(legacyTok)); resp.StatusCode != 401 || out["code"] != codeLegacy {
		t.Errorf("legacy participant = %d %v (locked out since #17)", resp.StatusCode, out)
	}
}

// memberJoins makes the user signed in with cookie c a member of wsID (as an
// invite would) and returns their participant token.
func (e *authEnv) memberJoins(c *http.Cookie, wsID, name string) string {
	e.t.Helper()
	_, me := e.do("GET", "/api/me/user", "", withCookie(c))
	_, tok, _, _, err := e.st.JoinAsUser(e.t.Context(), wsID, int64(me["id"].(float64)), name, "")
	if err != nil {
		e.t.Fatal(err)
	}
	return tok
}
