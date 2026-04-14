package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/sofarpc/cli/internal/protocol"
)

// Each of these tests reuses the fake-daemon + tempHome scaffolding from exec_test.go to
// verify that the flag-driven subcommands build the right envelope and interpret the reply.

func TestInvokeSubcommandSendsEnvelope(t *testing.T) {
	dir, cleanup := tempHome(t)
	defer cleanup()

	srv, port := startFakeDaemon(t)
	defer srv.Close()
	writeState(t, filepath.Join(dir, "daemon", "state.json"), port)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runInvoke([]string{
		"--address", "127.0.0.1:12200",
		"--service", "com.example.UserService",
		"--method", "getUser",
		"--arg-types", "java.lang.String",
		"--args-json", `["u001"]`,
		"--no-spawn",
	}, Env{BuildVersion: "test", Stdout: stdout, Stderr: stderr})

	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, stderr.String())
	}
	var resp protocol.Response
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &resp); err != nil {
		t.Fatalf("decode: %v, out=%s", err, stdout.String())
	}
	if !resp.OK || resp.Code != protocol.CodeSuccess {
		t.Fatalf("bad resp: %+v", resp)
	}
}

func TestInvokeRejectsArgTypeMismatch(t *testing.T) {
	_, cleanup := tempHome(t)
	defer cleanup()

	stderr := &bytes.Buffer{}
	code := runInvoke([]string{
		"--address", "127.0.0.1:12200",
		"--service", "com.example.UserService",
		"--method", "getUser",
		"--arg-types", "java.lang.String",
		"--args-json", `["u001", 42]`,
	}, Env{BuildVersion: "test", Stdout: &bytes.Buffer{}, Stderr: stderr})

	if code != 2 {
		t.Fatalf("expected exit 2, got %d; stderr=%s", code, stderr.String())
	}
}

func TestPingSubcommandSendsEnvelope(t *testing.T) {
	dir, cleanup := tempHome(t)
	defer cleanup()

	srv, port := startFakeDaemon(t)
	defer srv.Close()
	writeState(t, filepath.Join(dir, "daemon", "state.json"), port)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runPing([]string{"--address", "127.0.0.1:9999", "--no-spawn"},
		Env{BuildVersion: "test", Stdout: stdout, Stderr: stderr})
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, stderr.String())
	}
	var resp protocol.Response
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK {
		t.Fatalf("bad resp: %+v", resp)
	}
}

func TestDaemonStatusWhenNoState(t *testing.T) {
	_, cleanup := tempHome(t)
	defer cleanup()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runDaemon([]string{"status"}, Env{BuildVersion: "test", Stdout: stdout, Stderr: stderr})
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, stderr.String())
	}
	var result map[string]interface{}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if running, _ := result["running"].(bool); running {
		t.Fatalf("expected running=false, got %v", result)
	}
}

func TestDaemonStatusReportsRunning(t *testing.T) {
	dir, cleanup := tempHome(t)
	defer cleanup()

	srv, port := startFakeDaemon(t)
	defer srv.Close()
	writeState(t, filepath.Join(dir, "daemon", "state.json"), port)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runDaemon([]string{"status"}, Env{BuildVersion: "test", Stdout: stdout, Stderr: stderr})
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, stderr.String())
	}
	var result map[string]interface{}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &result); err != nil {
		t.Fatalf("decode: %v, out=%s", err, stdout.String())
	}
	if ok, _ := result["ok"].(bool); !ok {
		t.Fatalf("expected ok=true, got %+v", result)
	}
	if running, _ := result["running"].(bool); !running {
		t.Fatalf("expected running=true, got %+v", result)
	}
}
