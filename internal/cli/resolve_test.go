package cli

import (
	"bytes"
	"encoding/json"
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

func TestProjectListSupportsTableAndJSON(t *testing.T) {
	_, cleanup := tempHome(t)
	defer cleanup()

	path, err := appconfig.DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	cfg := appconfig.Config{
		Version: appconfig.CurrentConfigVersion,
		Projects: map[string]appconfig.Project{
			"user": {
				WorkspaceRoot:   filepath.Dir(path),
				ServicePrefixes: []string{"com.example."},
				ActiveProfile:   "test",
				Profiles: map[string]appconfig.Profile{
					"local": {Address: "127.0.0.1:12200"},
					"test":  {Address: "192.0.2.10:12200"},
				},
			},
		},
	}
	if err := appconfig.Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	tableOut := &bytes.Buffer{}
	tableEnv := Env{Stdout: tableOut, Stderr: &bytes.Buffer{}}
	if code := runProject([]string{"list"}, tableEnv); code != 0 {
		t.Fatalf("table list exit=%d stderr=%s", code, tableEnv.Stderr.(*bytes.Buffer).String())
	}
	table := tableOut.String()
	if strings.HasPrefix(strings.TrimSpace(table), "{") {
		t.Fatalf("project list should default to table output, got %s", table)
	}
	for _, want := range []string{"PROJECT", "WORKSPACE", "ACTIVE_PROFILE", "PROFILES", "PREFIXES", "user", "test", "local,test", "com.example."} {
		if !strings.Contains(table, want) {
			t.Fatalf("project list table missing %q: %s", want, table)
		}
	}

	jsonOut := &bytes.Buffer{}
	jsonEnv := Env{Stdout: jsonOut, Stderr: &bytes.Buffer{}}
	if code := runProject([]string{"list", "--json"}, jsonEnv); code != 0 {
		t.Fatalf("json list exit=%d stderr=%s", code, jsonEnv.Stderr.(*bytes.Buffer).String())
	}
	var out struct {
		OK       bool `json:"ok"`
		Projects []struct {
			Name    string            `json:"name"`
			Project appconfig.Project `json:"project"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(jsonOut.Bytes()), &out); err != nil {
		t.Fatalf("decode project list json: %v", err)
	}
	if !out.OK || len(out.Projects) != 1 || out.Projects[0].Name != "user" || out.Projects[0].Project.ActiveProfile != "test" {
		t.Fatalf("unexpected project list json: %+v", out)
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
		Projects: map[string]appconfig.Project{
			"user": {WorkspaceRoot: filepath.Dir(path)},
		},
		Servers: map[string]appconfig.Server{
			"user-test": {Address: "192.0.2.10:12200", Project: "user"},
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
