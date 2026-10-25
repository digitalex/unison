package server

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

func TestJoinWhileSignedIn(t *testing.T) {
	e := newAuthEnv(t)
	ws, _, _ := e.st.CreateWorkspace(t.Context(), "W", "", "")
	join := "/api/workspaces/" + ws.ID + "/join"
	c, _ := e.login("github", "/", adaGitHub, adaGitHubEmails)
	_, me := e.do("GET", "/api/me/user", "", withCookie(c))
	userID := me["id"].(float64)

	resp, first := e.do("POST", join, `{"name":"Ada","emoji":"🦊"}`, withCookie(c), withCSRF())
	p := first["participant"].(map[string]any)
	if resp.StatusCode != 201 || first["existing"] != false || p["user_id"] != userID {
		t.Fatalf("signed-in join = %d %v", resp.StatusCode, first)
	}
	// Joining again (another tab, another name) returns the same participant
	// and token instead of a duplicate.
	resp, again := e.do("POST", join, `{"name":"AdaPhone"}`, withCookie(c), withCSRF())
	if resp.StatusCode != 200 || again["existing"] != true || again["token"] != first["token"] ||
		again["participant"].(map[string]any)["id"] != p["id"] {
		t.Errorf("second signed-in join = %d %v", resp.StatusCode, again)
	}
	// Same for the CLI's user bearer token.
	u, _ := e.st.LoginWithProvider(t.Context(), "github", store.Profile{Subject: "101"})
	cli, _, _ := e.st.CreateSession(t.Context(), u.ID, store.SessionCLI, "cli")
	if _, viaCLI := e.do("POST", join, `{}`, withBearer(cli)); viaCLI["token"] != first["token"] {
		t.Errorf("CLI join = %v, want the same participant", viaCLI)
	}

	// Without the CSRF header the cookie is ignored: a plain join, as today.
	resp, plain := e.do("POST", join, `{"name":"Mallory"}`, withCookie(c))
	if resp.StatusCode != 201 || plain["participant"].(map[string]any)["user_id"] != nil {
		t.Errorf("join with cookie but no CSRF header = %d %v", resp.StatusCode, plain)
	}
	resp, agent := e.do("POST", join, `{"name":"Quill"}`)
	if resp.StatusCode != 201 || agent["participant"].(map[string]any)["user_id"] != nil || agent["existing"] != nil {
		t.Errorf("agent join = %d %v (must be unchanged)", resp.StatusCode, agent)
	}

	// /me shows the account for linked participants only.
	_, mine := e.do("GET", "/api/workspaces/"+ws.ID+"/me", "", withBearer(first["token"].(string)))
	if user, _ := mine["user"].(map[string]any); user["display_name"] != "Ada Lovelace" || mine["name"] != "Ada" {
		t.Errorf("linked /me = %v", mine)
	}
	if _, theirs := e.do("GET", "/api/workspaces/"+ws.ID+"/me", "", withBearer(agent["token"].(string))); theirs["user"] != nil || theirs["name"] != "Quill" {
		t.Errorf("agent /me = %v", theirs)
	}
}

func TestClaimAndMembersAPI(t *testing.T) {
	e := newAuthEnv(t)
	ctx := t.Context()
	ws, _, _ := e.st.CreateWorkspace(ctx, "W", "", "")
	other, _, _ := e.st.CreateWorkspace(ctx, "Other", "", "")
	base := "/api/workspaces/" + ws.ID
	_, alexTok, _, _ := e.st.Join(ctx, ws.ID, "Alex", "🧑")
	_, mobileTok, _, _ := e.st.Join(ctx, ws.ID, "Alexmobile", "📱")
	e.st.Join(ctx, ws.ID, "Quill", "🦉")
	_, otherTok, _, _ := e.st.Join(ctx, other.ID, "Alex", "")
	alex, _ := e.login("google", "/", adaGoogle, nil)
	bob, _ := e.login("google", "/", bobGoogle, nil)

	for name, tc := range map[string]struct {
		opts []reqOpt
		body string
		want int
	}{
		"no account":              {[]reqOpt{withBearer(alexTok)}, "", 401},
		"cookie without CSRF":     {[]reqOpt{withCookie(alex), withBearer(alexTok)}, "", 403},
		"no participant token":    {[]reqOpt{withCookie(alex), withCSRF()}, "", 403},
		"bogus participant token": {[]reqOpt{withCookie(alex), withCSRF()}, `{"participant_token":"0123456789abcdef0123456789abcdef"}`, 403},
		"other workspace's token": {[]reqOpt{withCookie(alex), withCSRF(), withBearer(otherTok)}, "", 403},
	} {
		if resp, out := e.do("POST", base+"/claim", tc.body, tc.opts...); resp.StatusCode != tc.want {
			t.Errorf("claim (%s) = %d %v, want %d", name, resp.StatusCode, out, tc.want)
		}
	}
	// Web: the cookie is the account, the bearer is the participant.
	resp, p := e.do("POST", base+"/claim", "", withCookie(alex), withCSRF(), withBearer(alexTok))
	if resp.StatusCode != 200 || p["name"] != "Alex" || p["user_id"] == nil {
		t.Fatalf("web claim = %d %v", resp.StatusCode, p)
	}
	userID := p["user_id"].(float64)
	// CLI: the bearer is the account, the participant token is in the body.
	u, _ := e.st.GetUser(ctx, int64(userID))
	cli, _, _ := e.st.CreateSession(ctx, u.ID, store.SessionCLI, "cli")
	if resp, p := e.do("POST", base+"/claim", `{"participant_token":"`+mobileTok+`"}`, withBearer(cli)); resp.StatusCode != 200 || p["user_id"] != userID {
		t.Errorf("CLI claim = %d %v", resp.StatusCode, p)
	}
	if resp, _ := e.do("POST", base+"/claim", "", withCookie(bob), withCSRF(), withBearer(alexTok)); resp.StatusCode != 409 {
		t.Errorf("claiming someone else's participant = %d, want 409", resp.StatusCode)
	}
	_, evs := e.do("GET", base+"/events?after=0", "")
	claimed := 0
	for _, ev := range evs["events"].([]any) {
		if ev.(map[string]any)["type"] == "participant.claimed" {
			claimed++
		}
	}
	if claimed != 2 {
		t.Errorf("participant.claimed events = %d, want 2", claimed)
	}

	resp, m := e.do("GET", base+"/members", "")
	members, _ := m["members"].([]any)
	unlinked, _ := m["unlinked"].([]any)
	if resp.StatusCode != 200 || len(members) != 1 || len(unlinked) != 1 {
		t.Fatalf("members = %d %v", resp.StatusCode, m)
	}
	if mem := members[0].(map[string]any); mem["display_name"] != "Ada L." || len(mem["participants"].([]any)) != 2 {
		t.Errorf("member = %v", mem)
	}

	// Bob joins while signed in (a member), then removes Alex's account.
	e.do("POST", base+"/join", `{"name":"Bob"}`, withCookie(bob), withCSRF())
	remove := base + "/members/" + strconv.Itoa(int(userID))
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
	_, m = e.do("GET", base+"/members", "")
	if len(m["members"].([]any)) != 1 || len(m["unlinked"].([]any)) != 3 {
		t.Errorf("members after removal = %v (Alex's participants stay, for history)", m)
	}
	// Removal enforces nothing yet: Alex's participants keep working.
	if resp, _ := e.do("GET", base+"/me", "", withBearer(alexTok)); resp.StatusCode != 200 {
		t.Errorf("removed member's participant /me = %d", resp.StatusCode)
	}
}
