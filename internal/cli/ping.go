package cli

import (
	"context"
	"flag"
	"fmt"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

func runPing(args []string, env Env) int {
	fs := flag.NewFlagSet("ping", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	service := fs.String("service", "", "optional service hint for richer errors")
	timeoutMS := fs.Int("timeout-ms", 0, "dial timeout (ms); 0 uses default")

	rest, err := parseMixed(fs, args)
	if err != nil {
		return 2
	}
	if len(rest) != 1 {
		fmt.Fprintln(env.Stderr, "usage: sofarpc ping <host:port|server> [--service <name>] [--timeout-ms <ms>]")
		return 2
	}
	// A raw host:port dials directly; anything else is a configured server name,
	// resolved by app.ProbeEndpoint (the same path the MCP probe tool uses).
	input := app.ProbeInput{Service: *service, TimeoutMS: *timeoutMS}
	if appconfig.IsHostPort(rest[0]) {
		input.Address = rest[0]
	} else {
		input.Server = rest[0]
	}
	probe := app.New(nil).ProbeEndpoint(context.Background(), input)
	return emitResult(env, "ping", app.RenderProbe(probe))
}
