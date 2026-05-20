package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"

	"github.com/diandian921/sofarpc-cli/internal/appconfig"
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

// ResultError carries diagnostic info on non-SUCCESS results. NextTool is a
// machine-readable recovery hint: the agent should call that MCP tool next
// instead of re-deriving the recovery step from the prose message.
type ResultError struct {
	Message  string                 `json:"message"`
	Cause    string                 `json:"cause,omitempty"`
	NextTool string                 `json:"nextTool,omitempty"`
	Details  map[string]interface{} `json:"details,omitempty"`
}

// newResultError builds a ResultError with a recovery hint derived from the
// stable code/kind, so every failure path emits a consistent nextTool.
func newResultError(code, message, cause string, details map[string]interface{}) *ResultError {
	return &ResultError{
		Message:  message,
		Cause:    cause,
		NextTool: nextToolFor(code, details),
		Details:  details,
	}
}

// nextToolFor maps a stable failure code (refined by DomainError kind when
// present) to the MCP tool an agent should call to recover. An empty string
// means no specific next step.
func nextToolFor(code string, details map[string]interface{}) string {
	if kind, _ := details["kind"].(string); kind != "" {
		switch ErrorKind(kind) {
		case ErrProjectNotFound, ErrServerNotFound:
			return "sofarpc_config"
		case ErrEndpointNotFound:
			return "sofarpc_resolve"
		case ErrServiceNotFound, ErrMethodNotFound, ErrMethodAmbiguous, ErrArgumentTypeMismatch:
			return "sofarpc_describe"
		}
	}
	switch code {
	case CodeConnectFailed, CodeRPCTimeout:
		return "sofarpc_probe"
	case CodeBadRequest:
		return "sofarpc_describe"
	case CodeInvokeFailed, CodeInternalError:
		return "sofarpc_doctor"
	case appconfig.CodeConfigInvalid, appconfig.CodeConfigUnsupported:
		return "sofarpc_doctor"
	}
	return ""
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
