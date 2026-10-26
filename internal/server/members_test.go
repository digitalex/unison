package server

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

func TestJoinWhileSignedIn(t *testing.T) {
	e := newAuthEnv(t)
	c, _ := e.login("github", "/", adaGitHub, adaGitHubEmails)
	_, me := e.do("GET", "/api/me/user", "", withCookie(c))
	userID := me["id"].(float64)
	resp, created := e.do("POST", "/api/workspaces", `{"name":"W"}`, withCookie(c), withCSRF())
	if resp.StatusCode != 201 {
		t.Fatalf("create = %d %v", resp.StatusCode, created)
	}
	wsID := created["id"].(string)
	join := "/api/workspaces/" + wsID + "/join"

	// The creator is a member: joining creates their participant...
	resp, first := e.do("POST", join, `{"name":"Ada","emoji":"🦊"}`, withCookie(c), withCSRF())
	p, _ := first["participant"].(map[string]any)
	if resp.StatusCode != 201 || first["existing"] != false || p["user_id"] != userID {
		t.Fatalf("signed-in join = %d %v", resp.StatusCode, first)
	}
	// ...and joining again (another tab, another name) returns it.
	resp, again := e.do("POST", join, `{"name":"AdaPhone"}`, withCookie(c), withCSRF())
	if resp.StatusCode != 200 || again["existing"] != true || again["token"] != first["token"] {
		t.Errorf("second signed-in join = %d %v", resp.StatusCode, again)
	}
	// Same for the CLI's user bearer token.
	u, _ := e.st.LoginWithProvider(t.Context(), "github", store.Profile{Subject: "101"})
	cli, _, _ := e.st.CreateSession(t.Context(), u.ID, store.SessionCLI, "cli")
	if _, viaCLI := e.do("POST", join, `{}`, withBearer(cli)); viaCLI["token"] != first["token"] {
		t.Errorf("CLI join = %v, want the same participant", viaCLI)
	}

	// No anonymous joins: without the CSRF header the cookie is ignored.
	if resp, out := e.do("POST", join, `{"name":"Mallory"}`, withCookie(c)); resp.StatusCode != 401 || out["code"] != codeSignIn {
		t.Errorf("join with cookie but no CSRF header = %d %v", resp.StatusCode, out)
	}
	if resp, out := e.do("POST", join, `{"name":"Quill"}`); resp.StatusCode != 401 {
		t.Errorf("anonymous join = %d %v", resp.StatusCode, out)
	}
	// Signed in but not a member, no invite: 403.
	bob, _ := e.login("google", "/", bobGoogle, nil)
	if resp, out := e.do("POST", join, `{"name":"Bob"}`, withCookie(bob), withCSRF()); resp.StatusCode != 403 {
		t.Errorf("uninvited join = %d %v", resp.StatusCode, out)
	}

	// /me shows the account for linked participants; agents show a sponsor.
	_, mine := e.do("GET", "/api/workspaces/"+wsID+"/me", "", withBearer(first["token"].(string)))
	if user, _ := mine["user"].(map[string]any); user["display_name"] != "Ada Lovelace" || mine["name"] != "Ada" {
		t.Errorf("linked /me = %v", mine)
	}
	pass, _, _ := e.st.MintPass(t.Context(), wsID, int64(userID))
	_, agentTok, _, _ := e.st.JoinWithPass(t.Context(), wsID, pass, "Quill", "")
	if _, theirs := e.do("GET", "/api/workspaces/"+wsID+"/me", "", withBearer(agentTok)); theirs["user"] != nil || theirs["sponsor"] == nil {
		t.Errorf("agent /me = %v", theirs)
	}
}

func TestMembersAPI(t *testing.T) {
	e := newAuthEnv(t)
	ctx := t.Context()
	alex, _ := e.login("google", "/", adaGoogle, nil)
	_, me := e.do("GET", "/api/me/user", "", withCookie(alex))
	alexID := int64(me["id"].(float64))
	ws, _, _ := e.st.CreateWorkspaceFor(ctx, alexID, "W", "", "")
	base := "/api/workspaces/" + ws.ID
	_, alexTok, _, _, _ := e.st.JoinAsUser(ctx, ws.ID, alexID, "Alex", "🧑")
	e.st.Join(ctx, ws.ID, "Quill", "🦉") // Linked to no account: not listed.

	if resp, _ := e.do("GET", base+"/members", ""); resp.StatusCode != 401 {
		t.Errorf("anonymous members = %d", resp.StatusCode)
	}
	resp, m := e.do("GET", base+"/members", "", withBearer(alexTok))
	members, _ := m["members"].([]any)
	if resp.StatusCode != 200 || len(members) != 1 || m["unlinked"] != nil {
		t.Fatalf("members = %d %v", resp.StatusCode, m)
	}

	// Bob joins with an invite link, then removes Alex.
	bob, _ := e.login("google", "/", bobGoogle, nil)
	_, inv := e.do("POST", base+"/invites", `{"link":true}`, withCookie(alex), withCSRF())
	if resp, out := e.do("POST", base+"/join", `{"name":"Bob","invite":"`+inv["token"].(string)+`"}`, withCookie(bob), withCSRF()); resp.StatusCode != 201 {
		t.Fatalf("join with link = %d %v", resp.StatusCode, out)
	}
	remove := base + "/members/" + strconv.FormatInt(alexID, 10)
	if resp, _ := e.do("DELETE", remove, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous remove = %d", resp.StatusCode)
	}
	carol, _ := e.st.LoginWithProvider(ctx, "github", store.Profile{Subject: "c"})
	carolTok, _, _ := e.st.CreateSession(ctx, carol.ID, store.SessionCLI, "")
	if resp, _ := e.do("DELETE", remove, "", withBearer(carolTok)); resp.StatusCode != 403 {
		t.Errorf("non-member remove = %d, want 403", resp.StatusCode)
	}
	if resp, _ := e.do("DELETE", base+"/members/abc", "", withCookie(bob), withCSRF()); resp.StatusCode != 400 {
		t.Errorf("bad id = %d", resp.StatusCode)
	}
	if resp, out := e.do("DELETE", remove, "", withCookie(bob), withCSRF()); resp.StatusCode != 200 {
		t.Fatalf("remove = %d %v", resp.StatusCode, out)
	}
	if resp, _ := e.do("DELETE", remove, "", withCookie(bob), withCSRF()); resp.StatusCode != 404 {
		t.Errorf("remove twice = %d, want 404", resp.StatusCode)
	}
	// Removal is enforced now: Alex's participant and cookie are out.
	if resp, out := e.do("GET", base+"/me", "", withBearer(alexTok)); resp.StatusCode != 403 || out["code"] != codeMember {
		t.Errorf("removed member's participant /me = %d %v", resp.StatusCode, out)
	}
	if resp, _ := e.do("GET", base, "", withCookie(alex)); resp.StatusCode != 403 {
		t.Errorf("removed member's cookie = %d", resp.StatusCode)
	}
	if resp, _ := e.do("POST", base+"/join", `{}`, withCookie(alex), withCSRF()); resp.StatusCode != 403 {
		t.Errorf("removed member rejoining without an invite = %d", resp.StatusCode)
	}
}
