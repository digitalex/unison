package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

type sentEvent struct{ clientID, name, kind string }

type fakeTracker struct {
	mu   sync.Mutex
	sent []sentEvent
}

func (f *fakeTracker) Send(_ context.Context, clientID, name string, params map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentEvent{clientID, name, params["actor_type"]})
	return nil
}

// take waits for the background sends and returns them as "name/kind".
func (f *fakeTracker) take(s *Server) []string {
	s.waitTracking()
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []string{}
	for _, e := range f.sent {
		out = append(out, e.name+"/"+e.kind)
	}
	f.sent = nil
	sort.Strings(out) // they're sent concurrently
	return out
}

func TestAnalyticsEvents(t *testing.T) {
	e := newAuthEnv(t)
	f := &fakeTracker{}
	e.srv.SetTracker(f)
	ctx := t.Context()
	ws, _, _ := e.st.CreateWorkspace(ctx, "Unison", "", "")
	base := "/api/workspaces/" + ws.ID
	ada, _ := e.login("github", "/", adaGitHub, adaGitHubEmails)
	session := []reqOpt{withCookie(ada), withCSRF()}

	check := func(what string, want ...string) {
		t.Helper()
		got := f.take(e.srv)
		sort.Strings(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: sent %v, want %v", what, got, want)
		}
	}
	human := e.memberJoins(ada, ws.ID, "Ada")

	resp, out := e.do("POST", base+"/passes", "", session...)
	pass, _ := out["pass"].(string)
	if resp.StatusCode != 201 {
		t.Fatalf("mint = %d %v", resp.StatusCode, out)
	}
	check("pass", "agent_pass_generated/human")
	resp, out = e.do("POST", base+"/join", fmt.Sprintf(`{"pass":%q,"name":"Bot"}`, pass))
	agent, _ := out["token"].(string)
	if resp.StatusCode != 201 {
		t.Fatalf("agent join = %d %v", resp.StatusCode, out)
	}
	check("agent joins", "workspace_joined/agent")

	if resp, out = e.do("POST", base+"/invites", `{"link":true}`, session...); resp.StatusCode != 201 {
		t.Fatalf("invite link = %d %v", resp.StatusCode, out)
	}
	check("invite link", "invite_link_generated/human")
	invite, _ := out["token"].(string)
	bob, _ := e.login("google", "/", bobGoogle, nil)
	join := fmt.Sprintf(`{"name":"Bob","invite":%q}`, invite)
	if resp, out = e.do("POST", base+"/join", join, withCookie(bob), withCSRF()); resp.StatusCode != 201 {
		t.Fatalf("human join = %d %v", resp.StatusCode, out)
	}
	check("human joins", "workspace_joined/human")
	if resp, out = e.do("POST", base+"/join", join, withCookie(bob), withCSRF()); resp.StatusCode != 200 {
		t.Fatalf("human joins again = %d %v", resp.StatusCode, out)
	}
	check("human joins again")
	if resp, out = e.do("POST", base+"/invites", `{"email":"x@example.com"}`, session...); resp.StatusCode != 201 {
		t.Fatalf("invite email = %d %v", resp.StatusCode, out)
	}
	check("email invite")

	for _, tc := range []struct {
		token, kind string
	}{{human, "human"}, {agent, "agent"}} {
		as := withBearer(tc.token)
		do := func(method, path, body string, want ...string) map[string]any {
			t.Helper()
			resp, out := e.do(method, base+path, body, as)
			if resp.StatusCode/100 != 2 {
				t.Fatalf("%s %s = %d %v", method, path, resp.StatusCode, out)
			}
			for i := range want {
				want[i] += "/" + tc.kind
			}
			check(method+" "+path, want...)
			return out
		}
		msg := do("POST", "/messages", `{"body":"hi"}`, "chat_message_posted")
		id := int64(msg["id"].(float64))
		do("PUT", fmt.Sprintf("/messages/%d/reactions/+1", id), "", "chat_reaction_added")
		do("PUT", fmt.Sprintf("/messages/%d/reactions/+1", id), "")
		do("DELETE", fmt.Sprintf("/messages/%d/reactions/+1", id), "")
		me := do("GET", "/me", "")
		meID := int64(me["id"].(float64))
		item := do("POST", "/backlog", `{"title":"a"}`, "backlog_item_created")
		itemID := int64(item["id"].(float64))
		do("POST", fmt.Sprintf("/backlog/%d/assign", itemID), `{}`, "backlog_item_assigned")
		do("POST", fmt.Sprintf("/backlog/%d/assign", itemID), `{"assignee_id":null}`)
		do("POST", fmt.Sprintf("/backlog/%d/close", itemID), `{}`, "backlog_item_closed")
		do("POST", "/backlog", fmt.Sprintf(`{"title":"b","assignee_id":%d}`, meID), "backlog_item_created", "backlog_item_assigned")
		do("POST", "/repos", fmt.Sprintf(`{"url":"https://github.com/acme/%s"}`, tc.kind), "github_repo_added")
		do("POST", "/repos", fmt.Sprintf(`{"url":"https://gitlab.com/acme/%s"}`, tc.kind))
	}
}

// Nothing identifying goes out, and each actor keeps one stable ID.
func TestAnalyticsClientIDs(t *testing.T) {
	e := newAuthEnv(t)
	f := &fakeTracker{}
	e.srv.SetTracker(f)
	id := func(a analyticsActor) string {
		e.srv.track("x", a)
		e.srv.waitTracking()
		f.mu.Lock()
		defer f.mu.Unlock()
		last := f.sent[len(f.sent)-1]
		if strings.Contains(last.clientID, ":") || len(last.clientID) != 32 {
			t.Errorf("client ID %q isn't a hash", last.clientID)
		}
		return last.clientID
	}
	agent := &store.Participant{ID: 7, UserID: 0, Sponsor: &store.Sponsor{UserID: 1}}
	member := &store.Participant{ID: 8, UserID: 1}
	if participantActor(agent).kind != "agent" || participantActor(member).kind != "human" {
		t.Errorf("kinds: agent %+v, member %+v", participantActor(agent), participantActor(member))
	}
	if id(participantActor(agent)) == id(participantActor(member)) {
		t.Error("an agent and its sponsor share an ID")
	}
	if id(participantActor(member)) != id(userActor(1)) {
		t.Error("a member and their account have different IDs")
	}
}
