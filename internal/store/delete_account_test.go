package store

import (
	"context"
	"errors"
	"testing"
)

func TestDeleteAccount(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	ada, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "1", Name: "Ada", Email: "ada@example.com", EmailVerified: true})
	bo, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "2", Name: "Bo"})
	adaTok, adaSession, _ := s.CreateSession(ctx, ada.ID, SessionCLI, "")
	if _, err := s.RegisterDevice(ctx, ada.ID, adaSession.ID, "a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90", "iPhone"); err != nil {
		t.Fatal(err)
	}

	// Ada's own workspace, and one shared with Bo where Ada has an agent
	// and someone already goes by "DeletedUser".
	solo, _, _ := s.CreateWorkspace(ctx, "Solo", "", "")
	s.JoinAsUser(ctx, solo.ID, ada.ID, "Ada", "")
	shared, _, _ := s.CreateWorkspace(ctx, "Shared", "", "")
	adaP, adaPTok, _, _, _ := s.JoinAsUser(ctx, shared.ID, ada.ID, "Ada", "")
	s.JoinAsUser(ctx, shared.ID, bo.ID, "Bo", "")
	pass, _, _ := s.MintPass(ctx, shared.ID, ada.ID)
	agent, agentTok, _, err := s.JoinWithPass(ctx, shared.ID, pass, "Quill", "")
	if err != nil {
		t.Fatal(err)
	}
	boPass, _, _ := s.MintPass(ctx, shared.ID, bo.ID)
	if _, _, _, err := s.JoinWithPass(ctx, shared.ID, boPass, "DeletedUser", ""); err != nil {
		t.Fatal(err)
	}
	msg, _, _ := s.Post(ctx, adaP, NewMessage{Body: "hello from Ada"})
	unusedPass, _, _ := s.MintPass(ctx, shared.ID, ada.ID)

	got, evs, err := s.DeleteAccount(ctx, ada.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.DeletedWorkspaces) != 1 || got.DeletedWorkspaces[0] != solo.ID || len(got.LeftWorkspaces) != 1 || got.LeftWorkspaces[0] != shared.ID {
		t.Fatalf("deletion = %+v", got)
	}
	if !sameTypes(evs, "member.removed", "participant.revoked", "participant.revoked") {
		t.Errorf("events = %v", eventTypes(evs))
	}

	// The solo workspace is gone; the shared one keeps Ada's message.
	if _, err := s.GetWorkspace(ctx, solo.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("solo workspace: %v, want gone", err)
	}
	msgs, _ := s.ListMessages(ctx, shared.ID, 50)
	if len(msgs) == 0 || msgs[len(msgs)-1].ID != msg.ID {
		t.Errorf("Ada's message should stay in the shared workspace: %+v", msgs)
	}

	// Ada's participant and agent are renamed and stop working.
	for _, c := range []struct {
		token, name string
		id          int64
	}{{adaPTok, "DeletedUser2", adaP.ID}, {agentTok, "DeletedUser3", agent.ID}} {
		if _, err := s.Authenticate(ctx, c.token); err == nil {
			t.Errorf("participant %d still authenticates", c.id)
		}
		var name, emoji string
		var revoked *int64
		s.db.QueryRowContext(ctx, `SELECT name, emoji, revoked_at FROM participants WHERE id = ?`, c.id).Scan(&name, &emoji, &revoked)
		if name != c.name || emoji != "👤" || revoked == nil {
			t.Errorf("participant %d = %q %q revoked %v, want %q 👤 revoked", c.id, name, emoji, revoked, c.name)
		}
	}

	// Signing in, sessions, devices and the unused pass are gone; the user
	// row stays but holds nothing.
	if _, _, err := s.SessionByToken(ctx, adaTok); err == nil {
		t.Error("Ada's session still works")
	}
	if d, _ := s.ListDevices(ctx, ada.ID); len(d) != 0 {
		t.Errorf("devices = %+v", d)
	}
	u, err := s.GetUser(ctx, ada.ID)
	if err != nil || u.DisplayName != "" || u.AvatarURL != "" || len(u.Identities) != 0 {
		t.Errorf("user = %+v %v, want an empty row", u, err)
	}
	if _, _, _, err := s.JoinWithPass(ctx, shared.ID, unusedPass, "Late", ""); err == nil {
		t.Error("Ada's unused pass still works")
	}
	// Signing in with the same GitHub account makes a new, empty account.
	again, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "1", Name: "Ada"})
	if again.ID == ada.ID {
		t.Error("signing in again revived the deleted account")
	}

	// Bo is untouched; an unknown user is not found.
	if m, _ := s.ListMembers(ctx, shared.ID); len(m.Members) != 1 || m.Members[0].UserID != bo.ID {
		t.Errorf("members = %+v, want just Bo", m)
	}
	if _, _, err := s.DeleteAccount(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown user: %v", err)
	}
}
