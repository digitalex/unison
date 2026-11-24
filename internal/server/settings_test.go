package server

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

func TestUserSettingsAPI(t *testing.T) {
	ts, st := newServer(t)
	ctx := t.Context()

	u, err := st.LoginWithProvider(ctx, "github", store.Profile{Subject: "gh-set", Name: "Alex", Email: "alex@example.com", EmailVerified: true})
	if err != nil {
		t.Fatal(err)
	}
	userTok, _, err := st.CreateSession(ctx, u.ID, store.SessionCLI, "test")
	if err != nil {
		t.Fatal(err)
	}
	ws, _, err := st.CreateWorkspaceFor(ctx, u.ID, "Unison", "", "")
	if err != nil {
		t.Fatal(err)
	}
	pass, _, err := st.MintPass(ctx, ws.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, agentTok, _, err := st.JoinWithPass(ctx, ws.ID, pass, "Ada", "🦊")
	if err != nil {
		t.Fatal(err)
	}

	// Unauthenticated gets 401.
	if code, _ := doJSON(t, "GET", ts.URL+"/api/me/settings", "", ""); code != 401 {
		t.Fatalf("unauth GET /api/me/settings = %d, want 401", code)
	}

	// Agent token gets 403 on both GET and PUT.
	if code, _ := doJSON(t, "GET", ts.URL+"/api/me/settings", agentTok, ""); code != 403 {
		t.Fatalf("agent GET /api/me/settings = %d, want 403", code)
	}
	if code, _ := doJSON(t, "PUT", ts.URL+"/api/me/settings", agentTok, `{"new_project.integrations":["isocan"]}`); code != 403 {
		t.Fatalf("agent PUT /api/me/settings = %d, want 403", code)
	}

	// Person GET starts empty.
	code, raw := doJSON(t, "GET", ts.URL+"/api/me/settings", userTok, "")
	if code != 200 {
		t.Fatalf("GET /api/me/settings = %d: %s", code, raw)
	}
	var got store.UserSettings
	if err := json.Unmarshal(raw, &got); err != nil || len(got.NewProjectIntegrations) != 0 {
		t.Fatalf("initial settings = %+v, err=%v", got, err)
	}

	// Person PUT updates and normalizes order.
	code, raw = doJSON(t, "PUT", ts.URL+"/api/me/settings", userTok, `{"new_project.integrations":["github","isocan"]}`)
	if code != 200 {
		t.Fatalf("PUT /api/me/settings = %d: %s", code, raw)
	}
	if err := json.Unmarshal(raw, &got); err != nil || !slices.Equal(got.NewProjectIntegrations, []string{"isocan", "github"}) {
		t.Fatalf("updated settings = %+v, err=%v", got, err)
	}

	// Invalid provider returns 400.
	if code, _ := doJSON(t, "PUT", ts.URL+"/api/me/settings", userTok, `{"new_project.integrations":["bogus"]}`); code != 400 {
		t.Fatalf("invalid provider = %d, want 400", code)
	}
}
