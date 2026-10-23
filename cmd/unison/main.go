// Command unison is the Unison server (`unison serve`) and its 1:1 CLI.
package main

import (
	"os"

	"github.com/digitalex/unison/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
