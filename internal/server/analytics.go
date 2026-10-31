package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/digitalex/unison/internal/analytics"
	"github.com/digitalex/unison/internal/store"
)

// Key actions go to Google Analytics from here rather than the browser, so
// agents count too (#155). Only the event's name and whether a human or an
// agent did it are sent, under a hash of the account or participant ID: never
// names, emails, workspace IDs or anything anyone wrote. Copying the agent
// join instructions has no request, so the web UI sends that one itself.
const (
	eventJoined        = "workspace_joined"
	eventMessagePosted = "chat_message_posted"
	eventReacted       = "chat_reaction_added"
	eventItemCreated   = "backlog_item_created"
	eventItemClosed    = "backlog_item_closed"
	eventItemAssigned  = "backlog_item_assigned"
	eventPassGenerated = "agent_pass_generated"
	eventInviteLink    = "invite_link_generated"
	eventRepoAdded     = "github_repo_added"
)

// analyticsActor is who did something, as analytics sees them.
type analyticsActor struct{ id, kind string }

// participantActor is a participant: an agent unless it's linked to an
// account of its own.
func participantActor(p *store.Participant) analyticsActor {
	if p.UserID != 0 && p.Sponsor == nil {
		return userActor(p.UserID)
	}
	return analyticsActor{fmt.Sprintf("participant:%d", p.ID), "agent"}
}

// userActor is a signed-in account, which is always a human.
func userActor(id int64) analyticsActor {
	return analyticsActor{fmt.Sprintf("user:%d", id), "human"}
}

// SetTracker enables analytics through t (nil disables them).
func (s *Server) SetTracker(t analytics.Sender) { s.tracker = t }

// track sends an event in the background; a failure only logs.
func (s *Server) track(name string, a analyticsActor) {
	if s.tracker == nil {
		return
	}
	sum := sha256.Sum256([]byte(a.id))
	clientID := hex.EncodeToString(sum[:16])
	s.tracking.Add(1)
	go func() {
		defer s.tracking.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.tracker.Send(ctx, clientID, name, map[string]string{"actor_type": a.kind}); err != nil {
			log.Printf("analytics: %s: %v", name, err)
		}
	}()
}

// trackRepos tracks the GitHub repositories that evs added.
func (s *Server) trackRepos(evs []store.Event, a analyticsActor) {
	for _, e := range evs {
		if e.Type != "repo.added" {
			continue
		}
		var repo store.Repo
		if err := json.Unmarshal(e.Data, &repo); err == nil && strings.Contains(strings.ToLower(repo.URL), "github.com") {
			s.track(eventRepoAdded, a)
		}
	}
}

func (s *Server) waitTracking() { s.tracking.Wait() }
