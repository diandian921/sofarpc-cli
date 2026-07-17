package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/mcp"
)

// runMCP is the stdio MCP server entry point, formerly the standalone
// sofarpc-mcp binary. It is registered as a subcommand so the project ships
// a single user-facing binary (sofarpc) with mcp as a verb that hosts spawn.
func runMCP(args []string, env Env) int {
	// Default the streams first: they are used below (flag output, --version)
	// before the server is built, so a caller passing a partial Env must not
	// hit a nil writer.
	stdin := env.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	stdout := env.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := env.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	disableConfigWrite := fs.Bool("disable-config-write", false, "reject MCP config actions that modify config.json")
	selfTest := fs.Bool("selftest", false, "initialize server machinery and exit 0 without serving stdio")
	showVersion := fs.Bool("version", false, "print build version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *showVersion {
		fmt.Fprintln(stdout, env.BuildVersion)
		return 0
	}

	server := &mcp.Server{
		BuildVersion:       env.BuildVersion,
		Stdin:              stdin,
		Stdout:             stdout,
		Stderr:             stderr,
		DisableConfigWrite: *disableConfigWrite,
		App:                app.New(nil),
	}

	if *selfTest {
		if err := server.SelfTest(); err != nil {
			fmt.Fprintf(stderr, "selftest failed: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "ok")
		return 0
	}

	return server.Run()
}
