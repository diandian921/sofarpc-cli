package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/diandian921/sofarpc-cli/internal/app"
	"github.com/diandian921/sofarpc-cli/internal/buildversion"
	"github.com/diandian921/sofarpc-cli/internal/mcp"
)

// BuildVersion is stamped at link time via -ldflags "-X main.BuildVersion=...".
var BuildVersion = "dev"

func main() {
	version := buildversion.Resolve(BuildVersion)
	fs := flag.NewFlagSet("sofarpc-mcp", flag.ExitOnError)
	disableConfigWrite := fs.Bool("disable-config-write", false, "reject MCP config actions that modify config.json")
	selfTest := fs.Bool("selftest", false, "initialize the server machinery, exit 0 on success, without serving stdio")
	showVersion := fs.Bool("version", false, "print build version and exit")
	_ = fs.Parse(os.Args[1:])

	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	server := &mcp.Server{
		BuildVersion:       version,
		Stdin:              os.Stdin,
		Stdout:             os.Stdout,
		Stderr:             os.Stderr,
		DisableConfigWrite: *disableConfigWrite,
		App:                app.New(nil),
	}

	if *selfTest {
		if err := server.SelfTest(); err != nil {
			fmt.Fprintf(os.Stderr, "selftest failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("ok")
		os.Exit(0)
	}

	os.Exit(server.Run())
}
