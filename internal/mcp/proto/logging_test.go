package proto

import (
	"bytes"
	"strings"
	"testing"
)

func TestLoggingSetLevelIsAcknowledged(t *testing.T) {
	out := &bytes.Buffer{}
	in := handshakeFrames() + `{"jsonrpc":"2.0","id":5,"method":"logging/setLevel","params":{"level":"warning"}}` + "\n"
	if code := newTestSession(strings.NewReader(in), out).Run(); code != 0 {
		t.Fatalf("Run = %d", code)
	}
	if !strings.Contains(out.String(), `"id":5`) || strings.Contains(out.String(), `"code":-32601`) {
		t.Fatalf("logging/setLevel must be acknowledged, not method-not-found: %s", out.String())
	}
}

func TestLoggingSetLevelBeforeHandshakeRejected(t *testing.T) {
	out := &bytes.Buffer{}
	in := `{"jsonrpc":"2.0","id":5,"method":"logging/setLevel","params":{"level":"warning"}}` + "\n"
	if code := newTestSession(strings.NewReader(in), out).Run(); code != 0 {
		t.Fatalf("Run = %d", code)
	}
	if !strings.Contains(out.String(), `"code":-32002`) {
		t.Fatalf("logging/setLevel before handshake must be -32002: %s", out.String())
	}
}
