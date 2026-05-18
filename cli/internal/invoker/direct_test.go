package invoker

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sofarpc/cli/internal/direct"
	"github.com/sofarpc/cli/internal/protocol"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return false }

func TestErrorCodeUsesTypedErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "connect", err: &direct.ConnectError{Address: "127.0.0.1:1", Err: context.Canceled}, want: protocol.CodeConnectFailed},
		{name: "remote", err: &direct.RemoteError{Message: "remote failed"}, want: protocol.CodeInvokeFailed},
		{name: "deadline", err: context.DeadlineExceeded, want: protocol.CodeRPCTimeout},
		{name: "net timeout", err: timeoutErr{}, want: protocol.CodeRPCTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ErrorCode(tt.err); got != tt.want {
				t.Fatalf("ErrorCode(%T) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

func TestDecodeInvokePayloadPreservesJSONNumbers(t *testing.T) {
	payload, err := DecodeInvokePayload(json.RawMessage(`{
		"address":"127.0.0.1:12200",
		"service":"com.example.Facade",
		"method":"query",
		"argTypes":["java.lang.Long"],
		"args":[433905635109773312]
	}`))
	if err != nil {
		t.Fatalf("DecodeInvokePayload: %v", err)
	}
	n, ok := payload.Args[0].(json.Number)
	if !ok {
		t.Fatalf("arg type = %T, want json.Number", payload.Args[0])
	}
	if n.String() != "433905635109773312" {
		t.Fatalf("arg = %s", n.String())
	}
}

func TestDirectRequestPreservesRequestIDOnBadPayload(t *testing.T) {
	resp, err := DirectRequest(protocol.Request{
		RequestID: "invoke-test",
		Op:        protocol.OpInvoke,
		Payload:   json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("DirectRequest: %v", err)
	}
	if resp.RequestID != "invoke-test" {
		t.Fatalf("requestId = %q", resp.RequestID)
	}
	if resp.OK || resp.Code != protocol.CodeBadRequest {
		t.Fatalf("response = %+v", resp)
	}
}
