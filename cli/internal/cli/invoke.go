package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"

	"github.com/sofarpc/cli/internal/protocol"
)

// runInvoke is the human-facing shortcut for OpInvoke. Agents should prefer `exec --stdin`
// because it avoids shell-quoting JSON; this subcommand maps flat flags to the same envelope.
func runInvoke(args []string, env Env) int {
	fs := flag.NewFlagSet("invoke", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	addr := fs.String("address", "", "target bolt address, e.g. 127.0.0.1:12200")
	service := fs.String("service", "", "fully qualified service interface")
	method := fs.String("method", "", "method name")
	argTypesCSV := fs.String("arg-types", "", "comma-separated Java argument types")
	argsJSON := fs.String("args-json", "[]", "JSON array of arguments")
	timeoutMS := fs.Int("timeout-ms", 0, "per-call RPC timeout (ms); 0 uses Engine default")
	assertionsJSON := fs.String("assertions-json", "", "JSON array of assertion specs (optional)")
	engineMode := fs.String("engine", "", "invoke engine: java, go, or auto; empty uses config")
	noSpawn := fs.Bool("no-spawn", false, "fail instead of spawning the Engine")
	jar := fs.String("jar", "", "path to sofarpc-engine.jar (overrides autodiscovery)")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *addr == "" || *service == "" || *method == "" {
		fmt.Fprintln(env.Stderr, "invoke: --address, --service, --method are required")
		return 2
	}
	resolved, err := resolveAddress(*addr)
	if err != nil {
		fmt.Fprintln(env.Stderr, "invoke:", err)
		return 2
	}
	*addr = resolved

	payload, err := buildInvokePayload(*addr, *service, *method, *argTypesCSV, *argsJSON, *assertionsJSON, *timeoutMS)
	if err != nil {
		fmt.Fprintln(env.Stderr, "invoke:", err)
		return 2
	}

	req, err := protocol.NewRequest(protocol.OpInvoke, payload)
	if err != nil {
		fmt.Fprintln(env.Stderr, "invoke: build request:", err)
		return 1
	}
	if strings.TrimSpace(*engineMode) != "" {
		req.Meta = map[string]interface{}{"engine": strings.TrimSpace(*engineMode)}
	}

	resp, err := dispatch(req, execConfig(env, *noSpawn, *jar))
	if err != nil {
		writeDispatchFailure(env.Stdout, req.RequestID, err)
		return 1
	}
	if err := writeResponse(env.Stdout, resp); err != nil {
		fmt.Fprintln(env.Stderr, "invoke: write response:", err)
		return 1
	}
	if !resp.OK {
		return 1
	}
	return 0
}

func buildInvokePayload(addr, service, method, argTypesCSV, argsJSON, assertionsJSON string, timeoutMS int) (protocol.InvokePayload, error) {
	payload := protocol.InvokePayload{
		Address:      addr,
		Service:      service,
		Method:       method,
		ArgTypes:     splitTrim(argTypesCSV),
		RPCTimeoutMS: timeoutMS,
	}
	var argsArr []interface{}
	dec := json.NewDecoder(strings.NewReader(argsJSON))
	dec.UseNumber()
	if err := dec.Decode(&argsArr); err != nil {
		return payload, fmt.Errorf("invalid --args-json: %w", err)
	}
	payload.Args = argsArr
	if len(payload.ArgTypes) != len(payload.Args) {
		return payload, fmt.Errorf("argTypes (%d) and args (%d) length mismatch", len(payload.ArgTypes), len(payload.Args))
	}
	if assertionsJSON != "" {
		var specs []protocol.AssertionSpec
		if err := json.Unmarshal([]byte(assertionsJSON), &specs); err != nil {
			return payload, fmt.Errorf("invalid --assertions-json: %w", err)
		}
		payload.Assertions = specs
	}
	return payload, nil
}

func splitTrim(csv string) []string {
	if csv == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
