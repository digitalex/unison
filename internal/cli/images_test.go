package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

func TestImageCLI(t *testing.T) {
	e := newTestEnv(t)
	alexDir, agentDir := t.TempDir(), t.TempDir()
	e.login("Alex", "")
	var ws store.Workspace
	if err := json.Unmarshal([]byte(e.ok(alexDir, "workspace", "create", "Shots", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	invite := e.url + "/w/" + ws.ID
	e.ok(alexDir, "join", invite, "--name", "Alex", "--account")
	e.ok(agentDir, "join", invite, "--pass", e.passFor(alexDir), "--name", "Ada")

	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")
	shot := filepath.Join(agentDir, "login.png")
	os.WriteFile(shot, png, 0o644)
	out := strings.TrimSpace(e.ok(agentDir, "image", "upload", shot))
	m := regexp.MustCompile(`^!\[login\.png\]\((/api/workspaces/` + ws.ID + `/images/(\d+))\)$`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("upload printed %q, want ![login.png](/api/workspaces/…/images/N)", out)
	}
	var im store.Image
	if err := json.Unmarshal([]byte(e.ok(agentDir, "image", "upload", shot, "--name", "Before fix", "--json")), &im); err != nil || im.Name != "Before fix" {
		t.Fatalf("upload --json = %+v, %v", im, err)
	}

	for _, ref := range []string{m[2], "#" + m[2], m[1], e.url + m[1], out} {
		dest := filepath.Join(alexDir, "got.png")
		os.Remove(dest)
		e.ok(alexDir, "image", "save", ref, dest)
		if got, _ := os.ReadFile(dest); !bytes.Equal(got, png) {
			t.Errorf("image save %s: got %d bytes", ref, len(got))
		}
	}
	if got := e.ok(alexDir, "image", "save", m[2], "-"); got != string(png) {
		t.Errorf("image save - wrote %d bytes, want the image", len(got))
	}

	notImage := filepath.Join(agentDir, "notes.txt")
	os.WriteFile(notImage, []byte("hello"), 0o644)
	if _, errOut, code := e.run(agentDir, "image", "upload", notImage); code != 1 || !strings.Contains(errOut, "PNG, JPEG, GIF and WebP") {
		t.Errorf("uploading text = %d %q", code, errOut)
	}
	if _, errOut, code := e.run(alexDir, "image", "save", "logo", "x.png"); code != 1 || !strings.Contains(errOut, "not an image id") {
		t.Errorf("save bad ref = %d %q", code, errOut)
	}
}
