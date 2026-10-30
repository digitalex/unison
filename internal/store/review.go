package store

import (
	"context"
	"fmt"
)

// App Review sign-in (#139): Apple's reviewers can't sign in with their own
// GitHub or Google accounts, so a review-only access code (a server secret,
// checked by the server) signs into one dedicated account. That account has
// its own small demo workspace, seeded the first time and again if it was
// deleted; it can't see anyone else's workspaces.

// ReviewProvider is the identity provider name of the App Review account.
const ReviewProvider = "review"

// ReviewLogin signs into the App Review account, creating it and its demo
// workspace if needed, and returns a new app session's token (the same kind
// of session as RedeemAppCode's). The caller checks the access code.
func (s *Store) ReviewLogin(ctx context.Context, label string) (string, error) {
	u, err := s.LoginWithProvider(ctx, ReviewProvider, Profile{Subject: "app-review", Name: "App Review"})
	if err != nil {
		return "", err
	}
	list, err := s.ListWorkspacesFor(ctx, u.ID)
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		if err := s.seedDemoWorkspace(ctx, u.ID); err != nil {
			return "", fmt.Errorf("seeding the demo workspace: %w", err)
		}
	}
	var token string
	_, err = s.tx(ctx, func(t *tx, log *eventLog) error {
		var err error
		token, _, err = createSession(t, u.ID, SessionApp, label, log.now)
		return err
	})
	return token, err
}

// seedDemoWorkspace creates a workspace for userID with a little of
// everything: queued items, chat (with a thread), and a recurring task.
func (s *Store) seedDemoWorkspace(ctx context.Context, userID int64) error {
	ws, _, err := s.CreateWorkspaceFor(ctx, userID, "Demo workspace", "", "")
	if err != nil {
		return err
	}
	p, _, _, _, err := s.JoinAsUser(ctx, ws.ID, userID, "AppReview", "🧪")
	if err != nil {
		return err
	}
	items := []struct{ title, body string }{
		{"Write the release notes", "Summarize what changed since the last version, in plain language."},
		{"Fix the login button on small screens", "It overlaps the logo on iPhone SE."},
		{"Plan next week's work", "Pick the top three items with the team."},
	}
	for _, it := range items {
		if _, _, err := s.AddItem(ctx, p, it.title, it.body, 0); err != nil {
			return err
		}
	}
	m, _, err := s.PostMessage(ctx, p, "Welcome to Unison! This demo workspace has a backlog, this chat, and a recurring task. Agents you invite work here too.", nil, nil)
	if err != nil {
		return err
	}
	if _, _, err := s.PostMessage(ctx, p, "Replies like this one form a thread.", &m.ID, nil); err != nil {
		return err
	}
	_, _, err = s.AddSchedule(ctx, p, ScheduleInput{Title: "Check yesterday's crash reports", Cadence: CadenceDaily})
	return err
}
