package launcher

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

const (
	ReasonDaemonUnavailable      = "daemon_unavailable"
	ReasonNoSpawn                = "no_spawn"
	ReasonJavaNotFound           = "java_not_found"
	ReasonJavaVersionUnsupported = "java_version_unsupported"
	ReasonEngineJarNotFound      = "engine_jar_not_found"
	ReasonEngineStartTimeout     = "engine_start_timeout"
	ReasonPortOccupied           = "port_occupied"
	ReasonSpawnFailed            = "spawn_failed"
)

// DiagnosticError is a structured launcher failure. CLI and MCP callers can turn
// it into stable JSON instead of forcing agents to inspect local logs manually.
type DiagnosticError struct {
	Reason  string                 `json:"reason"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
	Cause   error                  `json:"-"`
}

func NewDiagnosticError(reason, message string, cause error) *DiagnosticError {
	return &DiagnosticError{
		Reason:  reason,
		Message: message,
		Cause:   cause,
	}
}

func (e *DiagnosticError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *DiagnosticError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *DiagnosticError) WithDetail(key string, value interface{}) *DiagnosticError {
	if e.Details == nil {
		e.Details = make(map[string]interface{})
	}
	e.Details[key] = value
	return e
}

func (e *DiagnosticError) WithLogTail(path string, maxBytes int) *DiagnosticError {
	tail, err := TailFile(path, maxBytes)
	if err == nil && tail != "" {
		e.WithDetail("logFile", path)
		e.WithDetail("logTail", tail)
	}
	return e
}

func AsDiagnostic(err error) (*DiagnosticError, bool) {
	var diag *DiagnosticError
	if errors.As(err, &diag) {
		return diag, true
	}
	return nil, false
}

func TailFile(path string, maxBytes int) (string, error) {
	if maxBytes <= 0 {
		return "", nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := stat.Size()
	offset := int64(0)
	if size > int64(maxBytes) {
		offset = size - int64(maxBytes)
	}
	buf := make([]byte, size-offset)
	if _, err := f.ReadAt(buf, offset); err != nil {
		if errors.Is(err, io.EOF) {
			return string(buf), nil
		}
		return "", fmt.Errorf("read tail %s: %w", path, err)
	}
	return string(buf), nil
}

func isExecNotFound(err error) bool {
	return errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist)
}
