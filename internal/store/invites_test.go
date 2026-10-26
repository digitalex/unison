package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEmailInvites(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	alex, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "1", Name: "Alex"})
	ws, _, err := s.CreateWorkspaceFor(ctx, alex.ID, "W", "", "")
	if err != nil {
		t.Fatal(err)
	}
	bob, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "2", Name: "Bob", Email: "carol@example.com", EmailVerified: false})
	carol, _ := s.LoginWithProvider(ctx, "google", Profile{Subject: "3", Name: "Carol", Email: "Carol@Example.com", EmailVerified: true})

	if _, _, err := s.CreateEmailInvite(ctx, ws.ID, bob.ID, "x@example.com"); !errors.Is(err, ErrForbidden) {
		t.Errorf("non-member invite err = %v", err)
	}
	if _, _, err := s.CreateEmailInvite(ctx, ws.ID, alex.ID, "not an email"); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad email err = %v", err)
	}
	inv, evs, err := s.CreateEmailInvite(ctx, ws.ID, alex.ID, " CAROL@example.com ")
	if err != nil || inv.Kind != InviteEmail || inv.Email != "carol@example.com" || inv.CreatorName != "Alex" || !sameTypes(evs, "invite.created") {
		t.Fatalf("email invite = %+v %v %v", inv, eventTypes(evs), err)
	}
	if again, evs, _ := s.CreateEmailInvite(ctx, ws.ID, alex.ID, "carol@example.com"); again.ID != inv.ID || len(evs) != 0 {
		t.Errorf("inviting the same email again = %+v %v, want the same invite", again, eventTypes(evs))
	}

	// Bob claims that address but it isn't verified: no.
	if _, _, _, _, err := s.JoinWorkspace(ctx, ws.ID, bob.ID, "Bob", "", ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("unverified email join err = %v", err)
	}
	p, _, existing, evs, err := s.JoinWorkspace(ctx, ws.ID, carol.ID, "Carol", "", "")
	if err != nil || existing || p.UserID != carol.ID || !sameTypes(evs, "invite.used", "participant.joined", "member.added") {
		t.Fatalf("verified email join = %+v %v %v", p, eventTypes(evs), err)
	}
	if list, _ := s.ListInvites(ctx, ws.ID); len(list) != 0 {
		t.Errorf("used invite still listed: %+v", list)
	}
	// Now a member: joining again needs nothing and returns the participant.
	if again, _, existing, _, err := s.JoinWorkspace(ctx, ws.ID, carol.ID, "", "", ""); err != nil || !existing || again.ID != p.ID {
		t.Errorf("member rejoin = %+v %v %v", again, existing, err)
	}
	// Revoked email invites don't work.
	dave, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "4", Name: "Dave", Email: "dave@example.com", EmailVerified: true})
	dinv, _, _ := s.CreateEmailInvite(ctx, ws.ID, carol.ID, "dave@example.com") // any member may invite
	if _, evs, err := s.RevokeInvite(ctx, ws.ID, alex.ID, dinv.ID); err != nil || !sameTypes(evs, "invite.revoked") {
		t.Fatalf("revoke = %v %v", eventTypes(evs), err)
	}
	if _, _, _, _, err := s.JoinWorkspace(ctx, ws.ID, dave.ID, "Dave", "", ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("revoked invite join err = %v", err)
	}
	if _, _, err := s.RevokeInvite(ctx, ws.ID, dave.ID, dinv.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("non-member revoke err = %v", err)
	}
	// Email invites work for 48 hours; after that a new one is made.
	erin, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "5", Name: "Erin", Email: "erin@example.com", EmailVerified: true})
	einv, _, _ := s.CreateEmailInvite(ctx, ws.ID, alex.ID, "erin@example.com")
	if einv.ExpiresAt != now.UnixMilli()+(48*time.Hour).Milliseconds() {
		t.Errorf("email invite expires_at = %d, want 48h from now", einv.ExpiresAt)
	}
	*now = now.Add(48*time.Hour + time.Minute)
	if _, _, _, _, err := s.JoinWorkspace(ctx, ws.ID, erin.ID, "Erin", "", ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("expired email invite join err = %v", err)
	}
	if fresh, evs, _ := s.CreateEmailInvite(ctx, ws.ID, alex.ID, "erin@example.com"); fresh.ID == einv.ID || !sameTypes(evs, "invite.created") {
		t.Errorf("re-inviting after expiry = %+v, want a new invite", fresh)
	}
}

func TestLinkInvites(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	alex, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "1", Name: "Alex"})
	ws, _, _ := s.CreateWorkspaceFor(ctx, alex.ID, "W", "", "")
	other, _, _ := s.CreateWorkspaceFor(ctx, alex.ID, "Other", "", "")
	users := make([]*User, 4)
	for i := range users {
		users[i], _ = s.LoginWithProvider(ctx, "github", Profile{Subject: string(rune('a' + i))})
	}

	inv, token, evs, err := s.CreateLinkInvite(ctx, ws.ID, alex.ID)
	if err != nil || inv.Kind != InviteLink || inv.ExpiresAt != now.UnixMilli()+(48*time.Hour).Milliseconds() || !sameTypes(evs, "invite.created") || token == "" {
		t.Fatalf("link invite = %+v %q %v %v", inv, token, eventTypes(evs), err)
	}
	if l, err := s.LookupInviteLink(ctx, token); err != nil || l.WorkspaceID != ws.ID || l.WorkspaceName != "W" {
		t.Errorf("lookup = %+v %v", l, err)
	}
	// Multi-use.
	for _, u := range users[:2] {
		if _, _, _, _, err := s.JoinWorkspace(ctx, ws.ID, u.ID, "", "", token); err != nil {
			t.Fatalf("join with link: %v", err)
		}
	}
	if list, _ := s.ListInvites(ctx, ws.ID); len(list) != 1 || list[0].Uses != 2 {
		t.Errorf("invites after two uses = %+v", list)
	}
	if _, _, _, _, err := s.JoinWorkspace(ctx, other.ID, users[2].ID, "", "", token); !errors.Is(err, ErrForbidden) {
		t.Errorf("link for another workspace err = %v", err)
	}
	if _, _, _, _, err := s.JoinWorkspace(ctx, ws.ID, users[2].ID, "", "", "ui_bogus"); !errors.Is(err, ErrForbidden) {
		t.Errorf("bogus link err = %v", err)
	}
	// Expiry.
	*now = now.Add(48*time.Hour + time.Minute)
	if _, err := s.LookupInviteLink(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired lookup err = %v", err)
	}
	if _, _, _, _, err := s.JoinWorkspace(ctx, ws.ID, users[2].ID, "", "", token); !errors.Is(err, ErrForbidden) {
		t.Errorf("expired link err = %v", err)
	}
	// Revocation.
	inv2, token2, _, _ := s.CreateLinkInvite(ctx, ws.ID, alex.ID)
	s.RevokeInvite(ctx, ws.ID, alex.ID, inv2.ID)
	if _, _, _, _, err := s.JoinWorkspace(ctx, ws.ID, users[3].ID, "", "", token2); !errors.Is(err, ErrForbidden) {
		t.Errorf("revoked link err = %v", err)
	}
}

func TestJoinRules(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	alex, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "1", Name: "Alex"})
	bob, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "2", Name: "Bob"})
	stranger, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "3"})
	ws, _, _ := s.CreateWorkspaceFor(ctx, alex.ID, "W", "", "")
	mine, _, _ := s.CreateWorkspaceFor(ctx, bob.ID, "Bob's", "", "")

	if list, _ := s.ListWorkspacesFor(ctx, alex.ID); len(list) != 1 || list[0].ID != ws.ID {
		t.Errorf("Alex's workspaces = %+v", list)
	}
	if list, _ := s.ListWorkspacesFor(ctx, stranger.ID); len(list) != 0 {
		t.Errorf("a stranger's workspaces = %+v", list)
	}
	// The creator is a member and joins without an invite.
	if _, _, _, _, err := s.JoinWorkspace(ctx, ws.ID, alex.ID, "Alex", "", ""); err != nil {
		t.Fatalf("creator join: %v", err)
	}
	if _, _, _, _, err := s.JoinWorkspace(ctx, ws.ID, stranger.ID, "X", "", ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("uninvited join err = %v", err)
	}
	if _, _, _, _, err := s.JoinWorkspace(ctx, "ws_missing", alex.ID, "X", "", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown workspace err = %v", err)
	}
	// A removed member needs a fresh invite (#15 note).
	_, tok, _, _ := s.CreateLinkInvite(ctx, ws.ID, alex.ID)
	s.JoinWorkspace(ctx, ws.ID, bob.ID, "Bob", "", tok)
	s.RemoveMember(ctx, ws.ID, alex.ID, bob.ID)
	if _, _, _, _, err := s.JoinWorkspace(ctx, ws.ID, bob.ID, "", "", ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("removed member rejoin err = %v", err)
	}
	_, fresh, _, _ := s.CreateLinkInvite(ctx, ws.ID, alex.ID)
	p, _, existing, _, err := s.JoinWorkspace(ctx, ws.ID, bob.ID, "", "", fresh)
	if err != nil || !existing || p.Name != "Bob" {
		t.Errorf("rejoin with a fresh invite = %+v %v %v (gets the old participant back)", p, existing, err)
	}
	_ = mine
}

func TestCheckPass(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	alex, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "1", Name: "Alex"})
	ws, _, _ := s.CreateWorkspaceFor(ctx, alex.ID, "W", "", "")
	other, _, _ := s.CreateWorkspaceFor(ctx, alex.ID, "Other", "", "")
	pass, _, _ := s.MintPass(ctx, ws.ID, alex.ID)
	for i := 0; i < 2; i++ { // Checking does not use it up.
		if err := s.CheckPass(ctx, ws.ID, pass); err != nil {
			t.Fatalf("check %d: %v", i, err)
		}
	}
	if err := s.CheckPass(ctx, other.ID, pass); !errors.Is(err, ErrForbidden) {
		t.Errorf("pass for another workspace err = %v", err)
	}
	if _, _, _, err := s.JoinWithPass(ctx, ws.ID, pass, "Quill", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckPass(ctx, ws.ID, pass); !errors.Is(err, ErrForbidden) {
		t.Errorf("used pass err = %v", err)
	}
	if err := s.CheckPass(ctx, ws.ID, "up_bogus"); !errors.Is(err, ErrForbidden) {
		t.Errorf("bogus pass err = %v", err)
	}
}
