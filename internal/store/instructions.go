package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"
)

const (
	// MaxInstructionsBytes is the largest workspace instructions body accepted (32 KiB).
	MaxInstructionsBytes = 32 * 1024
	// MaxInstructionsPathLen is the longest repo-relative instructions file path.
	MaxInstructionsPathLen = 256
	// InstructionsInlineLimit is the maximum rune count of workspace instructions
	// inlined directly into the agent doorway; longer instructions are indexed by
	// their ## headings so agents load sections on demand.
	InstructionsInlineLimit = 1200
)

// WorkspaceInstructions is a workspace's opt-in agent instructions,
// returned by GET /api/workspaces/{ws}/instructions and updated by PUT.
type WorkspaceInstructions struct {
	Instructions string   `json:"instructions"`
	Path         string   `json:"instructions_path,omitempty"`
	UpdatedAt    int64    `json:"instructions_updated_at,omitempty"`
	Headings     []string `json:"headings"`
	Section      string   `json:"section,omitempty"`
}

// InstructionsInput is the payload for PUT /api/workspaces/{ws}/instructions.
// Omitted (nil) fields are left unchanged; empty strings clear the field.
type InstructionsInput struct {
	Instructions *string `json:"instructions,omitempty"`
	Path         *string `json:"instructions_path,omitempty"`
}

// InstructionsUpdatedEvent is the compact payload of the
// workspace.instructions_updated event. It intentionally omits the full
// instructions body so event streams and `unison wait` stay lightweight.
type InstructionsUpdatedEvent struct {
	HasInstructions       bool     `json:"has_instructions"`
	InstructionsPath      string   `json:"instructions_path,omitempty"`
	InstructionsUpdatedAt int64    `json:"instructions_updated_at,omitempty"`
	CharCount             int      `json:"char_count"`
	Headings              []string `json:"headings,omitempty"`
}

// NormalizeInstructions validates and normalizes a workspace instructions body.
// Line endings are normalized to LF; whitespace-only input becomes "".
func NormalizeInstructions(raw string) (string, error) {
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	if strings.TrimSpace(s) == "" {
		return "", nil
	}
	if len(s) > MaxInstructionsBytes {
		return "", fmt.Errorf("%w: instructions exceed %d bytes", ErrInvalid, MaxInstructionsBytes)
	}
	return s, nil
}

// NormalizeInstructionsPath validates a repo-relative instructions file path:
// empty, or a clean relative POSIX path without leading "/", "..", or "\".
func NormalizeInstructionsPath(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", nil
	}
	if len(p) > MaxInstructionsPathLen {
		return "", fmt.Errorf("%w: instructions_path exceeds %d characters", ErrInvalid, MaxInstructionsPathLen)
	}
	if strings.ContainsAny(p, "\x00\\") || strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("%w: instructions_path must be a relative path without leading '/' or '\\', got %q", ErrInvalid, raw)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", fmt.Errorf("%w: instructions_path must not contain '..', got %q", ErrInvalid, raw)
		}
	}
	cleaned := path.Clean(p)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "/") || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%w: invalid instructions_path %q", ErrInvalid, raw)
	}
	return cleaned, nil
}

func isFenceLine(trimmed string) (byte, int) {
	if len(trimmed) < 3 {
		return 0, 0
	}
	ch := trimmed[0]
	if ch != '`' && ch != '~' {
		return 0, 0
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0
	}
	return ch, n
}

func parseH2(line string) (string, bool) {
	if !strings.HasPrefix(line, "## ") && !strings.HasPrefix(line, "##\t") {
		return "", false
	}
	text := strings.TrimSpace(line[3:])
	text = strings.TrimRight(text, "#")
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	return text, true
}

func isTopHeading(line string) bool {
	return strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "#\t") ||
		strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "##\t")
}

// ExtractInstructionHeadings returns the level-2 ("## ") Markdown headings in
// md in order, ignoring headings inside fenced code blocks. Always returns a
// non-nil slice.
func ExtractInstructionHeadings(md string) []string {
	headings := []string{}
	var fenceChar byte
	var fenceLen int
	for _, line := range strings.Split(md, "\n") {
		trimmed := strings.TrimSpace(line)
		if ch, n := isFenceLine(trimmed); ch != 0 {
			if fenceChar == 0 {
				fenceChar, fenceLen = ch, n
			} else if ch == fenceChar && n >= fenceLen && strings.TrimSpace(trimmed[n:]) == "" {
				fenceChar, fenceLen = 0, 0
			}
			continue
		}
		if fenceChar != 0 {
			continue
		}
		if h, ok := parseH2(line); ok {
			headings = append(headings, h)
		}
	}
	return headings
}

// ExtractInstructionSection extracts the level-2 ("## ") Markdown section
// whose heading matches name (case-insensitively, trimmed). It returns the
// matched heading text, the section body (including the "## " line up to the
// next "#" or "##" heading), and true; or ("", "", false) if not found.
func ExtractInstructionSection(md, name string) (string, string, bool) {
	want := strings.TrimSpace(name)
	if want == "" {
		return "", "", false
	}
	lines := strings.Split(md, "\n")
	var fenceChar byte
	var fenceLen int
	start := -1
	matched := ""
	end := len(lines)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if ch, n := isFenceLine(trimmed); ch != 0 {
			if fenceChar == 0 {
				fenceChar, fenceLen = ch, n
			} else if ch == fenceChar && n >= fenceLen && strings.TrimSpace(trimmed[n:]) == "" {
				fenceChar, fenceLen = 0, 0
			}
			continue
		}
		if fenceChar != 0 {
			continue
		}
		if start < 0 {
			if h, ok := parseH2(line); ok && strings.EqualFold(h, want) {
				start = i
				matched = h
			}
			continue
		}
		if isTopHeading(line) {
			end = i
			break
		}
	}
	if start < 0 {
		return "", "", false
	}
	section := strings.TrimRight(strings.Join(lines[start:end], "\n"), "\n \t")
	return matched, section, true
}

// isPerson reports whether p is a person rather than a pass-joined agent.
func isPerson(p *Participant) bool {
	return p != nil && p.Sponsor == nil
}

// GetWorkspaceInstructions returns the workspace's opt-in agent instructions,
// optional repo file path, update timestamp, and extracted ## headings.
func (s *Store) GetWorkspaceInstructions(ctx context.Context, wsID string) (*WorkspaceInstructions, error) {
	var body, repoPath string
	var updatedAt int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(instructions, ''), COALESCE(instructions_path, ''), COALESCE(instructions_updated_at, 0) FROM workspaces WHERE id = ?`, wsID).
		Scan(&body, &repoPath, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &WorkspaceInstructions{
		Instructions: body,
		Path:         repoPath,
		UpdatedAt:    updatedAt,
		Headings:     ExtractInstructionHeadings(body),
	}, nil
}

// SetWorkspaceInstructions updates a workspace's instructions and/or repo file
// path, bumps instructions_updated_at, and records a compact
// workspace.instructions_updated event (without the full body). Only a person
// may edit workspace instructions; agents get ErrForbidden. When neither
// instructions nor instructions_path changes, it returns the current state
// without writing or emitting an event.
func (s *Store) SetWorkspaceInstructions(ctx context.Context, wsID string, actor *Participant, in InstructionsInput) (*WorkspaceInstructions, []Event, error) {
	if !isPerson(actor) {
		return nil, nil, fmt.Errorf("%w: only a person can edit workspace instructions", ErrForbidden)
	}
	var nextBody *string
	if in.Instructions != nil {
		v, err := NormalizeInstructions(*in.Instructions)
		if err != nil {
			return nil, nil, err
		}
		nextBody = &v
	}
	var nextPath *string
	if in.Path != nil {
		v, err := NormalizeInstructionsPath(*in.Path)
		if err != nil {
			return nil, nil, err
		}
		nextPath = &v
	}

	var out *WorkspaceInstructions
	evs, err := s.tx(ctx, func(t *tx, log *eventLog) error {
		var curBody, curPath string
		var curUpdated int64
		if err := t.QueryRow(`SELECT COALESCE(instructions, ''), COALESCE(instructions_path, ''), COALESCE(instructions_updated_at, 0) FROM workspaces WHERE id = ?`, wsID).
			Scan(&curBody, &curPath, &curUpdated); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		body := curBody
		if nextBody != nil {
			body = *nextBody
		}
		repoPath := curPath
		if nextPath != nil {
			repoPath = *nextPath
		}
		headings := ExtractInstructionHeadings(body)
		if body == curBody && repoPath == curPath {
			out = &WorkspaceInstructions{
				Instructions: body,
				Path:         repoPath,
				UpdatedAt:    curUpdated,
				Headings:     headings,
			}
			return nil
		}
		updatedAt := log.now
		if _, err := t.Exec(`UPDATE workspaces SET instructions = ?, instructions_path = ?, instructions_updated_at = ? WHERE id = ?`,
			body, repoPath, updatedAt, wsID); err != nil {
			return err
		}
		out = &WorkspaceInstructions{
			Instructions: body,
			Path:         repoPath,
			UpdatedAt:    updatedAt,
			Headings:     headings,
		}
		return log.add(wsID, "workspace.instructions_updated", actor.ID, InstructionsUpdatedEvent{
			HasInstructions:       body != "",
			InstructionsPath:      repoPath,
			InstructionsUpdatedAt: updatedAt,
			CharCount:             utf8.RuneCountInString(body),
			Headings:              headings,
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return out, evs, nil
}
