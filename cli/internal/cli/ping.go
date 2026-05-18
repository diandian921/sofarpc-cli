package cli

import (
	"context"
	"flag"
	"fmt"

	"github.com/sofarpc/cli/internal/app"
	"github.com/sofarpc/cli/internal/protocol"
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
		fmt.Fprintln(env.Stderr, "usage: sofarpc-cli ping <host:port|server> [--service <name>] [--timeout-ms <ms>]")
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
	resp := protocolResponseFromProbe(probe)
	resp.RequestID = protocol.NewRequestID(protocol.OpPing)
	if err := writeResponse(env.Stdout, &resp); err != nil {
		fmt.Fprintln(env.Stderr, "ping: write response:", err)
		return 1
	}
	if !resp.OK {
		return 1
	}
	return 0
}
