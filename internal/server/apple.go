package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/digitalex/unison/internal/store"
)

// Sign in with Apple (#136). Apple differs from GitHub and Google in four
// ways: the client secret is a short-lived JWT signed with the team's key,
// the callback is a form POST (response_mode=form_post, needed for the
// name and email scopes), the profile is the token response's id_token,
// and the name comes only once, as the callback's "user" field on the
// first sign-in. The iPhone app's native button gets a code for the App
// ID instead, which it redeems at POST /api/auth/apple/native.

const appleIssuer = "https://appleid.apple.com"

// AppleKey signs client secrets for Sign in with Apple.
type AppleKey struct {
	TeamID string
	KeyID  string
	Key    *ecdsa.PrivateKey
	// AppID is the iPhone app's bundle ID: the client ID of codes from
	// its native button.
	AppID string
}

// AppleProvider returns Apple's endpoints for the Services ID clientID,
// signing with the .p8 key (PEM) keyPEM.
func AppleProvider(clientID, teamID, keyID, keyPEM, appID string) (*OAuthProvider, error) {
	block, _ := pem.Decode([]byte(strings.ReplaceAll(keyPEM, `\n`, "\n")))
	if block == nil {
		return nil, errors.New("the Apple key is not a PEM (.p8) key")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("the Apple key: %w", err)
	}
	ec, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("the Apple key is not an EC key")
	}
	return &OAuthProvider{
		Name: "apple", Label: "Apple", ClientID: clientID,
		AuthURL:  appleIssuer + "/auth/authorize",
		TokenURL: appleIssuer + "/auth/token",
		Scopes:   []string{"name", "email"},
		Apple:    &AppleKey{TeamID: teamID, KeyID: keyID, Key: ec, AppID: appID},
	}, nil
}

// clientSecret is the ES256 JWT Apple takes as client_secret for clientID.
func (k *AppleKey) clientSecret(clientID string, now time.Time) (string, error) {
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	input := enc(map[string]string{"alg": "ES256", "kid": k.KeyID}) + "." + enc(map[string]any{
		"iss": k.TeamID, "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(), "aud": appleIssuer, "sub": clientID,
	})
	h := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, k.Key, h[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return input + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// appleProfile redeems an authorization code issued to clientID (the
// Services ID, or the App ID for the native button) and reads the profile
// from the id_token. redirect is empty for native codes.
func (s *Server) appleProfile(ctx context.Context, p *OAuthProvider, code, clientID, redirect string) (*store.Profile, error) {
	if code == "" {
		return nil, errors.New("no authorization code")
	}
	secret, err := p.Apple.clientSecret(clientID, time.Now())
	if err != nil {
		return nil, err
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID}, "client_secret": {secret}}
	if redirect != "" {
		form.Set("redirect_uri", redirect)
	}
	var tok struct {
		IDToken string `json:"id_token"`
		Error   string `json:"error"`
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := s.providerJSON(req, &tok); err != nil {
		return nil, err
	}
	if tok.IDToken == "" {
		return nil, fmt.Errorf("token exchange failed: %s", firstNonEmpty(tok.Error, "no id_token"))
	}
	return appleClaims(tok.IDToken, clientID)
}

// appleClaims reads the profile from an id_token. It came straight from
// Apple's token endpoint over TLS, in exchange for our signed secret, so
// (as OpenID Connect allows) its signature isn't checked; its issuer and
// audience are.
func appleClaims(idToken, clientID string) (*store.Profile, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed id_token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("malformed id_token")
	}
	var c struct {
		Iss           string `json:"iss"`
		Aud           string `json:"aud"`
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified any    `json:"email_verified"` // bool or "true"
	}
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, errors.New("malformed id_token")
	}
	if c.Iss != appleIssuer || c.Aud != clientID || c.Sub == "" {
		return nil, errors.New("the id_token is not for this server")
	}
	verified := c.EmailVerified == true || c.EmailVerified == "true"
	return &store.Profile{Subject: c.Sub, Email: c.Email, EmailVerified: verified}, nil
}

// appleName is the name in the callback's "user" field (first sign-in only):
// {"name": {"firstName": …, "lastName": …}, "email": …}.
func appleName(user string) string {
	var u struct {
		Name struct {
			FirstName string `json:"firstName"`
			LastName  string `json:"lastName"`
		} `json:"name"`
	}
	_ = json.Unmarshal([]byte(user), &u)
	return strings.TrimSpace(u.Name.FirstName + " " + u.Name.LastName)
}

// appleNative signs in with a code from the iPhone app's native Sign in
// with Apple button and returns an app session token, as appToken does.
// The app sends the name, which Apple gives it (not us) on first sign-in.
func (s *Server) appleNative(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code       string `json:"code"`
		GivenName  string `json:"given_name"`
		FamilyName string `json:"family_name"`
		Label      string `json:"label"`
	}
	if !decode(w, r, &req) {
		return
	}
	p := s.provider("apple")
	if p == nil || p.Apple.AppID == "" {
		writeError(w, http.StatusNotFound, "sign-in with Apple is not configured on this server")
		return
	}
	prof, err := s.appleProfile(r.Context(), p, req.Code, p.Apple.AppID, "")
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not sign in with Apple: "+err.Error())
		return
	}
	prof.Name = strings.TrimSpace(req.GivenName + " " + req.FamilyName)
	u, err := s.store.LoginWithProvider(r.Context(), p.Name, *prof)
	if err != nil {
		storeError(w, err)
		return
	}
	token, _, err := s.store.CreateSession(r.Context(), u.ID, store.SessionApp, firstNonEmpty(req.Label, "app"))
	s.respond(w, http.StatusOK, map[string]any{"token": token}, nil, err)
}
