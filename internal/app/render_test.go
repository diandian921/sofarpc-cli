package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderExecutionKeepsDataOnAssertionFailure(t *testing.T) {
	exec := InvocationExecution{
		OK:   false,
		Code: CodeAssertionFailed,
		Data: map[string]interface{}{
			"result":     map[string]interface{}{"name": "alice"},
			"assertions": []interface{}{map[string]interface{}{"path": "$.name", "passed": false}},
		},
		Error: &ExecutionError{Message: "1 of 1 assertions failed"},
	}
	result := RenderExecution(exec)

	if result.OK || result.Code != CodeAssertionFailed {
		t.Fatalf("unexpected ok/code: %+v", result)
	}
	if result.Error == nil || result.Error.Message == "" {
		t.Fatalf("assertion failure must keep the error: %+v", result)
	}
	if len(result.Data) == 0 {
		t.Fatalf("assertion failure must keep data.result and data.assertions, got empty data")
	}
	var data map[string]interface{}
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("data not valid JSON: %v", err)
	}
	if _, ok := data["result"]; !ok {
		t.Fatalf("data.result dropped: %s", string(result.Data))
	}
	if _, ok := data["assertions"]; !ok {
		t.Fatalf("data.assertions dropped: %s", string(result.Data))
	}
}

func TestRenderProbeUsesProbeCode(t *testing.T) {
	probe := ProbeResult{
		Address: "10.0.0.1:1",
		Error:   &ExecutionError{Message: "config read failed"},
		Code:    CodeInternalError,
	}
	result := RenderProbe(probe)
	if result.OK {
		t.Fatalf("expected failure: %+v", result)
	}
	if result.Code != CodeInternalError {
		t.Fatalf("probe code = %q, want %q (must not be flattened to CONNECT_FAILED)", result.Code, CodeInternalError)
	}
}

func TestRenderProbeDefaultsToConnectFailed(t *testing.T) {
	result := RenderProbe(ProbeResult{Error: &ExecutionError{Message: "dial failed"}})
	if result.Code != CodeConnectFailed {
		t.Fatalf("empty probe code should default to CONNECT_FAILED, got %q", result.Code)
	}
}

func TestNextToolForMapsCodesAndKinds(t *testing.T) {
	cases := []struct {
		name    string
		code    string
		details map[string]interface{}
		want    string
	}{
		{"connect", CodeConnectFailed, nil, "sofarpc_probe"},
		{"timeout", CodeRPCTimeout, nil, "sofarpc_probe"},
		{"bad-request", CodeBadRequest, nil, ""},
		{"invoke-failed", CodeInvokeFailed, nil, "sofarpc_doctor"},
		{"internal", CodeInternalError, nil, "sofarpc_doctor"},
		{"config-invalid", "CONFIG_INVALID", nil, "sofarpc_doctor"},
		{"kind-project", CodeBadRequest, map[string]interface{}{"kind": string(ErrProjectNotFound)}, "sofarpc_config_list"},
		{"kind-endpoint", CodeInvokeFailed, map[string]interface{}{"kind": string(ErrEndpointNotFound)}, "sofarpc_resolve"},
		{"kind-method", CodeBadRequest, map[string]interface{}{"kind": string(ErrMethodAmbiguous)}, "sofarpc_describe"},
		{"unknown", "WEIRD_CODE", nil, ""},
		{"success", CodeSuccess, nil, ""},
	}
	for _, c := range cases {
		if got := nextToolFor(c.code, c.details); got != c.want {
			t.Fatalf("%s: nextToolFor(%q,%v)=%q want %q", c.name, c.code, c.details, got, c.want)
		}
	}
}

func TestRenderFailureSetsNextTool(t *testing.T) {
	r := RenderFailure(CodeConnectFailed, "boom", nil)
	if r.Error == nil || r.Error.NextTool != "sofarpc_probe" {
		t.Fatalf("RenderFailure nextTool = %+v, want sofarpc_probe", r.Error)
	}
}

func TestRecoveryForGivesActionableStep(t *testing.T) {
	cases := []struct {
		name    string
		code    string
		details map[string]interface{}
		want    string
	}{
		{"server-missing", CodeBadRequest, map[string]interface{}{"kind": string(ErrServerNotFound)}, "sofarpc_config_list"},
		{"ambiguous", CodeBadRequest, map[string]interface{}{"kind": string(ErrMethodAmbiguous)}, "paramTypes"},
		{"method-missing", CodeBadRequest, map[string]interface{}{"kind": string(ErrMethodNotFound)}, "sofarpc_describe"},
		{"connect", CodeConnectFailed, nil, "sofarpc_probe"},
		{"success", CodeSuccess, nil, ""},
	}
	for _, c := range cases {
		got := recoveryFor(c.code, c.details)
		if c.want == "" {
			if got != "" {
				t.Fatalf("%s: recoveryFor=%q, want empty", c.name, got)
			}
			continue
		}
		if !strings.Contains(got, c.want) {
			t.Fatalf("%s: recoveryFor(%q,%v)=%q, want it to mention %q", c.name, c.code, c.details, got, c.want)
		}
	}
}

func TestRenderFailureSetsRecovery(t *testing.T) {
	r := RenderFailure(CodeBadRequest, "no server", map[string]interface{}{"kind": string(ErrServerNotFound)})
	if r.Error == nil || !strings.Contains(r.Error.Recovery, "sofarpc_config_list") {
		t.Fatalf("RenderFailure recovery = %+v, want it to mention sofarpc_config_list", r.Error)
	}
}

// TestRenderProbeFailureDoesNotPointAtProbe pins the advice invariant: a failed
// probe must never recommend calling sofarpc_probe again; doctor owns diagnosis.
func TestRenderProbeFailureDoesNotPointAtProbe(t *testing.T) {
	result := RenderProbe(ProbeResult{Error: &ExecutionError{Message: "dial failed"}})
	if result.Error == nil {
		t.Fatal("expected an error envelope")
	}
	if result.Error.NextTool == "sofarpc_probe" {
		t.Fatalf("probe failure must not point back at probe: %+v", result.Error)
	}
	if result.Error.NextTool != "sofarpc_doctor" {
		t.Fatalf("probe failure nextTool = %q, want sofarpc_doctor", result.Error.NextTool)
	}
}

// TestRenderFailureAdvisedPinsAdvice checks the caller-pinned advice overrides
// the table-driven fallback.
func TestRenderFailureAdvisedPinsAdvice(t *testing.T) {
	r := RenderFailureAdvised(CodeBadRequest, "query or service is required", nil, "", "Pass query or service.")
	if r.Error == nil || r.Error.NextTool != "" || r.Error.Recovery != "Pass query or service." {
		t.Fatalf("advised failure not pinned: %+v", r.Error)
	}
}

func TestRenderSuccessHasNoNextTool(t *testing.T) {
	r := RenderProbe(ProbeResult{Reachable: true, Address: "h:1", Meta: map[string]interface{}{}})
	if r.Error != nil {
		t.Fatalf("successful probe must have no error/nextTool, got %+v", r.Error)
	}
}
