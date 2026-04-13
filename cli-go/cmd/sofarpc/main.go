package main

import (
	"os"

	"github.com/sofarpc/cli-go/internal/cli"
)

// BuildVersion is stamped at link time via -ldflags "-X main.BuildVersion=...".
var BuildVersion = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], cli.Env{
		BuildVersion: BuildVersion,
		Stdin:        os.Stdin,
		Stdout:       os.Stdout,
		Stderr:       os.Stderr,
	}))
}
