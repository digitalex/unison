package server

import (
	"testing"
)

// GET /api/workspaces says how far each workspace's collaboration has got
// for the caller: what teammates did, not what they did themselves (#300).
func TestWorkspaceListActivitySeq(t *testing.T) {
	e := newAuthEnv(t)
	ada, _ := e.login("github", "/", adaGitHub, adaGitHubEmails)
	bob, _ := e.login("google", "/", bobGoogle, nil)
	_, ws := e.do("POST", "/api/workspaces", `{"name":"Shared"}`, withCookie(ada), withCSRF())
	wsID := ws["id"].(string)
	base := "/api/workspaces/" + wsID
	adaTok := e.memberJoins(ada, wsID, "Ada")
	bobTok := e.memberJoins(bob, wsID, "Bob")

	activity := func(who string, opts ...reqOpt) (seq float64, present bool) {
		t.Helper()
		resp, list := e.doList("GET", "/api/workspaces", opts...)
		if resp.StatusCode != 200 || len(list) != 1 {
			t.Fatalf("%s's workspaces = %d %v", who, resp.StatusCode, list)
		}
		seq, present = list[0]["activity_seq"].(float64)
		return seq, present
	}
	// Bob joined after Ada: that is activity for her, and hers is for him.
	adaSees, _ := activity("Ada", withCookie(ada))
	bobSees, _ := activity("Bob", withCookie(bob))
	if adaSees <= bobSees || bobSees == 0 {
		t.Fatalf("after joining: Ada sees %v, Bob sees %v; want Ada ahead (Bob joined last), both set", adaSees, bobSees)
	}

	// Ada's own message moves it for Bob, not for her.
	if resp, _ := e.do("POST", base+"/messages", `{"body":"hello"}`, withBearer(adaTok)); resp.StatusCode != 201 {
		t.Fatalf("Ada posting = %d", resp.StatusCode)
	}
	if got, _ := activity("Ada", withCookie(ada)); got != adaSees {
		t.Errorf("after her own message Ada sees %v, want it still %v", got, adaSees)
	}
	afterHello, _ := activity("Bob", withCookie(bob))
	if afterHello <= adaSees {
		t.Errorf("after Ada's message Bob sees %v, want it past %v", afterHello, adaSees)
	}

	// A status change is noise for everyone.
	if resp, _ := e.do("PATCH", base+"/me", `{"status":"thinking"}`, withBearer(bobTok)); resp.StatusCode != 200 {
		t.Fatalf("Bob's status = %d", resp.StatusCode)
	}
	if got, _ := activity("Ada", withCookie(ada)); got != adaSees {
		t.Errorf("after Bob's status Ada sees %v, want it still %v", got, adaSees)
	}

	// A workspace nobody else has touched has no activity_seq at all.
	solo := newAuthEnv(t)
	cy, _ := solo.login("github", "/", adaGitHub, adaGitHubEmails)
	_, mine := solo.do("POST", "/api/workspaces", `{"name":"Mine"}`, withCookie(cy), withCSRF())
	tok := solo.memberJoins(cy, mine["id"].(string), "Cy")
	if resp, _ := solo.do("POST", "/api/workspaces/"+mine["id"].(string)+"/messages", `{"body":"notes"}`, withBearer(tok)); resp.StatusCode != 201 {
		t.Fatalf("posting alone = %d", resp.StatusCode)
	}
	_, list := solo.doList("GET", "/api/workspaces", withCookie(cy))
	if len(list) != 1 {
		t.Fatalf("solo list = %v", list)
	}
	if v, present := list[0]["activity_seq"]; present {
		t.Errorf("a workspace only its owner used has activity_seq %v", v)
	}
	// One workspace on its own doesn't carry it either.
	if _, one := e.do("GET", base, "", withCookie(ada)); one["activity_seq"] != nil {
		t.Errorf("GET of a single workspace has activity_seq %v", one["activity_seq"])
	}
}
