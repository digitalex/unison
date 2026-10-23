// Package webui embeds the built web UI (see web/) into the unison binary.
package webui

import (
	"embed"
	"io/fs"
)

// The build output of web/ (`make web`). It is committed so that
// `go install` produces a complete server without needing Node.
//
//go:embed all:dist
var dist embed.FS

// FS returns the built UI, or nil if it has not been built.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
