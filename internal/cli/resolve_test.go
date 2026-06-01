package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

func TestServerAddListRemove(t *testing.T) {
	base, cleanup := tempHome(t)
	defer cleanup()

	env := Env{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	if code := runProject([]string{"add", "user", filepath.Dir(base)}, env); code != 0 {
		t.Fatalf("project add exit=%d stderr=%s", code, env.Stderr.(*bytes.Buffer).String())
	}
	if code := runServer([]string{"add", "user-test", "192.0.2.10:12200", "--project", "user"}, env); code != 0 {
		t.Fatalf("add exit=%d stderr=%s", code, env.Stderr.(*bytes.Buffer).String())
	}

	listOut := &bytes.Buffer{}
	listEnv := Env{Stdout: listOut, Stderr: &bytes.Buffer{}}
	if code := runServer([]string{"list", "--json"}, listEnv); code != 0 {
		t.Fatalf("list exit=%d", code)
	}
	if !strings.Contains(listOut.String(), `"user-test"`) {
		t.Fatalf("list missing server: %s", listOut.String())
	}

	rmEnv := Env{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	if code := runServer([]string{"remove", "user-test", "--confirm"}, rmEnv); code != 0 {
		t.Fatalf("remove exit=%d stderr=%s", code, rmEnv.Stderr.(*bytes.Buffer).String())
	}

	rmMissingEnv := Env{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	if code := runServer([]string{"remove", "user-test", "--confirm"}, rmMissingEnv); code == 0 {
		t.Fatal("expected non-zero exit when removing missing server")
	}
}

// TestResolveAddress pins the single config-backed resolution path: a raw
// host:port passes through, a configured server name resolves to its address, and
// an unknown name reports the known ones.
func TestResolveAddress(t *testing.T) {
	_, cleanup := tempHome(t)
	defer cleanup()

	path, err := appconfig.DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	// 192.0.2.0/24 is RFC 5737 TEST-NET-1, reserved for documentation. It is never
	// dialed here — resolveAddress only echoes back whatever address is configured.
	cfg := appconfig.Config{
		Version: appconfig.CurrentConfigVersion,
		Servers: map[string]appconfig.Server{
			"user-test": {Address: "192.0.2.10:12200"},
		},
	}
	if err := appconfig.Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	if got, err := resolveAddress("1.2.3.4:8080"); err != nil || got != "1.2.3.4:8080" {
		t.Fatalf("raw host:port must pass through, got %q err=%v", got, err)
	}
	if got, err := resolveAddress("user-test"); err != nil || got != "192.0.2.10:12200" {
		t.Fatalf("server name must resolve to its address, got %q err=%v", got, err)
	}
	_, err = resolveAddress("nope")
	if err == nil || !strings.Contains(err.Error(), "user-test") {
		t.Fatalf("unknown server must list known servers, got %v", err)
	}
}
