package cli

import (
	"flag"
	"fmt"
)

// runInstall is the top-level user verb: install the binary, then optionally
// register it with one or more agent hosts. It is a thin composer over
// self-install and setup; the underlying logic stays in those handlers.
//
//	sofarpc install              # self-install only
//	sofarpc install codex        # self-install + setup codex
//	sofarpc install claude       # self-install + setup claude
//	sofarpc install all          # self-install + setup claude and codex
func runInstall(args []string, env Env) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()

	var host string
	switch len(rest) {
	case 0:
	case 1:
		host = rest[0]
		switch host {
		case "claude", "codex", "all":
		default:
			fmt.Fprintf(env.Stderr, "install: unknown host %q (want claude|codex|all)\n", host)
			return 2
		}
	default:
		fmt.Fprintln(env.Stderr, "usage: sofarpc install [claude|codex|all]")
		return 2
	}

	if code := runSelfInstall(nil, env); code != 0 {
		return code
	}
	if host == "" {
		fmt.Fprintln(env.Stdout, "\nNext: register the MCP server with your agent host:")
		fmt.Fprintln(env.Stdout, "  sofarpc install claude    # or codex, or all")
		return 0
	}
	return runSetup([]string{host}, env)
}
