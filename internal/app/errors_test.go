package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/diandian921/sofarpc-mcp/internal/direct"
)

func TestErrorCodeClassification(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"canceled", context.Canceled, CodeCanceled},
		{"canceled wrapped", fmt.Errorf("dial: %w", context.Canceled), CodeCanceled},
		{"connect error", &direct.ConnectError{Err: errors.New("refused")}, CodeConnectFailed},
		{"remote error", &direct.RemoteError{Message: "boom"}, CodeInvokeFailed},
		{"deadline exceeded", context.DeadlineExceeded, CodeRPCTimeout},
		{"net timeout", timeoutErr{}, CodeRPCTimeout},
		{"dns error", &net.DNSError{Err: "no such host", Name: "x"}, CodeConnectFailed},
		{"dial op error", &net.OpError{Op: "dial", Err: errors.New("x")}, CodeConnectFailed},
		{"sniff io timeout", errors.New("read tcp: i/o timeout"), CodeRPCTimeout},
		{"sniff connection refused", errors.New("connection refused"), CodeConnectFailed},
		{"bare timeout word not matched", errors.New("configuration timeout policy invalid"), CodeInvokeFailed},
		{"default", errors.New("something else"), CodeInvokeFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := errorCode(tc.err); got != tc.want {
				t.Fatalf("errorCode(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// timeoutErr is a net.Error whose Timeout() is true, exercising the typed
// net.Error timeout branch independent of the string sniff.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "operation timed out" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return false }
