package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diandian921/sofarpc-mcp/internal/app"
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
	var envelope app.Result
	if err := json.Unmarshal(bytes.TrimSpace(jsonOut.Bytes()), &envelope); err != nil {
		t.Fatalf("decode project list envelope: %v", err)
	}
	if !envelope.OK || envelope.Code != "SUCCESS" || envelope.RequestID == "" {
		t.Fatalf("project list --json must emit the unified envelope: %+v", envelope)
	}
	var out struct {
		Projects []struct {
			Name    string            `json:"name"`
			Project appconfig.Project `json:"project"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(envelope.Data, &out); err != nil {
		t.Fatalf("decode project list data: %v", err)
	}
	if len(out.Projects) != 1 || out.Projects[0].Name != "user" || out.Projects[0].Project.ActiveProfile != "test" {
		t.Fatalf("unexpected project list data: %+v", out)
	}
}

// TestPingResolvesConfiguredServerName pins the single config-backed resolution
// path: ping shares app.ProbeEndpoint with the MCP probe tool, so a configured
// server name resolves to its address and an unknown name yields the unified
// envelope with a SERVER_NOT_FOUND kind.
func TestPingResolvesConfiguredServerName(t *testing.T) {
	_, cleanup := tempHome(t)
	defer cleanup()

	ln := startTCPListener(t)
	defer ln.Close()

	path, err := appconfig.DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	cfg := appconfig.Config{
		Version: appconfig.CurrentConfigVersion,
		Projects: map[string]appconfig.Project{
			"user": {WorkspaceRoot: filepath.Dir(path)},
		},
		Servers: map[string]appconfig.Server{
			"user-test": {Address: ln.Addr().String(), Project: "user"},
		},
	}
	if err := appconfig.Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	stdout := &bytes.Buffer{}
	if code := runPing([]string{"user-test"}, Env{Stdout: stdout, Stderr: &bytes.Buffer{}}); code != 0 {
		t.Fatalf("ping by server name exit=%d out=%s", code, stdout.String())
	}
	var okResult app.Result
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &okResult); err != nil || !okResult.OK {
		t.Fatalf("ping by server name must succeed via config resolution: %s", stdout.String())
	}

	stdout.Reset()
	if code := runPing([]string{"nope"}, Env{Stdout: stdout, Stderr: &bytes.Buffer{}}); code == 0 {
		t.Fatal("ping of an unknown server name must exit non-zero")
	}
	var failed app.Result
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &failed); err != nil {
		t.Fatalf("decode envelope: %v (%s)", err, stdout.String())
	}
	if failed.OK || failed.Error == nil {
		t.Fatalf("unknown server must emit a failure envelope: %s", stdout.String())
	}
	if !strings.Contains(failed.Error.Message, "nope") {
		t.Fatalf("failure message should name the unknown server: %+v", failed.Error)
	}
	if kind, _ := failed.Error.Details["kind"].(string); kind != "SERVER_NOT_FOUND" {
		t.Fatalf("failure should carry the SERVER_NOT_FOUND kind, got %+v", failed.Error.Details)
	}
}
