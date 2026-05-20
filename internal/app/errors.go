package app

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/diandian921/sofarpc-cli/internal/direct"
)

const (
	CodeSuccess         = "SUCCESS"
	CodeBadRequest      = "BAD_REQUEST"
	CodeConnectFailed   = "CONNECT_FAILED"
	CodeRPCTimeout      = "RPC_TIMEOUT"
	CodeInvokeFailed    = "INVOKE_FAILED"
	CodeAssertionFailed = "ASSERTION_FAILED"
	CodeInternalError   = "INTERNAL_ERROR"
)

type ErrorKind string

const (
	ErrProjectNotFound      ErrorKind = "PROJECT_NOT_FOUND"
	ErrServerNotFound       ErrorKind = "SERVER_NOT_FOUND"
	ErrServiceNotFound      ErrorKind = "SERVICE_NOT_FOUND"
	ErrEndpointNotFound     ErrorKind = "ENDPOINT_NOT_FOUND"
	ErrMethodNotFound       ErrorKind = "METHOD_NOT_FOUND"
	ErrMethodAmbiguous      ErrorKind = "METHOD_AMBIGUOUS"
	ErrArgumentTypeMismatch ErrorKind = "ARGUMENT_TYPE_MISMATCH"
)

type DomainError struct {
	Kind    ErrorKind
	Message string
	Details map[string]interface{}
}

func (e *DomainError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func DomainErrorDetails(err error) map[string]interface{} {
	var domain *DomainError
	if !errors.As(err, &domain) {
		return nil
	}
	details := map[string]interface{}{"kind": string(domain.Kind)}
	for k, v := range domain.Details {
		details[k] = v
	}
	return details
}

func errorCode(err error) string {
	var connectErr *direct.ConnectError
	if errors.As(err, &connectErr) {
		return CodeConnectFailed
	}
	var remote *direct.RemoteError
	if errors.As(err, &remote) {
		return CodeInvokeFailed
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return CodeRPCTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return CodeRPCTimeout
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return CodeConnectFailed
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		return CodeConnectFailed
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "timeout") {
		return CodeRPCTimeout
	}
	if strings.Contains(msg, "connection refused") || strings.Contains(msg, "no such host") {
		return CodeConnectFailed
	}
	return CodeInvokeFailed
}
