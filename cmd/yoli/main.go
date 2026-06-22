// Command yoli is the yoli coding-agent CLI. It dispatches to the
// version, chat, run, skills, and config subcommands implemented in
// the internal/cli package.
package main

import (
	"os"

	"yoli/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
