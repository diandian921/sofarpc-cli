package cli

import (
	"context"
	"flag"
	"fmt"

	"github.com/diandian921/sofarpc-cli/internal/app"
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
	addr, err := resolveAddress(rest[0])
	if err != nil {
		fmt.Fprintln(env.Stderr, "ping:", err)
		return 2
	}

	probe := app.New(nil).ProbeEndpoint(context.Background(), app.ProbeInput{
		Address:   addr,
		Service:   *service,
		TimeoutMS: *timeoutMS,
	})
	result := app.RenderProbe(probe)
	result.RequestID = app.NewRequestID("ping")
	if err := writeResult(env.Stdout, result); err != nil {
		fmt.Fprintln(env.Stderr, "ping: write result:", err)
		return 1
	}
	if !result.OK {
		return 1
	}
	return 0
}
