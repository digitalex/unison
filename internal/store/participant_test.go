package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// Participants change their own name, emoji and status; what a patch
// leaves out stays as it was (#303).
func TestUpdateParticipant(t *testing.T) {
	s, now, alex, bob := setup(t)
	ctx := context.Background()
	if _, _, err := s.UpdateParticipant(ctx, alex, ParticipantPatch{Status: ptr("reviewing")}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Post(ctx, bob, NewMessage{Body: "@Alex see #1"}); err != nil {
		t.Fatal(err)
	}

	*now = now.Add(time.Minute)
	got, evs, err := s.UpdateParticipant(ctx, alex, ParticipantPatch{Name: ptr("  Lex ")})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Lex" || got.Emoji != "🧑" || got.Status != "reviewing" || got.LastSeenAt != now.UnixMilli() {
		t.Fatalf("renamed: %+v, want Lex with the emoji and status kept", got)
	}
	if len(evs) != 1 || evs[0].Type != "participant.updated" || evs[0].ActorID != alex.ID {
		t.Fatalf("events: %+v, want one participant.updated by Alex", evs)
	}
	var inEvent Participant
	if err := json.Unmarshal(evs[0].Data, &inEvent); err != nil || inEvent.Name != "Lex" || inEvent.ID != alex.ID {
		t.Fatalf("event data: %s (%v)", evs[0].Data, err)
	}

	got, _, err = s.UpdateParticipant(ctx, alex, ParticipantPatch{Emoji: ptr("🦊")})
	if err != nil || got.Name != "Lex" || got.Emoji != "🦊" || got.Status != "reviewing" {
		t.Fatalf("emoji only: %+v, %v", got, err)
	}
	got, _, err = s.UpdateParticipant(ctx, alex, ParticipantPatch{Name: ptr("Alexa"), Emoji: ptr("🐺"), Status: ptr("")})
	if err != nil || got.Name != "Alexa" || got.Emoji != "🐺" || got.Status != "" {
		t.Fatalf("all three: %+v, %v", got, err)
	}

	// The roster and a fresh sign-in see what was stored.
	list, err := s.ListParticipants(ctx, alex.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	var stored *Participant
	for i := range list {
		if list[i].ID == alex.ID {
			stored = &list[i]
		}
	}
	if stored == nil || stored.Name != "Alexa" || stored.Emoji != "🐺" || stored.Status != "" {
		t.Fatalf("stored: %+v", stored)
	}

	// Nothing to change: nothing happens.
	if got, evs, err := s.UpdateParticipant(ctx, alex, ParticipantPatch{}); err != nil || len(evs) != 0 || got.Name != "Alexa" {
		t.Fatalf("empty patch: %+v, %v, %v", got, evs, err)
	}

	// What was written to the old name stays as written.
	msgs, err := s.ListMessages(ctx, alex.WorkspaceID, 10)
	if err != nil || len(msgs) != 1 || msgs[0].Body != "@Alex see #1" {
		t.Fatalf("messages: %+v, %v", msgs, err)
	}
}

// A new name follows the rules of joining: one word, no '@', nobody
// else's.
func TestUpdateParticipantNameRules(t *testing.T) {
	s, _, alex, bob := setup(t)
	ctx := context.Background()
	for _, bad := range []string{"", "   ", "two words", "tab\tbed", "a@b", "@alex"} {
		if _, evs, err := s.UpdateParticipant(ctx, alex, ParticipantPatch{Name: ptr(bad)}); !errors.Is(err, ErrInvalid) || len(evs) != 0 {
			t.Errorf("name %q: %v, want ErrInvalid", bad, err)
		}
	}
	if _, _, err := s.UpdateParticipant(ctx, alex, ParticipantPatch{Emoji: ptr(" ")}); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty emoji: %v, want ErrInvalid", err)
	}
	// A refused patch changes nothing, the other fields included.
	if _, _, err := s.UpdateParticipant(ctx, alex, ParticipantPatch{Name: ptr("a b"), Status: ptr("busy")}); !errors.Is(err, ErrInvalid) || alex.Status != "" {
		t.Errorf("refused patch: %v, status %q", err, alex.Status)
	}

	taken := bob.Name
	for _, name := range []string{taken, flipCase(taken)} {
		_, _, err := s.UpdateParticipant(ctx, alex, ParticipantPatch{Name: ptr(name)})
		if !errors.Is(err, ErrConflict) {
			t.Errorf("taking %q: %v, want ErrConflict", name, err)
		}
	}
	if alex.Name != "Alex" {
		t.Errorf("after refusals Alex is %q", alex.Name)
	}
	// Your own name in another case is yours to take.
	if got, _, err := s.UpdateParticipant(ctx, alex, ParticipantPatch{Name: ptr("ALEX")}); err != nil || got.Name != "ALEX" {
		t.Errorf("recasing: %+v, %v", got, err)
	}
	// And a name somebody gave up is free again.
	if _, _, err := s.UpdateParticipant(ctx, bob, ParticipantPatch{Name: ptr("Robert")}); err != nil {
		t.Fatal(err)
	}
	if got, _, err := s.UpdateParticipant(ctx, alex, ParticipantPatch{Name: ptr(taken)}); err != nil || got.Name != taken {
		t.Errorf("a freed name: %+v, %v", got, err)
	}
}

// flipCase flips the case of an ASCII name's letters.
func flipCase(s string) string {
	b := []byte(s)
	for i, c := range b {
		switch {
		case c >= 'a' && c <= 'z':
			b[i] = c - 32
		case c >= 'A' && c <= 'Z':
			b[i] = c + 32
		}
	}
	return string(b)
}

// An agent says which model it runs on, at joining or later (#304).
func TestParticipantModel(t *testing.T) {
	s, _, ws, alex, _ := passSetup(t)
	ctx := context.Background()
	human, _, _, err := s.Join(ctx, ws.ID, "Human", "")
	if err != nil {
		t.Fatal(err)
	}
	pass := func() string {
		t.Helper()
		p, _, err := s.MintPass(ctx, ws.ID, alex.ID)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	quill, _, evs, err := s.JoinAgent(ctx, ws.ID, pass(), AgentJoin{Name: "Quill", Model: "  sonnet-5  "})
	if err != nil {
		t.Fatal(err)
	}
	if quill.Model != "sonnet-5" || len(evs) == 0 {
		t.Fatalf("joined: %+v", quill)
	}
	var joined Participant
	if err := json.Unmarshal(evs[0].Data, &joined); err != nil || joined.Model != "sonnet-5" {
		t.Fatalf("participant.joined carries %s (%v)", evs[0].Data, err)
	}
	stored := func(id int64) Participant {
		t.Helper()
		list, err := s.ListParticipants(ctx, ws.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range list {
			if p.ID == id {
				return p
			}
		}
		t.Fatalf("participant %d is gone", id)
		return Participant{}
	}
	if got := stored(quill.ID); got.Model != "sonnet-5" {
		t.Fatalf("stored: %+v", got)
	}
	if got := stored(human.ID); got.Model != "" {
		t.Fatalf("a participant who never said a model has %q", got.Model)
	}

	// Changing it leaves the rest alone, and tells everyone.
	got, evs, err := s.UpdateParticipant(ctx, quill, ParticipantPatch{Model: ptr("opus-5")})
	if err != nil || got.Model != "opus-5" || got.Name != "Quill" || len(evs) != 1 || evs[0].Type != "participant.updated" {
		t.Fatalf("model only: %+v, %v, %v", got, evs, err)
	}
	// A rename keeps it; "" clears it.
	if got, _, err := s.UpdateParticipant(ctx, quill, ParticipantPatch{Name: ptr("Plume")}); err != nil || got.Model != "opus-5" {
		t.Fatalf("after a rename: %+v, %v", got, err)
	}
	if got, _, err := s.UpdateParticipant(ctx, quill, ParticipantPatch{Model: ptr(" ")}); err != nil || got.Model != "" || stored(quill.ID).Model != "" {
		t.Fatalf("cleared: %+v, %v", got, err)
	}

	// One line, at most ModelMax characters (not bytes).
	longest := strings.Repeat("é", ModelMax)
	if got, _, err := s.UpdateParticipant(ctx, quill, ParticipantPatch{Model: &longest}); err != nil || got.Model != longest {
		t.Fatalf("a %d-character model: %v", ModelMax, err)
	}
	for _, bad := range []string{longest + "é", "two\nlines", "tab\tbed"} {
		if _, evs, err := s.UpdateParticipant(ctx, quill, ParticipantPatch{Model: &bad}); !errors.Is(err, ErrInvalid) || len(evs) != 0 {
			t.Errorf("model %q: %v, want ErrInvalid", bad, err)
		}
		if _, _, _, err := s.JoinAgent(ctx, ws.ID, pass(), AgentJoin{Name: "Bad", Model: bad}); !errors.Is(err, ErrInvalid) {
			t.Errorf("joining with model %q: %v, want ErrInvalid", bad, err)
		}
	}

	// Coming back under the same name keeps the model unless a new one is given.
	back, _, _, err := s.JoinAgent(ctx, ws.ID, pass(), AgentJoin{Name: "Plume"})
	if err != nil || back.ID != quill.ID || back.Model != longest {
		t.Fatalf("re-joined: %+v, %v", back, err)
	}
	back, _, _, err = s.JoinAgent(ctx, ws.ID, pass(), AgentJoin{Name: "Plume", Model: "haiku-5"})
	if err != nil || back.Model != "haiku-5" || stored(quill.ID).Model != "haiku-5" {
		t.Fatalf("re-joined with a model: %+v, %v", back, err)
	}
}
