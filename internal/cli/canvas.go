package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// isocanCanvasURL is the address of a canvas on isocan.io.
var isocanCanvasURL = regexp.MustCompile(`^https://isocan\.io/p/[A-Za-z0-9_-]+$`)

// newCanvas makes an isocan canvas named name with the isocan CLI
// (`isocan --json canvas new <name>`) and returns its address. The CLI
// prints the new canvas's id ({"canvasId": "prj_…"}); a "url" is taken as
// is, should it print one.
func newCanvas(name string) (string, error) {
	bin, err := exec.LookPath("isocan")
	if err != nil {
		return "", errors.New("--create-canvas needs the isocan CLI on your PATH: install it with `npx github:dglazkov/isocan#release setup`, or pass --canvas <url>")
	}
	cmd := exec.Command(bin, "--json", "canvas", "new", name)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("`isocan canvas new` failed (%v): %s", err, strings.TrimSpace(stderr.String()))
	}
	var created struct {
		URL      string `json:"url"`
		CanvasID string `json:"canvasId"`
		ID       string `json:"id"`
	}
	json.Unmarshal(out, &created)
	u := created.URL
	if id := firstNonEmpty(created.CanvasID, created.ID); u == "" && id != "" {
		u = "https://isocan.io/p/" + id
	}
	if !isocanCanvasURL.MatchString(u) {
		return "", fmt.Errorf("`isocan --json canvas new` didn't print a canvas id or an https://isocan.io/p/<id> URL: %s", strings.TrimSpace(string(out)))
	}
	return u, nil
}
