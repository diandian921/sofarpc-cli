package cli

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sofarpc/cli/internal/ipc"
	"github.com/sofarpc/cli/internal/launcher"
	"github.com/sofarpc/cli/internal/protocol"
)

// TestExecStdinRoundTrip drives the real exec handler end-to-end against an in-process fake
// daemon. The fake writes a canned state.json, accepts one framed request, echoes a success
// envelope, and the test asserts stdout matches the wire format.
func TestExecStdinRoundTrip(t *testing.T) {
	dir, cleanup := tempHome(t)
	defer cleanup()

	srv, port := startFakeDaemon(t)
	defer srv.Close()

	writeState(t, filepath.Join(dir, "daemon", "state.json"), port)

	reqBody := protocol.Request{
		RequestID: "exec-test-1",
		Op:        protocol.OpPing,
		Payload:   json.RawMessage(`{"address":"127.0.0.1:9999"}`),
	}
	stdin := mustMarshal(t, reqBody)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runExec([]string{"--stdin", "--no-spawn"}, Env{
		BuildVersion: "test",
		Stdin:        bytes.NewReader(stdin),
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

// TestExecStdinNoSpawnWithoutDaemon asserts that when --no-spawn is set and no daemon is
// reachable, the client returns a daemon-shaped failure envelope on stdout and exits non-zero.
func TestExecStdinNoSpawnWithoutDaemon(t *testing.T) {
	_, cleanup := tempHome(t)
	defer cleanup()

	stdin := mustMarshal(t, protocol.Request{Op: protocol.OpPing})
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runExec([]string{"--stdin", "--no-spawn"}, Env{
		BuildVersion: "test",
		Stdin:        bytes.NewReader(stdin),
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
	if resp.OK || resp.Code != protocol.CodeDaemonUnavailable {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

// tempHome redirects launcher.DefaultPaths to a throwaway directory by overriding HOME.
func tempHome(t *testing.T) (string, func()) {
	t.Helper()
	dir, err := ioutil.TempDir("", "sofarpc-cli-test-")
	if err != nil {
		t.Fatalf("tempdir: %v", err)
	}
	prevHome := os.Getenv("HOME")
	if err := os.Setenv("HOME", dir); err != nil {
		t.Fatalf("setenv HOME: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".sofarpc", "daemon"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Rewrite the caller-visible HOME subdir so writeState can find it with the same root.
	return filepath.Join(dir, ".sofarpc"), func() {
		_ = os.Setenv("HOME", prevHome)
		_ = os.RemoveAll(dir)
	}
}

func writeState(t *testing.T, path string, port int) {
	t.Helper()
	state := launcher.State{
		PID:          os.Getpid(),
		Port:         port,
		BuildVersion: "test",
		StartedAtMS:  time.Now().UnixMilli(),
		Status:       "RUNNING",
	}
	body, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := ioutil.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
}

// startFakeDaemon listens on a random loopback port and replies to each connection with a
// success envelope mirroring the request id. It mimics the daemon wire format closely enough
// for the client-side test to exercise launcher, ipc, and exec together.
func startFakeDaemon(t *testing.T) (net.Listener, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port, err := strconv.Atoi(strings.Split(ln.Addr().String(), ":")[1])
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleFakeConn(t, conn)
		}
	}()
	return ln, port
}

func handleFakeConn(t *testing.T, conn net.Conn) {
	defer conn.Close()
	for {
		body, err := ipc.ReadFrame(conn)
		if err != nil {
			return
		}
		var req protocol.Request
		if err := json.Unmarshal(body, &req); err != nil {
			return
		}
		resp := &protocol.Response{
			RequestID: req.RequestID,
			OK:        true,
			Code:      protocol.CodeSuccess,
		}
		switch req.Op {
		case protocol.OpHealth:
			data, _ := json.Marshal(protocol.HealthData{
				PID:          int64(os.Getpid()),
				BuildVersion: "test",
				StartedAtMS:  time.Now().UnixMilli(),
			})
			resp.Data = data
		default:
			resp.Data = json.RawMessage(`{}`)
		}
		out, _ := json.Marshal(resp)
		if err := ipc.WriteFrame(conn, out); err != nil {
			return
		}
	}
}

func mustMarshal(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
