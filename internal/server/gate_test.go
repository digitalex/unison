package server

import (
	"bufio"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/digitalex/unison/internal/store"
)

// concrete fills a route pattern ("POST /api/workspaces/{ws}/backlog/{id}")
// with values, returning the method and path.
func concrete(pattern, wsID string) (string, string) {
	method, path, _ := strings.Cut(pattern, " ")
	path = strings.NewReplacer("{ws}", wsID, "{id}", "1", "{cid}", "1", "{user}", "1").Replace(path)
	return method, path
}

// TestEveryWorkspaceRouteIsGated walks every registered workspace route:
// none may answer anonymous callers, participants from before accounts,
// signed-in non-members, or tokens of another workspace.
func TestEveryWorkspaceRouteIsGated(t *testing.T) {
	e := newAuthEnv(t)
	ctx := t.Context()
	alex, _ := e.st.LoginWithProvider(ctx, "github", store.Profile{Subject: "alex", Name: "Alex"})
	ws, _, _ := e.st.CreateWorkspaceFor(ctx, alex.ID, "Private", "", "")
	other, _, _ := e.st.CreateWorkspaceFor(ctx, alex.ID, "Other", "", "")
	_, legacyTok, _, _ := e.st.Join(ctx, ws.ID, "Old", "")
	_, otherTok, _, _, _ := e.st.JoinAsUser(ctx, other.ID, alex.ID, "Alex", "")
	stranger, _ := e.login("google", "/", bobGoogle, nil)
	strangerUser, _ := e.st.LoginWithProvider(ctx, "google", store.Profile{Subject: "g-202"})
	strangerTok, _, _ := e.st.CreateSession(ctx, strangerUser.ID, store.SessionCLI, "")

	if len(e.srv.wsRoutes) < 40 {
		t.Fatalf("only %d workspace routes registered", len(e.srv.wsRoutes))
	}
	for _, pattern := range e.srv.wsRoutes {
		method, path := concrete(pattern, ws.ID)
		for name, tc := range map[string]struct {
			opts []reqOpt
			want int
			code string
		}{
			"anonymous":                {nil, 401, codeSignIn},
			"legacy participant":       {[]reqOpt{withBearer(legacyTok)}, 401, codeLegacy},
			"legacy via ?token=":       {nil, 401, codeLegacy},
			"bogus token":              {[]reqOpt{withBearer("0123456789abcdef0123456789abcdef")}, 401, codeSignIn},
			"another workspace's":      {[]reqOpt{withBearer(otherTok)}, 403, codeOther},
			"signed-in non-member":     {[]reqOpt{withCookie(stranger), withCSRF()}, 403, codeMember},
			"non-member's user bearer": {[]reqOpt{withBearer(strangerTok)}, 403, codeMember},
		} {
			p := path
			if name == "legacy via ?token=" {
				p += "?token=" + legacyTok
			}
			resp, out := e.do(method, p, `{}`, tc.opts...)
			if resp.StatusCode != tc.want || out["code"] != tc.code {
				t.Errorf("%s (%s) = %d %v, want %d %s", pattern, name, resp.StatusCode, out, tc.want, tc.code)
			}
		}
	}
	if resp, out := e.do("GET", "/api/workspaces/"+ws.ID, "", withBearer(legacyTok)); out["error"] != legacyMessage {
		t.Errorf("legacy message = %d %v", resp.StatusCode, out)
	}
}

// TestProdAccessOnDeploy seeds the production shape and checks exactly who
// keeps access: Alex (user 1, a member) by cookie and by his linked
// participant, and the agents Wren (7) and Quillon (9) he sponsors. The
// participants from before accounts, Quill (2) and AlexHome (4) among them,
// are locked out.
func TestProdAccessOnDeploy(t *testing.T) {
	e := newAuthEnv(t)
	ctx := t.Context()
	alex, _ := e.st.LoginWithProvider(ctx, "github", store.Profile{Subject: "1", Name: "Alex", Email: "alex@example.com", EmailVerified: true})
	if alex.ID != 1 {
		t.Fatalf("Alex is user %d", alex.ID)
	}
	ws, _, _ := e.st.CreateWorkspace(ctx, "Unison", "", "")
	toks := map[int64]string{}
	add := func(id int64, tok string, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		toks[id] = tok
	}
	legacy := func(want int64, name string) {
		t.Helper()
		p, tok, _, err := e.st.Join(ctx, ws.ID, name, "")
		if err == nil && p.ID != want {
			t.Fatalf("%s is participant %d, want %d", name, p.ID, want)
		}
		add(p.ID, tok, err)
	}
	agent := func(want int64, name string) {
		t.Helper()
		pass, _, _ := e.st.MintPass(ctx, ws.ID, alex.ID)
		p, tok, _, err := e.st.JoinWithPass(ctx, ws.ID, pass, name, "")
		if err == nil && p.ID != want {
			t.Fatalf("%s is participant %d, want %d", name, p.ID, want)
		}
		add(p.ID, tok, err)
	}
	p1, tok1, _, _, err := e.st.JoinAsUser(ctx, ws.ID, alex.ID, "Alex", "🧑")
	add(p1.ID, tok1, err)
	legacy(2, "Quill")
	legacy(3, "Euler")
	legacy(4, "AlexHome")
	legacy(5, "Ada")
	legacy(6, "Lark")
	agent(7, "Wren")
	legacy(8, "Bob")
	agent(9, "Quillon")
	session, _, _ := e.st.CreateSession(ctx, alex.ID, store.SessionBrowser, "Chrome")
	cookie := &http.Cookie{Name: sessionCookie, Value: session}

	base := "/api/workspaces/" + ws.ID
	for id := int64(1); id <= 9; id++ {
		resp, out := e.do("GET", base+"/backlog", "", withBearer(toks[id]))
		keeps := id == 1 || id == 7 || id == 9
		switch {
		case keeps && resp.StatusCode != 200:
			t.Errorf("participant %d lost access: %d %v", id, resp.StatusCode, out)
		case !keeps && (resp.StatusCode != 401 || out["code"] != codeLegacy):
			t.Errorf("legacy participant %d = %d %v, want 401 legacy", id, resp.StatusCode, out)
		}
	}
	if resp, _ := e.do("GET", base+"/backlog", "", withCookie(cookie)); resp.StatusCode != 200 {
		t.Errorf("Alex by cookie = %d", resp.StatusCode)
	}
	if resp, _ := e.do("POST", base+"/messages", `{"body":"hi"}`, withCookie(cookie), withCSRF()); resp.StatusCode != 201 {
		t.Errorf("Alex posting by cookie = %d", resp.StatusCode)
	}
	if resp, list := e.doList("GET", "/api/workspaces", withCookie(cookie)); resp.StatusCode != 200 || len(list) != 1 {
		t.Errorf("Alex's workspaces = %d %v", resp.StatusCode, list)
	}
	// Signing in again on another device and joining gets his participant.
	if _, out := e.do("POST", base+"/join", `{}`, withCookie(cookie), withCSRF()); out["participant"].(map[string]any)["id"] != 1.0 {
		t.Errorf("Alex rejoining = %v", out)
	}
}

func TestWorkspaceListAndCreate(t *testing.T) {
	e := newAuthEnv(t)
	if resp, _ := e.do("GET", "/api/workspaces", ""); resp.StatusCode != 401 {
		t.Errorf("anonymous list = %d", resp.StatusCode)
	}
	ada, _ := e.login("github", "/", adaGitHub, adaGitHubEmails)
	bob, _ := e.login("google", "/", bobGoogle, nil)
	e.do("POST", "/api/workspaces", `{"name":"Ada's"}`, withCookie(ada), withCSRF())
	e.do("POST", "/api/workspaces", `{"name":"Bob's"}`, withCookie(bob), withCSRF())
	if _, list := e.doList("GET", "/api/workspaces", withCookie(ada)); len(list) != 1 || list[0]["name"] != "Ada's" {
		t.Errorf("Ada's list = %v (only her workspaces)", list)
	}
	if resp, _ := e.do("POST", "/api/workspaces", `{"name":"X"}`, withCookie(ada)); resp.StatusCode != 403 {
		t.Errorf("create without the CSRF header = %d", resp.StatusCode)
	}
}

func TestInvitesAPI(t *testing.T) {
	e := newAuthEnv(t)
	ctx := t.Context()
	ada, _ := e.login("github", "/", adaGitHub, adaGitHubEmails)
	_, created := e.do("POST", "/api/workspaces", `{"name":"Unison"}`, withCookie(ada), withCSRF())
	wsID := created["id"].(string)
	base := "/api/workspaces/" + wsID
	adaTok := e.memberJoins(ada, wsID, "Ada")

	if resp, _ := e.do("POST", base+"/invites", `{"email":"bob@example.com"}`); resp.StatusCode != 401 {
		t.Errorf("anonymous invite = %d", resp.StatusCode)
	}
	if resp, _ := e.do("POST", base+"/invites", `{"email":"bob@example.com"}`, withBearer(adaTok)); resp.StatusCode != 401 {
		t.Errorf("invite with a participant token = %d, want 401 (needs the account)", resp.StatusCode)
	}
	if resp, _ := e.do("POST", base+"/invites", `{"email":"bob@example.com"}`, withCookie(ada)); resp.StatusCode != 403 {
		t.Errorf("invite without CSRF = %d", resp.StatusCode)
	}
	resp, out := e.do("POST", base+"/invites", `{"email":"Bob@Example.com"}`, withCookie(ada), withCSRF())
	if inv, _ := out["invite"].(map[string]any); resp.StatusCode != 201 || inv["email"] != "bob@example.com" || out["token"] != nil {
		t.Fatalf("email invite = %d %v", resp.StatusCode, out)
	}
	// Bob signs in with that verified email and joins: a member.
	bob, _ := e.login("google", "/", bobGoogle, nil)
	if resp, out := e.do("POST", base+"/join", `{"name":"Bob"}`, withCookie(bob), withCSRF()); resp.StatusCode != 201 {
		t.Errorf("join by email invite = %d %v", resp.StatusCode, out)
	}

	resp, link := e.do("POST", base+"/invites", `{"link":true,"expires_in":"2d"}`, withCookie(ada), withCSRF())
	token, _ := link["token"].(string)
	if resp.StatusCode != 201 || !strings.HasPrefix(token, store.InviteTokenPrefix) || link["url"] != e.ts.URL+"/w/"+wsID+"/join?invite="+token {
		t.Fatalf("link invite = %d %v", resp.StatusCode, link)
	}
	if resp, _ := e.do("POST", base+"/invites", `{"link":true,"expires_in":"soon"}`, withCookie(ada), withCSRF()); resp.StatusCode != 400 {
		t.Errorf("bad expiry = %d", resp.StatusCode)
	}
	lookup := "/api/invites/lookup?token=" + token
	if resp, _ := e.do("GET", lookup, ""); resp.StatusCode != 401 {
		t.Errorf("anonymous lookup = %d (signed-out visitors learn nothing)", resp.StatusCode)
	}
	carol, _ := e.login("github", "/", map[string]any{"id": 303, "login": "carol"}, nil)
	if resp, l := e.do("GET", lookup, "", withCookie(carol)); resp.StatusCode != 200 || l["workspace_name"] != "Unison" {
		t.Errorf("lookup = %d %v", resp.StatusCode, l)
	}
	if resp, out := e.do("POST", base+"/join", `{"name":"Carol","invite":"`+token+`"}`, withCookie(carol), withCSRF()); resp.StatusCode != 201 {
		t.Errorf("join by link = %d %v", resp.StatusCode, out)
	}
	resp, list := e.doList("GET", base+"/invites", withBearer(adaTok))
	if resp.StatusCode != 200 || len(list) != 1 || list[0]["uses"] != 1.0 {
		t.Errorf("invites = %d %v", resp.StatusCode, list)
	}
	id := jsonID(list[0]["id"])
	if resp, _ := e.do("DELETE", base+"/invites/"+id, "", withCookie(ada), withCSRF()); resp.StatusCode != 200 {
		t.Errorf("revoke = %d", resp.StatusCode)
	}
	if resp, _ := e.do("GET", lookup, "", withCookie(carol)); resp.StatusCode != 404 {
		t.Errorf("lookup of a revoked link = %d", resp.StatusCode)
	}
	dave, _ := e.st.LoginWithProvider(ctx, "github", store.Profile{Subject: "dave"})
	daveTok, _, _ := e.st.CreateSession(ctx, dave.ID, store.SessionCLI, "")
	if resp, _ := e.do("POST", base+"/join", `{"invite":"`+token+`"}`, withBearer(daveTok)); resp.StatusCode != 403 {
		t.Errorf("join with a revoked link = %d", resp.StatusCode)
	}
}

func TestDoorwayAccess(t *testing.T) {
	e := newAuthEnv(t)
	ctx := t.Context()
	alex, _ := e.st.LoginWithProvider(ctx, "github", store.Profile{Subject: "alex", Name: "Alex"})
	ws, _, _ := e.st.CreateWorkspaceFor(ctx, alex.ID, "Secret Project", "", "https://isocan.io/p/secret")
	_, alexTok, _, _, _ := e.st.JoinAsUser(ctx, ws.ID, alex.ID, "Alex", "")
	_, legacyTok, _, _ := e.st.Join(ctx, ws.ID, "Old", "")
	doorway := "/w/" + ws.ID + "/agent.md"
	read := func(path string, opts ...reqOpt) (int, string) {
		req, _ := http.NewRequest("GET", e.ts.URL+path, nil)
		for _, o := range opts {
			o(req)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var b strings.Builder
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			b.WriteString(sc.Text() + "\n")
		}
		return resp.StatusCode, b.String()
	}

	if status, body := read("/agent.md"); status != 200 || strings.Contains(body, "Secret Project") {
		t.Errorf("generic doorway = %d (must stay public, without workspace data)", status)
	}
	for name, opts := range map[string][]reqOpt{"anonymous": nil, "legacy": {withBearer(legacyTok)}} {
		if status, body := read(doorway, opts...); status != 401 || strings.Contains(body, "Secret Project") {
			t.Errorf("%s doorway = %d %q", name, status, body)
		}
	}
	if status, body := read(doorway, withBearer(alexTok)); status != 200 || !strings.Contains(body, "Secret Project") {
		t.Errorf("member doorway = %d", status)
	}
	pass, _, _ := e.st.MintPass(ctx, ws.ID, alex.ID)
	for i := 0; i < 2; i++ { // Reading with the pass doesn't use it up.
		status, body := read(doorway + "?pass=" + pass)
		if status != 200 || !strings.Contains(body, "Secret Project") || !strings.Contains(body, "--pass "+pass) {
			t.Fatalf("doorway with a pass = %d %q", status, body)
		}
	}
	if _, _, _, err := e.st.JoinWithPass(ctx, ws.ID, pass, "Quill", ""); err != nil {
		t.Fatalf("join after reading the doorway: %v", err)
	}
	if status, body := read(doorway + "?pass=" + pass); status != 401 || strings.Contains(body, "Secret Project") {
		t.Errorf("doorway with a used pass = %d", status)
	}
	if status, _ := read(doorway + "?pass=up_bogus"); status != 401 {
		t.Errorf("doorway with a bogus pass = %d", status)
	}
}

func TestEventsAndStreamGated(t *testing.T) {
	e := newAuthEnv(t)
	ctx := t.Context()
	alex, _ := e.st.LoginWithProvider(ctx, "github", store.Profile{Subject: "alex", Name: "Alex"})
	ws, _, _ := e.st.CreateWorkspaceFor(ctx, alex.ID, "W", "", "")
	_, tok, _, _, _ := e.st.JoinAsUser(ctx, ws.ID, alex.ID, "Alex", "")
	_, legacyTok, _, _ := e.st.Join(ctx, ws.ID, "Old", "")
	base := e.ts.URL + "/api/workspaces/" + ws.ID
	for name, url := range map[string]string{
		"events, anonymous": base + "/events?after=0",
		"events, legacy":    base + "/events?after=0&token=" + legacyTok,
		"stream, anonymous": base + "/stream",
		"stream, legacy":    base + "/stream?token=" + legacyTok,
	} {
		resp, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Errorf("%s = %d", name, resp.StatusCode)
		}
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(base + "/stream?token=" + tok)
	if err != nil {
		t.Fatal(err)
	}
	line, _ := bufio.NewReader(resp.Body).ReadString('\n')
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(line, "ready") {
		t.Errorf("member stream = %d %q", resp.StatusCode, line)
	}
}

// TestOpenMode: UNISON_AUTH=off (local development without OAuth) is the
// old, open behaviour.
func TestOpenMode(t *testing.T) {
	e := newAuthEnv(t)
	e.srv.SetAuth(AuthConfig{Open: true})
	resp, ws := e.do("POST", "/api/workspaces", `{"name":"Dev"}`)
	if resp.StatusCode != 201 {
		t.Fatalf("anonymous create = %d", resp.StatusCode)
	}
	base := "/api/workspaces/" + ws["id"].(string)
	resp, joined := e.do("POST", base+"/join", `{"name":"Ada"}`)
	if resp.StatusCode != 201 {
		t.Fatalf("anonymous join = %d", resp.StatusCode)
	}
	if resp, _ := e.do("GET", base+"/backlog", ""); resp.StatusCode != 200 {
		t.Errorf("anonymous read = %d", resp.StatusCode)
	}
	if resp, _ := e.do("POST", base+"/messages", `{"body":"hi"}`, withBearer(joined["token"].(string))); resp.StatusCode != 201 {
		t.Errorf("unlinked participant posting = %d", resp.StatusCode)
	}
	if resp, list := e.doList("GET", "/api/workspaces"); resp.StatusCode != 200 || len(list) != 1 {
		t.Errorf("open list = %d %v", resp.StatusCode, list)
	}
	if resp, _ := e.do("GET", "/w/"+ws["id"].(string)+"/agent.md", ""); resp.StatusCode != 200 {
		t.Errorf("open doorway = %d", resp.StatusCode)
	}
}
