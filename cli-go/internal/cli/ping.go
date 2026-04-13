package cli

import (
	"flag"
	"fmt"

	"github.com/sofarpc/cli-go/internal/protocol"
)

func runPing(args []string, env Env) int {
	fs := flag.NewFlagSet("ping", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	addr := fs.String("address", "", "target bolt address, e.g. 127.0.0.1:12200")
	service := fs.String("service", "", "optional service hint for richer errors")
	timeoutMS := fs.Int("timeout-ms", 0, "dial timeout (ms); 0 uses daemon default")
	noSpawn := fs.Bool("no-spawn", false, "fail instead of spawning the daemon")
	jar := fs.String("jar", "", "path to sofarpcd.jar (overrides autodiscovery)")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *addr == "" {
		fmt.Fprintln(env.Stderr, "ping: --address is required")
		return 2
	}
	resolved, err := resolveAddress(*addr)
	if err != nil {
		fmt.Fprintln(env.Stderr, "ping:", err)
		return 2
	}
	*addr = resolved

	payload := protocol.PingPayload{
		Address:      *addr,
		Service:      *service,
		RPCTimeoutMS: *timeoutMS,
	}
	req, err := protocol.NewRequest(protocol.OpPing, payload)
	if err != nil {
		fmt.Fprintln(env.Stderr, "ping: build request:", err)
		return 1
	}

	resp, err := dispatch(req, execConfig(env, *noSpawn, *jar))
	if err != nil {
		writeLocalFailure(env.Stdout, req.RequestID, protocol.CodeDaemonUnavailable, err.Error())
		return 1
	}
	if err := writeResponse(env.Stdout, resp); err != nil {
		fmt.Fprintln(env.Stderr, "ping: write response:", err)
		return 1
	}
	if !resp.OK {
		return 1
	}
	return 0
}
