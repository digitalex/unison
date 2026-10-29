package store

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

const (
	tokA = "a0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf"
	tokB = "c0c1c2c3c4c5c6c7c8c9cacbcccdcecfd0d1d2d3d4d5d6d7d8d9dadbdcdddedf"
)

func deviceIDs(t *testing.T, s *Store, userID int64) []int64 {
	t.Helper()
	list, err := s.ListDevices(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, d := range list {
		ids = append(ids, d.ID)
	}
	return ids
}

// TestDevices: registering upserts on the token (moving it between
// accounts), and deleting is limited to your own devices.
func TestDevices(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	alex, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "1"})
	bob, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "2"})
	_, alexSess, _ := s.CreateSession(ctx, alex.ID, SessionCLI, "")
	_, bobSess, _ := s.CreateSession(ctx, bob.ID, SessionCLI, "")

	if _, err := s.RegisterDevice(ctx, alex.ID, alexSess.ID, "not hex!", "x"); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad token err = %v", err)
	}
	d1, err := s.RegisterDevice(ctx, alex.ID, alexSess.ID, strings.ToUpper(tokA), " iPhone ")
	if err != nil {
		t.Fatal(err)
	}
	if d1.UserID != alex.ID || d1.APNSToken != tokA || d1.Label != "iPhone" || d1.CreatedAt == 0 {
		t.Errorf("registered %+v", d1)
	}
	again, err := s.RegisterDevice(ctx, alex.ID, alexSess.ID, tokA, "Alex's iPhone")
	if err != nil || again.ID != d1.ID || again.Label != "Alex's iPhone" {
		t.Errorf("re-register = %+v, %v; want the same device relabelled", again, err)
	}
	d2, _ := s.RegisterDevice(ctx, alex.ID, alexSess.ID, tokB, "iPad")
	if got := deviceIDs(t, s, alex.ID); !slices.Equal(got, []int64{d1.ID, d2.ID}) {
		t.Errorf("alex's devices = %v", got)
	}

	moved, err := s.RegisterDevice(ctx, bob.ID, bobSess.ID, tokB, "Bob's iPad")
	if err != nil || moved.ID != d2.ID || moved.UserID != bob.ID {
		t.Fatalf("reassign = %+v, %v", moved, err)
	}
	if got := deviceIDs(t, s, alex.ID); !slices.Equal(got, []int64{d1.ID}) {
		t.Errorf("after reassign alex has %v", got)
	}
	if got := deviceIDs(t, s, bob.ID); !slices.Equal(got, []int64{d2.ID}) {
		t.Errorf("after reassign bob has %v", got)
	}

	if err := s.DeleteDevice(ctx, alex.ID, d2.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting another account's device: %v", err)
	}
	if err := s.DeleteDevice(ctx, alex.ID, d1.ID); err != nil {
		t.Errorf("deleting own device: %v", err)
	}
	if err := s.DeleteDevice(ctx, alex.ID, d1.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting it twice: %v", err)
	}
	if got := deviceIDs(t, s, bob.ID); len(got) != 1 {
		t.Errorf("bob's device went too: %v", got)
	}

	if err := s.TouchDevice(ctx, d2.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.ListDevices(ctx, bob.ID); list[0].LastUsedAt == 0 {
		t.Error("TouchDevice did not set last_used_at")
	}
	if err := s.ForgetDevice(ctx, d2.ID); err != nil || len(deviceIDs(t, s, bob.ID)) != 0 {
		t.Errorf("ForgetDevice: %v", err)
	}
}

// TestSignOutRemovesDevices: revoking a session drops the devices it
// registered, and only those.
func TestSignOutRemovesDevices(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	alex, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "1"})
	_, phone, _ := s.CreateSession(ctx, alex.ID, SessionCLI, "phone")
	_, tablet, _ := s.CreateSession(ctx, alex.ID, SessionCLI, "tablet")
	_, web, _ := s.CreateSession(ctx, alex.ID, SessionBrowser, "web")
	dPhone, _ := s.RegisterDevice(ctx, alex.ID, phone.ID, tokA, "phone")
	dTablet, _ := s.RegisterDevice(ctx, alex.ID, tablet.ID, tokB, "tablet")

	if err := s.RevokeSession(ctx, alex.ID, phone.ID); err != nil {
		t.Fatal(err)
	}
	if got := deviceIDs(t, s, alex.ID); !slices.Equal(got, []int64{dTablet.ID}) {
		t.Errorf("after signing out the phone: %v, want [%d] (not %d)", got, dTablet.ID, dPhone.ID)
	}
	if _, err := s.RevokeOtherSessions(ctx, alex.ID, web.ID); err != nil {
		t.Fatal(err)
	}
	if got := deviceIDs(t, s, alex.ID); len(got) != 0 {
		t.Errorf("after signing out everywhere else: %v", got)
	}
}

// TestPushRecipients: accounts asked to act, then accounts mentioned; each
// once, never the author's, never a former member's.
func TestPushRecipients(t *testing.T) {
	f := newActionFixture(t)
	ctx := context.Background()
	alexU, bobU := f.alex.UserID, f.bob.UserID
	check := func(what string, author *Participant, body string, actionFor []int64, wantAction, wantMention []int64) {
		t.Helper()
		action, mention, err := f.s.PushRecipients(ctx, f.ws.ID, author.ID, body, actionFor)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(action, wantAction) || !slices.Equal(mention, wantMention) {
			t.Errorf("%s: action %v mention %v; want %v, %v", what, action, mention, wantAction, wantMention)
		}
	}
	check("mention", f.bob, "hi @alex", nil, nil, []int64{alexU})
	check("own account", f.alex, "@Alex look", nil, nil, nil)
	check("action wins", f.bob, "@Alex do it", []int64{f.alex.ID}, []int64{alexU}, nil)
	check("action and mention", f.alex, "@Bob please, cc @nobody", []int64{f.bob.ID}, []int64{bobU}, nil)
	check("agent author notifies its sponsor", f.wren, "@Alex done", nil, nil, []int64{alexU})
	check("agents have no account", f.alex, "@Wren go", nil, nil, nil)

	if _, err := f.s.RemoveMember(ctx, f.ws.ID, alexU, bobU); err != nil {
		t.Fatal(err)
	}
	check("former member", f.alex, "@Bob still there?", nil, nil, nil)
}
