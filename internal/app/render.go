package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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

// ResultError carries diagnostic info on non-SUCCESS results.
type ResultError struct {
	Message string                 `json:"message"`
	Cause   string                 `json:"cause,omitempty"`
	Details map[string]interface{} `json:"details,omitempty"`
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
		result.Error = &ResultError{
			Message: exec.Error.Message,
			Cause:   exec.Error.Cause,
			Details: exec.Error.Details,
		}
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
			OK:   false,
			Code: code,
			Error: &ResultError{
				Message: probe.Error.Message,
				Cause:   probe.Error.Cause,
				Details: probe.Error.Details,
			},
			Meta: probe.Meta,
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
		OK:   false,
		Code: code,
		Error: &ResultError{
			Message: message,
			Details: details,
		},
		Meta: map[string]interface{}{"runtime": "go"},
	}
}
