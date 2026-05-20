package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"

	"github.com/diandian921/sofarpc-cli/internal/app"
	"github.com/diandian921/sofarpc-cli/internal/presentation"
)

// runInvoke is the human-facing shortcut for an invocation. It maps flat flags
// to an app InvocationInput and emits the single rendered result contract.
func runInvoke(args []string, env Env) int {
	fs := flag.NewFlagSet("invoke", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	addr := fs.String("address", "", "target bolt address, e.g. 127.0.0.1:12200")
	service := fs.String("service", "", "fully qualified service interface")
	method := fs.String("method", "", "method name")
	argTypesCSV := fs.String("arg-types", "", "comma-separated Java argument types")
	argsJSON := fs.String("args-json", "[]", "JSON array of arguments")
	timeoutMS := fs.Int("timeout-ms", 0, "per-call RPC timeout (ms); 0 uses default")
	assertionsJSON := fs.String("assertions-json", "", "JSON array of assertion specs (optional)")

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

	input, specs, err := buildInvokeInput(*addr, *service, *method, *argTypesCSV, *argsJSON, *assertionsJSON, *timeoutMS)
	if err != nil {
		fmt.Fprintln(env.Stderr, "invoke:", err)
		return 2
	}

	result := executeInvoke(input, specs)
	result.RequestID = app.NewRequestID("invoke")
	if err := writeResult(env.Stdout, result); err != nil {
		fmt.Fprintln(env.Stderr, "invoke: write result:", err)
		return 1
	}
	if !result.OK {
		return 1
	}
	return 0
}

type assertionSpec struct {
	Path   string      `json:"path"`
	Equals interface{} `json:"equals,omitempty"`
	Exists *bool       `json:"exists,omitempty"`
}

func buildInvokeInput(addr, service, method, argTypesCSV, argsJSON, assertionsJSON string, timeoutMS int) (app.InvocationInput, []assertionSpec, error) {
	input := app.InvocationInput{
		Address:             addr,
		Service:             service,
		Method:              method,
		ParamTypes:          splitTrim(argTypesCSV),
		HasOrderedArguments: true,
		TimeoutMS:           timeoutMS,
	}
	var argsArr []interface{}
	dec := json.NewDecoder(strings.NewReader(argsJSON))
	dec.UseNumber()
	if err := dec.Decode(&argsArr); err != nil {
		return input, nil, fmt.Errorf("invalid --args-json: %w", err)
	}
	input.OrderedArguments = argsArr
	if len(input.ParamTypes) != len(input.OrderedArguments) {
		return input, nil, fmt.Errorf("argTypes (%d) and args (%d) length mismatch", len(input.ParamTypes), len(input.OrderedArguments))
	}
	var specs []assertionSpec
	if assertionsJSON != "" {
		if err := json.Unmarshal([]byte(assertionsJSON), &specs); err != nil {
			return input, nil, fmt.Errorf("invalid --assertions-json: %w", err)
		}
	}
	return input, specs, nil
}

func executeInvoke(input app.InvocationInput, specs []assertionSpec) app.Result {
	service := app.New(nil)
	plan, err := service.PlanInvocation(context.Background(), input)
	if err != nil {
		return app.RenderFailure(app.CodeBadRequest, err.Error(), app.DomainErrorDetails(err))
	}
	exec := service.ExecuteInvocation(context.Background(), plan)
	if exec.OK {
		applyAssertions(&exec, specs)
	}
	return app.RenderExecution(exec)
}

func applyAssertions(exec *app.InvocationExecution, specs []assertionSpec) {
	if exec == nil || len(specs) == 0 {
		return
	}
	assertions := make([]presentation.Assertion, len(specs))
	for i, item := range specs {
		assertions[i] = presentation.Assertion{Path: item.Path, Equals: item.Equals, Exists: item.Exists}
	}
	out, failed := presentation.EvaluateAssertions(exec.Data["result"], assertions)
	exec.Data["assertions"] = out
	if failed > 0 {
		exec.OK = false
		exec.Code = app.CodeAssertionFailed
		exec.Error = &app.ExecutionError{Message: fmt.Sprintf("%d of %d assertions failed", failed, len(out))}
	}
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
