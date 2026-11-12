package store

import (
	"context"
	"encoding/json"
	"errors"
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
