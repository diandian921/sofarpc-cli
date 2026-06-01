package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"

	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

// Result is the single agent-facing rendered contract. MCP tool responses and
// human CLI commands both emit this shape; there is no separate envelope.
type Result struct {
	RequestID string                 `json:"requestId"`
	OK        bool                   `json:"ok"`
	Code      string                 `json:"code"`
	Data      json.RawMessage        `json:"data,omitempty"`
	Error     *ResultError           `json:"error,omitempty"`
	Meta      map[string]interface{} `json:"meta,omitempty"`
}

// ResultError carries diagnostic info on non-SUCCESS results. NextTool is the
// machine-readable recovery hint (which MCP tool to call next); Recovery is the
// same step spelled out for the agent, so it does not have to infer intent from
// the prose message.
type ResultError struct {
	Message  string                 `json:"message"`
	Cause    string                 `json:"cause,omitempty"`
	NextTool string                 `json:"nextTool,omitempty"`
	Recovery string                 `json:"recovery,omitempty"`
	Details  map[string]interface{} `json:"details,omitempty"`
}

// newResultError builds a ResultError with recovery hints derived from the stable
// code/kind, so every failure path emits a consistent nextTool + recovery step.
func newResultError(code, message, cause string, details map[string]interface{}) *ResultError {
	return &ResultError{
		Message:  message,
		Cause:    cause,
		NextTool: nextToolFor(code, details),
		Recovery: recoveryFor(code, details),
		Details:  details,
	}
}

// recoveryAdvice pairs the machine-readable recovery hint (which MCP tool to call
// next) with that same step spelled out in prose. Holding both on one entry is the
// point: nextTool and its recovery sentence can no longer drift apart.
type recoveryAdvice struct {
	nextTool string
	recovery string
}

// adviceByKind refines a failure when a DomainError kind is present. Each kind
// names the tool that recovers it and the prose step that mirrors that tool.
var adviceByKind = map[ErrorKind]recoveryAdvice{
	ErrProjectNotFound:      {"sofarpc_config_list", "Call sofarpc_config_list to see configured projects, or sofarpc_config_save_project to add one."},
	ErrServerNotFound:       {"sofarpc_config_list", "Call sofarpc_config_list to see configured servers, or sofarpc_config_save_server to add one."},
	ErrEndpointNotFound:     {"sofarpc_resolve", "More than one server matched. Pass an explicit server, or call sofarpc_resolve to pick one."},
	ErrServiceNotFound:      {"sofarpc_describe", "Call sofarpc_describe with query=... to find the service interface FQN."},
	ErrMethodNotFound:       {"sofarpc_describe", "Call sofarpc_describe with the service to list its methods and parameter types."},
	ErrMethodAmbiguous:      {"sofarpc_describe", "Overloaded method: pass paramTypes to disambiguate (candidates are in error.details.candidates)."},
	ErrArgumentTypeMismatch: {"sofarpc_describe", "Call sofarpc_describe to check the parameter types, then pass matching paramTypes/arguments."},
}

// adviceByCode is the fallback when no DomainError kind refines the failure: the
// stable failure code alone decides the recovery step.
var adviceByCode = map[string]recoveryAdvice{
	CodeConnectFailed:               {"sofarpc_probe", "Call sofarpc_probe to check the server address is reachable."},
	CodeRPCTimeout:                  {"sofarpc_probe", "Call sofarpc_probe to check the server address is reachable."},
	CodeBadRequest:                  {"sofarpc_describe", "Call sofarpc_describe to confirm the service, method, and argument shape."},
	CodeInvokeFailed:                {"sofarpc_doctor", "Call sofarpc_doctor to diagnose config, source schema, and connectivity."},
	CodeInternalError:               {"sofarpc_doctor", "Call sofarpc_doctor to diagnose config, source schema, and connectivity."},
	appconfig.CodeConfigInvalid:     {"sofarpc_doctor", "Call sofarpc_doctor to inspect the configuration problem."},
	appconfig.CodeConfigUnsupported: {"sofarpc_doctor", "Call sofarpc_doctor to inspect the configuration problem."},
}

// adviceFor resolves the recovery advice for a failure: a DomainError kind, when
// present, refines it; otherwise the stable code decides. The zero value (no
// nextTool, no recovery) means there is no specific guidance.
func adviceFor(code string, details map[string]interface{}) recoveryAdvice {
	if kind, _ := details["kind"].(string); kind != "" {
		if advice, ok := adviceByKind[ErrorKind(kind)]; ok {
			return advice
		}
	}
	return adviceByCode[code]
}

// nextToolFor maps a stable failure code (refined by DomainError kind when
// present) to the MCP tool an agent should call to recover. Empty means no step.
func nextToolFor(code string, details map[string]interface{}) string {
	return adviceFor(code, details).nextTool
}

// recoveryFor spells out the next step in prose, paired with nextToolFor through
// the same advice entry. Empty means no specific guidance.
func recoveryFor(code string, details map[string]interface{}) string {
	return adviceFor(code, details).recovery
}

// NewRequestID returns a short, unique-ish identifier prefixed with the op.
func NewRequestID(prefix string) string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return prefix + "-fallback"
	}
	return prefix + "-" + hex.EncodeToString(buf[:])
}

// RenderExecution turns an invocation execution into the rendered contract.
func RenderExecution(exec InvocationExecution) Result {
	result := Result{OK: exec.OK, Code: exec.Code, Meta: exec.Meta}
	// Data is emitted whenever present, not only on success: an assertion
	// failure keeps OK=false but still carries data.result and data.assertions.
	if len(exec.Data) > 0 {
		body, err := json.Marshal(exec.Data)
		if err != nil {
			return RenderFailure(CodeInternalError, err.Error(), nil)
		}
		result.Data = body
	}
	if !exec.OK && exec.Error != nil {
		result.Error = newResultError(exec.Code, exec.Error.Message, exec.Error.Cause, exec.Error.Details)
	}
	return result
}

// RenderProbe turns a probe outcome into the rendered contract.
func RenderProbe(probe ProbeResult) Result {
	if probe.Error != nil {
		code := probe.Code
		if code == "" {
			code = CodeConnectFailed
		}
		return Result{
			OK:    false,
			Code:  code,
			Error: newResultError(code, probe.Error.Message, probe.Error.Cause, probe.Error.Details),
			Meta:  probe.Meta,
		}
	}
	body, err := json.Marshal(map[string]interface{}{
		"address":     probe.Address,
		"service":     probe.Service,
		"reachable":   probe.Reachable,
		"elapsedMs":   probe.ElapsedMS,
		"diagnostics": probe.Diagnostics,
	})
	if err != nil {
		return RenderFailure(CodeInternalError, err.Error(), nil)
	}
	return Result{OK: true, Code: CodeSuccess, Data: body, Meta: probe.Meta}
}

// RenderFailure builds a non-OK result for a local failure before or during planning.
func RenderFailure(code, message string, details map[string]interface{}) Result {
	return Result{
		OK:    false,
		Code:  code,
		Error: newResultError(code, message, "", details),
		Meta:  map[string]interface{}{"runtime": "go"},
	}
}
