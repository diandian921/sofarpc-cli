package main

import (
	"os"

	"github.com/diandian921/sofarpc-cli/cli/internal/buildversion"
	"github.com/diandian921/sofarpc-cli/cli/internal/cli"
)

// BuildVersion is stamped at link time via -ldflags "-X main.BuildVersion=...".
var BuildVersion = "dev"

func main() {
	version := buildversion.Resolve(BuildVersion)
	os.Exit(cli.Run(os.Args[1:], cli.Env{
		BuildVersion: version,
		Stdin:        os.Stdin,
		Stdout:       os.Stdout,
		Stderr:       os.Stderr,
	}))
}
