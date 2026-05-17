//go:build e2e

// Package e2e drives a real Java daemon from the Go client. Gated behind the `e2e` build tag
// so `go test ./...` on contributor machines stays offline and fast; CI (or a developer with
// Java) runs `go test -tags e2e ./internal/e2e/...` after `mvn package` produced the jar.
package e2e

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sofarpc/cli/internal/launcher"
	"github.com/sofarpc/cli/internal/protocol"
)

// TestE2EHealth boots the real daemon via launcher.Connect, calls health, and then shuts it
// down. It is the smallest possible "client-can-talk-to-daemon" round-trip and catches whole
// categories of regressions (framing drift, jar path, state.json format, health encoding).
func TestE2EHealth(t *testing.T) {
	jar := resolveJar(t)

	home := tempHome(t)
	defer os.RemoveAll(home)

	cfg := newConfig(t, jar)
	cfg.SpawnBudget = 45 * time.Second

	conn, err := launcher.Connect(cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer shutdown(t, conn.Client)

	if conn.Health.PID <= 0 {
		t.Fatalf("health pid not set: %+v", conn.Health)
	}
	if conn.State.Port == 0 {
		t.Fatalf("state port not set: %+v", conn.State)
	}

	// Second Connect on a warm daemon must hit the fast path and return the same pid.
	second, err := launcher.Connect(cfg)
	if err != nil {
		t.Fatalf("warm connect: %v", err)
	}
	if second.State.PID != conn.State.PID {
		t.Fatalf("warm reuse produced different pid: %d vs %d", second.State.PID, conn.State.PID)
	}
}

// TestE2EPingToNowhere asserts the CONNECT_FAILED error classification flows end-to-end.
// We ask the daemon to ping a port nobody is listening on and expect ok=false with the
// expected code — no exceptions leaking into wire format, no timeouts from the client.
func TestE2EPingToNowhere(t *testing.T) {
	jar := resolveJar(t)
	home := tempHome(t)
	defer os.RemoveAll(home)

	cfg := newConfig(t, jar)
	cfg.SpawnBudget = 45 * time.Second

	conn, err := launcher.Connect(cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer shutdown(t, conn.Client)

	req, err := protocol.NewRequest(protocol.OpPing, protocol.PingPayload{
		Address:      "127.0.0.1:1",
		RPCTimeoutMS: 300,
	})
	if err != nil {
		t.Fatalf("build req: %v", err)
	}
	resp, err := conn.Client.Call(req)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if resp.OK {
		t.Fatalf("expected ok=false for unreachable port, got %+v", resp)
	}
	if resp.Code != protocol.CodeConnectFailed {
		t.Fatalf("expected CONNECT_FAILED, got %s (resp=%+v)", resp.Code, resp)
	}
}

func resolveJar(t *testing.T) string {
	t.Helper()
	if env := os.Getenv("SOFARPCD_JAR"); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env
		}
		t.Fatalf("SOFARPCD_JAR=%s but file missing", env)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	candidates := []string{
		filepath.Join(cwd, "..", "..", "..", "daemon", "target", "sofarpc-engine.jar"),
		filepath.Join(cwd, "..", "..", "daemon", "target", "sofarpc-engine.jar"),
		filepath.Join(cwd, "..", "..", "..", "daemon", "target", "sofarpcd.jar"),
		filepath.Join(cwd, "..", "..", "daemon", "target", "sofarpcd.jar"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	t.Skipf("sofarpc-engine.jar not found; run `mvn -DskipTests package` in daemon (tried %v)", candidates)
	return ""
}

func tempHome(t *testing.T) string {
	t.Helper()
	dir, err := ioutil.TempDir("", "sofarpc-e2e-")
	if err != nil {
		t.Fatalf("tempdir: %v", err)
	}
	if err := os.Setenv("HOME", dir); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	return dir
}

func newConfig(t *testing.T, jar string) launcher.Config {
	t.Helper()
	cfg, err := launcher.DefaultConfig("e2e")
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	cfg.JarPath = jar
	cfg.IdleTTLMS = 0
	return cfg
}

func shutdown(t *testing.T, client interface {
	Call(protocol.Request) (*protocol.Response, error)
}) {
	t.Helper()
	req, err := protocol.NewRequest(protocol.OpShutdown, protocol.ShutdownPayload{GraceMS: 0})
	if err != nil {
		t.Logf("build shutdown: %v", err)
		return
	}
	resp, err := client.Call(req)
	if err != nil {
		t.Logf("shutdown call: %v", err)
		return
	}
	if !resp.OK {
		t.Logf("shutdown response not ok: %s", resp.Code)
	}
	var body map[string]interface{}
	_ = json.Unmarshal(resp.Data, &body)
}
