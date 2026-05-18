package cli

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"testing"

	"github.com/sofarpc/cli/internal/protocol"
)

func TestExecStdinPingRoundTrip(t *testing.T) {
	_, cleanup := tempHome(t)
	defer cleanup()
	ln := startTCPListener(t)
	defer ln.Close()

	reqBody := protocol.Request{
		RequestID: "exec-test-1",
		Op:        protocol.OpPing,
		Payload:   mustMarshal(t, protocol.PingPayload{Address: ln.Addr().String()}),
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runExec([]string{"--stdin"}, Env{
		BuildVersion: "test",
		Stdin:        bytes.NewReader(mustMarshal(t, reqBody)),
		Stdout:       stdout,
		Stderr:       stderr,
	})

	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, stderr.String())
	}
	var resp protocol.Response
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &resp); err != nil {
		t.Fatalf("decode stdout: %v\nstdout=%s", err, stdout.String())
	}
	if !resp.OK || resp.Code != protocol.CodeSuccess || resp.RequestID != "exec-test-1" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestExecStdinUnsupportedOp(t *testing.T) {
	_, cleanup := tempHome(t)
	defer cleanup()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runExec([]string{"--stdin"}, Env{
		BuildVersion: "test",
		Stdin:        bytes.NewReader(mustMarshal(t, protocol.Request{Op: "health"})),
		Stdout:       stdout,
		Stderr:       stderr,
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit, got 0; stdout=%s", stdout.String())
	}
	var resp protocol.Response
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &resp); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}
	if resp.OK || resp.Code != protocol.CodeBadRequest {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func tempHome(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	base := dir + string(os.PathSeparator) + ".sofarpc"
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("mkdir .sofarpc: %v", err)
	}
	prevHome := os.Getenv("HOME")
	if err := os.Setenv("HOME", dir); err != nil {
		t.Fatalf("setenv HOME: %v", err)
	}
	return base, func() {
		_ = os.Setenv("HOME", prevHome)
	}
}

func startTCPListener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	return ln
}

func mustMarshal(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
