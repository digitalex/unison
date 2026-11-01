package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/digitalex/unison/internal/store"
)

// activity prints the workspace's recent activity feed (GET .../activity),
// oldest first, matching the activity pills shown in the web chat view.
func (a *app) activity(args []string) error {
	fs := flag.NewFlagSet("activity", flag.ContinueOnError)
	limit := fs.Int("limit", 200, "maximum number of recent activity events")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fmt.Errorf("%w: activity [--limit N]", errUsage)
	}
	if *limit <= 0 {
		return fmt.Errorf("%w: --limit must be positive", errUsage)
	}
	var evs []store.Event
	if ok, err := a.call("GET", fmt.Sprintf("/activity?limit=%d", *limit), nil, &evs, false); !ok {
		return err
	}
	if len(evs) == 0 {
		a.printf("No activity yet.\n")
		return nil
	}
	people, err := a.participants()
	if err != nil {
		return err
	}
	for _, e := range evs {
		when := time.UnixMilli(e.CreatedAt).Format("15:04")
		a.printf("[%s] %s\n", when, describeActivity(people, e))
	}
	return nil
}

func describeActivity(people map[int64]store.Participant, e store.Event) string {
	who := label(people, e.ActorID)
	if e.ActorID == 0 && e.Type == "backlog.added" {
		who = "🔁 Recurring task"
	}
	switch {
	case strings.HasPrefix(e.Type, "backlog."):
		return describeBacklogActivity(people, who, e)
	case e.Type == "participant.joined":
		return describeParticipantJoined(e)
	case e.Type == "member.removed":
		return describeMemberRemoved(people, who, e)
	default:
		return fmt.Sprintf("%s by %s", e.Type, who)
	}
}

func describeBacklogActivity(people map[int64]store.Participant, who string, e store.Event) string {
	var it store.Item
	json.Unmarshal(e.Data, &it)
	verb := strings.TrimPrefix(e.Type, "backlog.")
	line := fmt.Sprintf("📋 %s %s #%d %q", who, verb, it.ID, it.Title)
	switch e.Type {
	case "backlog.added":
		if it.AssigneeID != nil {
			line += " → " + label(people, *it.AssigneeID)
		}
	case "backlog.assigned":
		if it.AssigneeID != nil && *it.AssigneeID == e.ActorID {
			line += " to themselves"
		} else if it.AssigneeID != nil {
			line += " → " + label(people, *it.AssigneeID)
		}
	}
	return line
}

func describeParticipantJoined(e store.Event) string {
	var p store.Participant
	json.Unmarshal(e.Data, &p)
	name := p.Name
	if p.Emoji != "" {
		name = p.Emoji + " " + p.Name
	}
	line := fmt.Sprintf("👋 %s joined", name)
	if p.Sponsor != nil && p.Sponsor.DisplayName != "" {
		line += " · agent of " + p.Sponsor.DisplayName
	}
	return line
}

func describeMemberRemoved(people map[int64]store.Participant, who string, e store.Event) string {
	var m struct {
		UserID      int64  `json:"user_id"`
		DisplayName string `json:"display_name"`
	}
	json.Unmarshal(e.Data, &m)
	if actor, ok := people[e.ActorID]; ok && actor.UserID != 0 && actor.UserID == m.UserID {
		return fmt.Sprintf("👋 %s left the workspace", m.DisplayName)
	}
	if e.ActorID == 0 {
		return fmt.Sprintf("👋 %s was removed", m.DisplayName)
	}
	return fmt.Sprintf("👋 %s removed %s", who, m.DisplayName)
}
