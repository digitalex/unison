package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestArtifactsLifecycle(t *testing.T) {
	s, now, ada, bob := setup(t)
	wsID := ada.WorkspaceID
	ctx := context.Background()

	item, _, err := s.AddItem(ctx, ada, "Implement artifacts", "", 0)
	if err != nil {
		t.Fatal(err)
	}

	mdV1 := []byte("# Design v1\n\nInitial proposal.")
	sumV1 := sha256.Sum256(mdV1)
	hexV1 := hex.EncodeToString(sumV1[:])

	art, evs, err := s.AddArtifact(ctx, ada, ArtifactInput{
		Name:   "design-doc.md",
		Title:  "Auth Design",
		Data:   mdV1,
		ItemID: &item.ID,
	})
	if err != nil {
		t.Fatalf("AddArtifact: %v", err)
	}
	if len(evs) != 1 || evs[0].Type != "artifact.created" {
		t.Fatalf("events = %+v, want [artifact.created]", evs)
	}
	if art.Name != "design-doc.md" || art.Title != "Auth Design" || art.MimeType != "text/markdown; charset=utf-8" ||
		art.Size != int64(len(mdV1)) || art.SHA256 != hexV1 || art.Version != 1 ||
		art.ItemID == nil || *art.ItemID != item.ID || art.UploadedBy == nil || *art.UploadedBy != ada.ID ||
		art.URL != "/w/"+wsID+"/artifacts/design-doc.md" || art.RawURL != "/api/workspaces/"+wsID+"/artifacts/design-doc.md" {
		t.Fatalf("unexpected artifact: %+v", art)
	}
	// Ensure raw data bytes are not leaked into event JSON or metadata JSON.
	if strings.Contains(string(evs[0].Data), "Initial proposal") {
		t.Errorf("event JSON contains raw bytes: %s", evs[0].Data)
	}
	rawJSON, _ := json.Marshal(art)
	if strings.Contains(string(rawJSON), "Initial proposal") {
		t.Errorf("metadata JSON contains raw bytes: %s", rawJSON)
	}

	// Duplicate name (including case-insensitive) conflicts with 409-style ErrConflict suggesting update.
	if _, _, err := s.AddArtifact(ctx, bob, ArtifactInput{Name: "Design-Doc.MD", Data: []byte("other")}); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "update") {
		t.Errorf("duplicate name err = %v, want ErrConflict mentioning update", err)
	}

	// Default title falls back to name; HTML mime type detected.
	*now = now.Add(time.Minute)
	proto, _, err := s.AddArtifact(ctx, bob, ArtifactInput{
		Name: "prototype.html",
		Data: []byte("<!doctype html><html><body><h1>Hi</h1></body></html>"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if proto.Title != "prototype.html" || proto.MimeType != "text/html; charset=utf-8" {
		t.Errorf("proto = %+v", proto)
	}

	// ListArtifacts orders newest updated_at first, and supports item_id filter.
	all, err := s.ListArtifacts(ctx, wsID, 0)
	if err != nil || len(all) != 2 || all[0].Name != "prototype.html" || all[1].Name != "design-doc.md" {
		t.Fatalf("ListArtifacts(all) = %+v, %v", all, err)
	}
	byItem, err := s.ListArtifacts(ctx, wsID, item.ID)
	if err != nil || len(byItem) != 1 || byItem[0].Name != "design-doc.md" {
		t.Fatalf("ListArtifacts(item) = %+v, %v", byItem, err)
	}

	// GetArtifact & GetArtifactMeta work case-insensitively and preserve exact name.
	meta, err := s.GetArtifactMeta(ctx, wsID, "DESIGN-DOC.MD")
	if err != nil || meta.Name != "design-doc.md" || meta.SHA256 != hexV1 {
		t.Fatalf("GetArtifactMeta = %+v, %v", meta, err)
	}
	gotArt, gotBytes, err := s.GetArtifact(ctx, wsID, "design-doc.md")
	if err != nil || !bytes.Equal(gotBytes, mdV1) || gotArt.Version != 1 {
		t.Fatalf("GetArtifact = %+v, %q, %v", gotArt, gotBytes, err)
	}

	// In-place content replacement keeps exact same name & permalink, increments version, updates sha256, records artifact.updated.
	*now = now.Add(time.Minute)
	mdV2 := []byte("# Design v2\n\nUpdated architecture with ETag caching.")
	sumV2 := sha256.Sum256(mdV2)
	hexV2 := hex.EncodeToString(sumV2[:])
	updated, evs, err := s.UpdateArtifact(ctx, bob, "DESIGN-DOC.md", ArtifactPatch{Data: &mdV2})
	if err != nil {
		t.Fatalf("UpdateArtifact(data): %v", err)
	}
	if len(evs) != 1 || evs[0].Type != "artifact.updated" {
		t.Fatalf("UpdateArtifact events = %+v, want [artifact.updated]", evs)
	}
	if updated.Name != "design-doc.md" || updated.URL != art.URL || updated.Version != 2 ||
		updated.SHA256 != hexV2 || updated.Size != int64(len(mdV2)) ||
		updated.Title != "Auth Design" || updated.UpdatedBy == nil || *updated.UpdatedBy != bob.ID ||
		updated.UploadedBy == nil || *updated.UploadedBy != ada.ID {
		t.Fatalf("updated artifact = %+v", updated)
	}
	_, gotV2, err := s.GetArtifact(ctx, wsID, "design-doc.md")
	if err != nil || !bytes.Equal(gotV2, mdV2) {
		t.Fatalf("GetArtifact after replace = %q, %v", gotV2, err)
	}

	// Metadata-only update preserves bytes & sha256, increments version, emits artifact.updated.
	*now = now.Add(time.Minute)
	newTitle := "Final Auth Design"
	metaUpdated, evs, err := s.UpdateArtifact(ctx, ada, "design-doc.md", ArtifactPatch{Title: &newTitle})
	if err != nil || len(evs) != 1 || evs[0].Type != "artifact.updated" {
		t.Fatalf("UpdateArtifact(meta) = %+v, %+v, %v", metaUpdated, evs, err)
	}
	if metaUpdated.Title != "Final Auth Design" || metaUpdated.Version != 3 || metaUpdated.SHA256 != hexV2 {
		t.Fatalf("metaUpdated = %+v", metaUpdated)
	}

	// Deleting the attached backlog item unlinks item_id without deleting the artifact.
	if _, err := s.DeleteItem(ctx, ada, item.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}
	afterItemDel, err := s.GetArtifactMeta(ctx, wsID, "design-doc.md")
	if err != nil || afterItemDel.ItemID != nil {
		t.Fatalf("after DeleteItem: %+v, %v (want ItemID nil)", afterItemDel, err)
	}

	// DeleteArtifact removes it and emits artifact.deleted.
	delEvs, err := s.DeleteArtifact(ctx, wsID, ada.ID, "design-doc.md")
	if err != nil || len(delEvs) != 1 || delEvs[0].Type != "artifact.deleted" {
		t.Fatalf("DeleteArtifact = %+v, %v", delEvs, err)
	}
	if _, _, err := s.GetArtifact(ctx, wsID, "design-doc.md"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetArtifact after delete = %v, want ErrNotFound", err)
	}

	// DeleteWorkspace cleans up remaining artifacts.
	if err := s.DeleteWorkspace(ctx, wsID, 0); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}
}

func TestArtifactValidationAndMimeTypes(t *testing.T) {
	s, _, ada, _ := setup(t)
	ctx := context.Background()

	badNames := []string{
		"",
		"   ",
		".",
		"..",
		"../secret.md",
		"sub/doc.md",
		`sub\doc.md`,
		"foo..bar.md",
		"bad\nname.md",
		"bad\x00name.md",
		strings.Repeat("a", MaxArtifactNameLen+1) + ".md",
	}
	for _, bad := range badNames {
		if _, _, err := s.AddArtifact(ctx, ada, ArtifactInput{Name: bad, Data: []byte("ok")}); !errors.Is(err, ErrInvalid) {
			t.Errorf("AddArtifact(%q) = %v, want ErrInvalid", bad, err)
		}
	}

	if _, _, err := s.AddArtifact(ctx, ada, ArtifactInput{Name: "empty.md", Data: nil}); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty data err = %v, want ErrInvalid", err)
	}
	tooBig := make([]byte, MaxArtifactBytes+1)
	if _, _, err := s.AddArtifact(ctx, ada, ArtifactInput{Name: "huge.bin", Data: tooBig}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("oversized data err = %v, want ErrTooLarge", err)
	}
	badItem := int64(99999)
	if _, _, err := s.AddArtifact(ctx, ada, ArtifactInput{Name: "doc.md", Data: []byte("hi"), ItemID: &badItem}); !errors.Is(err, ErrInvalid) {
		t.Errorf("non-existent item err = %v, want ErrInvalid", err)
	}

	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"readme.md", []byte("# hi"), "text/markdown; charset=utf-8"},
		{"notes.markdown", []byte("hi"), "text/markdown; charset=utf-8"},
		{"app.html", []byte("<div>hi</div>"), "text/html; charset=utf-8"},
		{"diagram.svg", []byte("<svg></svg>"), "image/svg+xml"},
		{"report.json", []byte(`{"ok":true}`), "application/json; charset=utf-8"},
		{"data.csv", []byte("a,b\n1,2"), "text/plain; charset=utf-8"},
		{"main.go", []byte("package main"), "text/plain; charset=utf-8"},
		{"shot.png", pngBytes, "image/png"},
		{"spec.pdf", []byte("%PDF-1.7"), "application/pdf"},
	}
	for _, tc := range cases {
		art, _, err := s.AddArtifact(ctx, ada, ArtifactInput{Name: tc.name, Data: tc.data})
		if err != nil {
			t.Fatalf("AddArtifact(%s): %v", tc.name, err)
		}
		if art.MimeType != tc.want {
			t.Errorf("%s mime_type = %q, want %q", tc.name, art.MimeType, tc.want)
		}
	}
}
