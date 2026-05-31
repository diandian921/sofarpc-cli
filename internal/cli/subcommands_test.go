package cli

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"testing"

	"github.com/diandian921/sofarpc-cli/internal/app"
)

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

func TestPingSubcommandRendersResult(t *testing.T) {
	_, cleanup := tempHome(t)
	defer cleanup()
	ln := startTCPListener(t)
	defer ln.Close()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runPing([]string{ln.Addr().String()},
		Env{BuildVersion: "test", Stdout: stdout, Stderr: stderr})
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, stderr.String())
	}
	var resp app.Result
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK {
		t.Fatalf("bad resp: %+v", resp)
	}
}

func TestPingAcceptsFlagsAfterPositional(t *testing.T) {
	_, cleanup := tempHome(t)
	defer cleanup()
	ln := startTCPListener(t)
	defer ln.Close()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runPing([]string{ln.Addr().String(), "--service", "com.x.Foo"},
		Env{BuildVersion: "test", Stdout: stdout, Stderr: stderr})
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, stderr.String())
	}
}

func TestServerAddAcceptsFlagAfterPositionals(t *testing.T) {
	dir, cleanup := tempHome(t)
	defer cleanup()

	projectOut := &bytes.Buffer{}
	projectErr := &bytes.Buffer{}
	projectCode := runProject([]string{"add", "user", dir, "--prefix", "com.example"},
		Env{BuildVersion: "test", Stdout: projectOut, Stderr: projectErr})
	if projectCode != 0 {
		t.Fatalf("project add exit = %d; stderr=%s", projectCode, projectErr.String())
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runServerAdd([]string{"user-test", "10.0.0.1:12200", "--project", "user", "--timeout-ms", "3000"},
		Env{BuildVersion: "test", Stdout: stdout, Stderr: stderr})
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, stderr.String())
	}
	var out map[string]interface{}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["name"] != "user-test" {
		t.Fatalf("unexpected output: %+v", out)
	}
}
