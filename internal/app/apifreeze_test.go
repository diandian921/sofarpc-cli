package app

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// jsonFields returns the sorted JSON wire names of an exported struct, skipping
// json:"-" fields and falling back to the Go field name when no tag is present.
func jsonFields(t reflect.Type) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		if tag == "-" {
			continue
		}
		name := tag
		if comma := strings.IndexByte(name, ','); comma >= 0 {
			name = name[:comma]
		}
		if name == "" {
			name = t.Field(i).Name
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// TestAppPublicTypesFrozen pins the agent-facing app wire contract for the
// duration of the MCP three-layer refactor. Changing any of these field sets is
// a breaking change to the shared CLI/MCP render contract and must be a
// deliberate, separate commit — not an accidental side effect of the refactor.
func TestAppPublicTypesFrozen(t *testing.T) {
	want := map[string][]string{
		"Result":              {"code", "data", "error", "meta", "ok", "requestId"},
		"ResultError":         {"cause", "details", "message", "nextTool", "recovery"},
		"Endpoint":            {"address", "appName", "attachments", "project", "protocol", "server", "timeoutMs"},
		"ProjectRef":          {"info", "name"},
		"MethodSignature":     {"name", "paramTypes"},
		"PlanWarning":         {"code", "details", "message"},
		"Diagnostics":         {"resolution", "timing", "warnings"},
		"ResolveResult":       {"diagnostics", "endpoint", "network", "project", "server", "servers"},
		"InvocationPlan":      {"arguments", "diagnostics", "endpoint", "method", "project", "rawResult", "server", "service", "timeoutMs", "warnings"},
		"InvocationExecution": {"code", "data", "error", "meta", "ok"},
		"ExecutionError":      {"cause", "details", "message"},
		"ProbeResult":         {"address", "diagnostics", "elapsedMs", "error", "meta", "project", "reachable", "server", "service", "timeoutMs"},
	}
	got := map[string][]string{
		"Result":              jsonFields(reflect.TypeOf(Result{})),
		"ResultError":         jsonFields(reflect.TypeOf(ResultError{})),
		"Endpoint":            jsonFields(reflect.TypeOf(Endpoint{})),
		"ProjectRef":          jsonFields(reflect.TypeOf(ProjectRef{})),
		"MethodSignature":     jsonFields(reflect.TypeOf(MethodSignature{})),
		"PlanWarning":         jsonFields(reflect.TypeOf(PlanWarning{})),
		"Diagnostics":         jsonFields(reflect.TypeOf(Diagnostics{})),
		"ResolveResult":       jsonFields(reflect.TypeOf(ResolveResult{})),
		"InvocationPlan":      jsonFields(reflect.TypeOf(InvocationPlan{})),
		"InvocationExecution": jsonFields(reflect.TypeOf(InvocationExecution{})),
		"ExecutionError":      jsonFields(reflect.TypeOf(ExecutionError{})),
		"ProbeResult":         jsonFields(reflect.TypeOf(ProbeResult{})),
	}
	for name, w := range want {
		if !reflect.DeepEqual(got[name], w) {
			t.Fatalf("app.%s wire fields drifted:\n got  %v\n want %v", name, got[name], w)
		}
	}
}

// Compile-time freeze of the app.Service surface that CLI and MCP both consume.
// A signature change breaks this var block before it breaks callers.
var _ = []interface{}{
	(*Service).PlanInvocation,
	(*Service).ExecuteInvocation,
	(*Service).Resolve,
	(*Service).ProbeEndpoint,
	New,
	RenderExecution,
	RenderProbe,
	RenderFailure,
	NewRequestID,
	DomainErrorDetails,
}
