package store

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// pngBytes is a 1×1 PNG.
var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")

func TestImages(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()

	im, evs, err := s.AddImage(ctx, a, " shots/Login screen [v2].png ", pngBytes)
	if err != nil || len(evs) != 1 || evs[0].Type != "image.uploaded" {
		t.Fatalf("AddImage = %+v, %+v, %v", im, evs, err)
	}
	if im.Name != "Login screen v2.png" || im.ContentType != "image/png" || im.Size != int64(len(pngBytes)) || im.UploadedBy != a.ID {
		t.Errorf("image = %+v", im)
	}
	wantURL := ImageURL(a.WorkspaceID, im.ID)
	if im.URL != wantURL || im.Markdown != "![Login screen v2.png]("+wantURL+")" {
		t.Errorf("url %q, markdown %q", im.URL, im.Markdown)
	}

	got, data, err := s.GetImage(ctx, a.WorkspaceID, im.ID)
	if err != nil || !bytes.Equal(data, pngBytes) || got.Name != im.Name || got.URL != wantURL {
		t.Fatalf("GetImage = %+v, %d bytes, %v", got, len(data), err)
	}
	if _, _, err := s.GetImage(ctx, "ws_other", im.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("image from another workspace err = %v, want ErrNotFound", err)
	}

	for name, data := range map[string][]byte{
		"empty":    nil,
		"html":     []byte("<html><script>alert(1)</script>"),
		"svg":      []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		"too big":  append(append([]byte{}, pngBytes...), make([]byte, MaxImageBytes)...),
		"riff wav": []byte("RIFF\x00\x00\x00\x00WAVEfmt "),
	} {
		if _, _, err := s.AddImage(ctx, a, "x", data); !errors.Is(err, ErrInvalid) {
			t.Errorf("AddImage(%s) err = %v, want ErrInvalid", name, err)
		}
	}
	for _, c := range []struct {
		data []byte
		want string
	}{
		{[]byte("\xff\xd8\xff\xe0rest"), "image/jpeg"},
		{[]byte("GIF89a....."), "image/gif"},
		{[]byte("RIFF\x10\x00\x00\x00WEBPVP8 "), "image/webp"},
	} {
		im, _, err := s.AddImage(ctx, a, "", c.data)
		if err != nil || im.ContentType != c.want || im.Name != "image" {
			t.Errorf("AddImage(%q) = %+v, %v; want %s named image", c.data[:4], im, err, c.want)
		}
	}

	if err := s.DeleteWorkspace(ctx, a.WorkspaceID); err != nil {
		t.Fatalf("DeleteWorkspace with images: %v", err)
	}
	if _, _, err := s.GetImage(ctx, a.WorkspaceID, im.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("image after workspace delete err = %v", err)
	}
}
