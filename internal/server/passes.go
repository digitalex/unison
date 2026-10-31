package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/digitalex/unison/internal/store"
)

// mintPass creates a single-use agent pass for the signed-in member and
// returns it with a ready-to-paste invite prompt.
func (s *Server) mintPass(w http.ResponseWriter, r *http.Request) {
	ws, err := s.store.GetWorkspace(r.Context(), r.PathValue("ws"))
	if err != nil {
		storeError(w, err)
		return
	}
	u := signedIn(r).user
	pass, expires, err := s.store.MintPass(r.Context(), ws.ID, u.ID)
	if err != nil {
		storeError(w, err)
		return
	}
	s.track(eventPassGenerated, userActor(u.ID))
	writeJSON(w, http.StatusCreated, map[string]any{
		"pass":          pass,
		"expires_at":    expires,
		"invite_prompt": invitePrompt(requestOrigin(r), ws, firstNonEmpty(u.DisplayName, "a member"), pass),
	})
}

// invitePrompt is what a member pastes into a coding agent. Agents can't
// join without a pass, so there is no passless prompt.
func invitePrompt(origin string, ws *store.Workspace, sponsor, pass string) string {
	url := origin + "/w/" + ws.ID
	// The doorway is private; the pass opens it (reading doesn't use it up).
	guide := url + "/agent.md?pass=" + pass
	return fmt.Sprintf("You're invited to collaborate in the Unison workspace %q as an agent of %s. "+
		"Fetch %s (e.g. curl -s '%s') and follow it: install the unison CLI, then join with this pass "+
		"(it works once and expires in %d minutes):\n\n"+
		"unison join %s --pass %s --name <YourName> --emoji <emoji>\n\n"+
		"Pick a name and emoji of your own and say hello in chat. Then pick the next backlog item you're able to work on and start on it, "+
		"posting important updates and follow-up questions in chat. Open a PR with auto-merge on, unless it's risky "+
		"(it changes the production database, affects auth, etc.); close the item once its PR is merged. "+
		"Park on `unison wait` between laps, and whenever there are no more backlog items you can work on: "+
		"it wakes you for mentions and for new items.",
		ws.Name, sponsor, guide, guide, int(store.AgentPassTTL/time.Minute), url, pass)
}
