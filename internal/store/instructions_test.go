package store

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestExtractInstructionHeadingsAndSection(t *testing.T) {
	md := strings.Join([]string{
		"# Top",
		"Intro",
		"## Claim etiquette",
		"- One item at a time",
		"```markdown",
		"## Not a heading",
		"```",
		"## Try it ##",
		"Verify before PR.",
		"### Subheading",
		"Still inside Try it.",
		"## Release",
		"Tag it.",
	}, "\n")

	got := ExtractInstructionHeadings(md)
	want := []string{"Claim etiquette", "Try it", "Release"}
	if !slices.Equal(got, want) {
		t.Fatalf("ExtractInstructionHeadings = %v, want %v", got, want)
	}

	heading, body, ok := ExtractInstructionSection(md, "try it")
	if !ok {
		t.Fatal("expected Try it section")
	}
	if heading != "Try it" {
		t.Errorf("heading = %q, want %q", heading, "Try it")
	}
	if !strings.Contains(body, "### Subheading") || strings.Contains(body, "## Release") {
		t.Errorf("unexpected section body:\n%s", body)
	}
	if _, _, ok := ExtractInstructionSection(md, "missing"); ok {
		t.Error("expected missing section to return false")
	}
}

func TestNormalizeInstructionsAndPath(t *testing.T) {
	if got, err := NormalizeInstructions("  \r\n\t "); err != nil || got != "" {
		t.Fatalf("whitespace-only: got (%q, %v), want (\"\", nil)", got, err)
	}
	if got, err := NormalizeInstructions("a\r\nb\rc"); err != nil || got != "a\nb\nc" {
		t.Fatalf("CRLF normalize: got (%q, %v)", got, err)
	}
	if _, err := NormalizeInstructions(strings.Repeat("x", MaxInstructionsBytes+1)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized instructions: got %v, want ErrInvalid", err)
	}
	if got, err := NormalizeInstructionsPath(" .unison//instructions.md "); err != nil || got != ".unison/instructions.md" {
		t.Fatalf("valid path: got (%q, %v)", got, err)
	}
	for _, bad := range []string{"/etc/passwd", "../secret.md", "a/../b", `a\b.md`, "."} {
		if _, err := NormalizeInstructionsPath(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("NormalizeInstructionsPath(%q) = %v, want ErrInvalid", bad, err)
		}
	}
}

func TestWorkspaceInstructionsLifecycle(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()

	u, err := s.LoginWithProvider(ctx, "github", Profile{Subject: "gh-1", Name: "Alex", Email: "alex@example.com", EmailVerified: true})
	if err != nil {
		t.Fatal(err)
	}
	ws, _, err := s.CreateWorkspaceWith(ctx, u.ID, NewWorkspace{
		Name:             "Unison",
		Instructions:     "## Rules\nBe nice.",
		InstructionsPath: ".unison/instructions.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ws.HasInstructions || ws.InstructionsPath != ".unison/instructions.md" || ws.InstructionsUpdatedAt == 0 {
		t.Fatalf("unexpected workspace summary: %+v", ws)
	}

	inst, err := s.GetWorkspaceInstructions(ctx, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if inst.Instructions != "## Rules\nBe nice." || !slices.Equal(inst.Headings, []string{"Rules"}) {
		t.Fatalf("GetWorkspaceInstructions = %+v", inst)
	}

	human, _, _, _, err := s.JoinAsUser(ctx, ws.ID, u.ID, "Alex", "🧑")
	if err != nil {
		t.Fatal(err)
	}
	pass, _, err := s.MintPass(ctx, ws.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	agent, _, _, err := s.JoinWithPass(ctx, ws.ID, pass, "Ada", "🦊")
	if err != nil {
		t.Fatal(err)
	}

	// Agents cannot edit workspace instructions.
	body := "## Hacked\nNope"
	if _, _, err := s.SetWorkspaceInstructions(ctx, ws.ID, agent, InstructionsInput{Instructions: &body}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("agent SetWorkspaceInstructions = %v, want ErrForbidden", err)
	}

	// No-op update emits no event and does not bump UpdatedAt.
	*now = now.Add(time.Minute)
	same := "## Rules\nBe nice."
	got, evs, err := s.SetWorkspaceInstructions(ctx, ws.ID, human, InstructionsInput{Instructions: &same})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 0 {
		t.Fatalf("no-op update emitted %d events, want 0", len(evs))
	}
	if got.UpdatedAt != inst.UpdatedAt {
		t.Fatalf("no-op update bumped UpdatedAt from %d to %d", inst.UpdatedAt, got.UpdatedAt)
	}

	// Real update emits workspace.instructions_updated and bumps UpdatedAt.
	updated := "## Rules\nBe kind.\n## Testing\nRun go test."
	got, evs, err = s.SetWorkspaceInstructions(ctx, ws.ID, human, InstructionsInput{Instructions: &updated})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Type != "workspace.instructions_updated" {
		t.Fatalf("expected 1 workspace.instructions_updated event, got %+v", evs)
	}
	if !slices.Equal(got.Headings, []string{"Rules", "Testing"}) || got.UpdatedAt <= inst.UpdatedAt {
		t.Fatalf("unexpected updated instructions: %+v", got)
	}
}
