package main

import (
	"flag"
	"os"

	"github.com/sofarpc/cli/internal/mcp"
)

// BuildVersion is stamped at link time via -ldflags "-X main.BuildVersion=...".
var BuildVersion = "dev"

func main() {
	fs := flag.NewFlagSet("sofarpc-mcp", flag.ExitOnError)
	disableConfigWrite := fs.Bool("disable-config-write", false, "reject MCP config actions that modify ~/.sofarpc/config.json")
	_ = fs.Parse(os.Args[1:])

	server := &mcp.Server{
		BuildVersion:       BuildVersion,
		Stdin:              os.Stdin,
		Stdout:             os.Stdout,
		Stderr:             os.Stderr,
		DisableConfigWrite: *disableConfigWrite,
	}
	os.Exit(server.Run())
}
