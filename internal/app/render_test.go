package app

import (
	"encoding/json"
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
		{"bad-request", CodeBadRequest, nil, "sofarpc_describe"},
		{"invoke-failed", CodeInvokeFailed, nil, "sofarpc_doctor"},
		{"internal", CodeInternalError, nil, "sofarpc_doctor"},
		{"config-invalid", "CONFIG_INVALID", nil, "sofarpc_doctor"},
		{"kind-project", CodeBadRequest, map[string]interface{}{"kind": string(ErrProjectNotFound)}, "sofarpc_config"},
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

func TestRenderSuccessHasNoNextTool(t *testing.T) {
	r := RenderProbe(ProbeResult{Reachable: true, Address: "h:1", Meta: map[string]interface{}{}})
	if r.Error != nil {
		t.Fatalf("successful probe must have no error/nextTool, got %+v", r.Error)
	}
}
