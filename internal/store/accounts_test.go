package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLoginLinksVerifiedEmails(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	gh, err := s.LoginWithProvider(ctx, "github", Profile{Subject: "1", Email: "Ada@Example.com", EmailVerified: true, Name: "Ada", AvatarURL: "https://a/1.png"})
	if err != nil {
		t.Fatal(err)
	}
	if gh.DisplayName != "Ada" || len(gh.Identities) != 1 || !gh.Identities[0].EmailVerified {
		t.Fatalf("github user = %+v", gh)
	}
	// Same provider subject: same user, profile refreshed.
	again, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "1", Email: "ada@example.com", EmailVerified: true, Name: "Ada L."})
	if again.ID != gh.ID || again.DisplayName != "Ada L." || again.AvatarURL != "https://a/1.png" {
		t.Errorf("re-login = %+v", again)
	}
	// Google with the same verified email (different case) joins that user.
	g, err := s.LoginWithProvider(ctx, "google", Profile{Subject: "g-1", Email: "ADA@example.com", EmailVerified: true})
	if err != nil {
		t.Fatal(err)
	}
	if g.ID != gh.ID || len(g.Identities) != 2 {
		t.Errorf("google login = %+v, want linked to user %d", g, gh.ID)
	}
	if emails := g.VerifiedEmails(); len(emails) != 2 || emails[0] != "ada@example.com" {
		t.Errorf("VerifiedEmails = %v", emails)
	}
	// An unverified email never links (it could be anyone's claim).
	u, _ := s.LoginWithProvider(ctx, "google", Profile{Subject: "g-2", Email: "ada@example.com", EmailVerified: false})
	if u.ID == gh.ID {
		t.Error("unverified email linked to an existing user")
	}
	// A verified email only matches other *verified* identities.
	s.LoginWithProvider(ctx, "github", Profile{Subject: "2", Email: "bob@example.com", EmailVerified: false})
	b, _ := s.LoginWithProvider(ctx, "google", Profile{Subject: "g-3", Email: "bob@example.com", EmailVerified: true})
	if len(b.Identities) != 1 {
		t.Errorf("verified login linked to an unverified identity: %+v", b)
	}
	if _, err := s.LoginWithProvider(ctx, "github", Profile{}); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty subject err = %v", err)
	}
}

func TestSessions(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	u, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "1", Name: "Ada"})
	token, se, err := s.CreateSession(ctx, u.ID, SessionCLI, "laptop")
	if err != nil || len(token) < 40 || se.ExpiresAt != now.UnixMilli()+SessionTTL.Milliseconds() {
		t.Fatalf("CreateSession = %q %+v %v", token, se, err)
	}
	var stored int
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE token_hash = ?`, token).Scan(&stored)
	if stored != 0 {
		t.Error("the raw token is stored")
	}
	got, gu, err := s.SessionByToken(ctx, token)
	if err != nil || got.ID != se.ID || gu.ID != u.ID {
		t.Fatalf("SessionByToken = %+v %+v %v", got, gu, err)
	}
	for _, bad := range []string{"", "us_nope", "0123456789abcdef0123456789abcdef"} {
		if _, _, err := s.SessionByToken(ctx, bad); !errors.Is(err, ErrNotFound) {
			t.Errorf("SessionByToken(%q) err = %v", bad, err)
		}
	}

	// Use within the slide window does not write; later use slides expiry.
	*now = now.Add(time.Minute)
	got, _, _ = s.SessionByToken(ctx, token)
	if got.LastSeenAt != se.LastSeenAt {
		t.Error("session slid before SessionSlideEvery")
	}
	*now = now.Add(80 * 24 * time.Hour)
	got, _, _ = s.SessionByToken(ctx, token)
	if got.ExpiresAt != now.UnixMilli()+SessionTTL.Milliseconds() {
		t.Errorf("expiry did not slide: %+v", got)
	}
	// Unused for longer than the TTL: expired.
	*now = now.Add(SessionTTL + time.Second)
	if _, _, err := s.SessionByToken(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired session err = %v", err)
	}

	t2, se2, _ := s.CreateSession(ctx, u.ID, SessionBrowser, "Firefox")
	list, _ := s.ListSessions(ctx, u.ID)
	if len(list) != 1 || list[0].ID != se2.ID {
		t.Errorf("ListSessions = %+v (expired sessions are hidden)", list)
	}
	if err := s.RevokeSession(ctx, u.ID+1, se2.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoking another user's session err = %v", err)
	}
	if err := s.RevokeSession(ctx, u.ID, se2.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SessionByToken(ctx, t2); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoked session err = %v", err)
	}
	if _, _, err := s.CreateSession(ctx, u.ID, "root", ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad kind err = %v", err)
	}
}

func TestOAuthStateSingleUse(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	s.SaveOAuthState(ctx, "state-1", OAuthState{Provider: "github", Verifier: "v", ReturnTo: "/w/x"})
	st, err := s.TakeOAuthState(ctx, "state-1")
	if err != nil || st.Verifier != "v" || st.ReturnTo != "/w/x" {
		t.Fatalf("TakeOAuthState = %+v, %v", st, err)
	}
	if _, err := s.TakeOAuthState(ctx, "state-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second take err = %v", err)
	}
	s.SaveOAuthState(ctx, "state-2", OAuthState{Provider: "google"})
	*now = now.Add(OAuthStateTTL + time.Second)
	if _, err := s.TakeOAuthState(ctx, "state-2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired state err = %v", err)
	}
}

func TestDeviceFlow(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	u, _ := s.LoginWithProvider(ctx, "github", Profile{Subject: "1"})
	d, err := s.StartDevice(ctx, 5*time.Second)
	if err != nil || len(d.UserCode) != 9 || d.UserCode[4] != '-' || d.Interval != 5 || d.ExpiresIn != 600 {
		t.Fatalf("StartDevice = %+v, %v", d, err)
	}
	if st, _, _ := s.PollDevice(ctx, d.DeviceCode, "cli"); st != DevicePending {
		t.Errorf("first poll = %s, want pending", st)
	}
	if st, _, _ := s.PollDevice(ctx, d.DeviceCode, "cli"); st != DeviceSlowDown {
		t.Errorf("immediate poll = %s, want slow_down", st)
	}
	if ok, _ := s.PendingDeviceCode(ctx, " "+d.UserCode[:4]+d.UserCode[5:]+" "); !ok {
		t.Error("PendingDeviceCode did not normalize the code")
	}
	// Approve with a lowercase, dash-less code.
	if err := s.DecideDevice(ctx, u.ID, d.UserCode[:4]+d.UserCode[5:], true); err != nil {
		t.Fatal(err)
	}
	if err := s.DecideDevice(ctx, u.ID, d.UserCode, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("deciding twice err = %v", err)
	}
	*now = now.Add(6 * time.Second)
	st, token, err := s.PollDevice(ctx, d.DeviceCode, "unison login")
	if err != nil || st != DeviceApproved || token == "" {
		t.Fatalf("approved poll = %s %q %v", st, token, err)
	}
	if se, gu, err := s.SessionByToken(ctx, token); err != nil || se.Kind != SessionCLI || gu.ID != u.ID {
		t.Errorf("CLI session = %+v %+v %v", se, gu, err)
	}
	if st, token, _ := s.PollDevice(ctx, d.DeviceCode, "cli"); st != DeviceExpired || token != "" {
		t.Errorf("poll after use = %s %q, want expired (single use)", st, token)
	}

	denied, _ := s.StartDevice(ctx, time.Second)
	s.DecideDevice(ctx, u.ID, denied.UserCode, false)
	if st, _, _ := s.PollDevice(ctx, denied.DeviceCode, "cli"); st != DeviceDenied {
		t.Errorf("denied poll = %s", st)
	}

	late, _ := s.StartDevice(ctx, time.Second)
	*now = now.Add(DeviceCodeTTL + time.Second)
	if st, _, _ := s.PollDevice(ctx, late.DeviceCode, "cli"); st != DeviceExpired {
		t.Errorf("expired poll = %s", st)
	}
	if err := s.DecideDevice(ctx, u.ID, late.UserCode, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("approving an expired code err = %v", err)
	}
	if st, _, _ := s.PollDevice(ctx, "ud_unknown", "cli"); st != DeviceExpired {
		t.Errorf("unknown device code = %s", st)
	}
}
