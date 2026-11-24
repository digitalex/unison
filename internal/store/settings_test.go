package store

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

func TestProvidersAndUserSettings(t *testing.T) {
	if got := ProviderKeys(); !slices.Equal(got, []string{ProviderIsocan, ProviderGitHub}) {
		t.Fatalf("ProviderKeys = %v", got)
	}
	norm, err := NormalizeProviders([]string{"GITHUB", "isocan", "github"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(norm, []string{ProviderIsocan, ProviderGitHub}) {
		t.Fatalf("NormalizeProviders = %v", norm)
	}
	if _, err := NormalizeProviders([]string{"seamus"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown provider: got %v, want ErrInvalid", err)
	}
	if got := FilterProviders([]string{"unknown", "github", "isocan"}); !slices.Equal(got, []string{ProviderIsocan, ProviderGitHub}) {
		t.Fatalf("FilterProviders = %v", got)
	}

	s, _ := newStore(t)
	ctx := context.Background()
	u, err := s.LoginWithProvider(ctx, "github", Profile{Subject: "gh-u1", Name: "Alex", Email: "alex@example.com", EmailVerified: true})
	if err != nil {
		t.Fatal(err)
	}

	// Default settings are empty.
	got, err := s.GetUserSettings(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.NewProjectIntegrations) != 0 {
		t.Fatalf("default settings = %+v, want empty", got)
	}

	// Set integrations (deduplicated & ordered).
	got, err = s.SetUserSettings(ctx, u.ID, map[string]json.RawMessage{
		SettingNewProjectIntegrations: json.RawMessage(`["github","isocan","github"]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.NewProjectIntegrations, []string{ProviderIsocan, ProviderGitHub}) {
		t.Fatalf("after set = %+v", got)
	}

	// Unknown key fails atomically without changing existing values.
	if _, err := s.SetUserSettings(ctx, u.ID, map[string]json.RawMessage{
		SettingNewProjectIntegrations: json.RawMessage(`[]`),
		"unknown.key":                 json.RawMessage(`true`),
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown key = %v, want ErrInvalid", err)
	}
	ints, err := s.NewProjectIntegrations(ctx, u.ID)
	if err != nil || !slices.Equal(ints, []string{ProviderIsocan, ProviderGitHub}) {
		t.Fatalf("settings mutated after failed set: %v, %v", ints, err)
	}

	// Setting back to empty clears the row.
	got, err = s.SetUserSettings(ctx, u.ID, map[string]json.RawMessage{
		SettingNewProjectIntegrations: json.RawMessage(`[]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.NewProjectIntegrations) != 0 {
		t.Fatalf("after clear = %+v, want empty", got)
	}

	// DeleteAccount removes user_settings rows cleanly.
	if _, err := s.SetUserSettings(ctx, u.ID, map[string]json.RawMessage{
		SettingNewProjectIntegrations: json.RawMessage(`["isocan"]`),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.DeleteAccount(ctx, u.ID); err != nil {
		t.Fatalf("DeleteAccount with user_settings: %v", err)
	}
}
