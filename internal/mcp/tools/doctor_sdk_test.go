package tools

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

// writeUserFacade drops a minimal Java facade under the standard Maven layout so
// the source_schema / describe checks have something to index.
func writeUserFacade(t *testing.T, ws string) {
	t.Helper()
	src := filepath.Join(ws, "src", "main", "java", "com", "example", "user")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	java := "package com.example.user;\n\npublic interface UserFacade {\n    String getUser(String userId);\n}\n"
	if err := os.WriteFile(filepath.Join(src, "UserFacade.java"), []byte(java), 0o644); err != nil {
		t.Fatal(err)
	}
}

// seedProjectWithSource seeds one project (with Java source) plus one bound
// server, the standard doctor fixture.
func seedProjectWithSource(t *testing.T) {
	t.Helper()
	ws := t.TempDir()
	writeUserFacade(t, ws)
	seedConfig(t, func(cfg *appconfig.Config) error {
		if _, err := cfg.AddProject("user", ws, []string{"com.example."}, false); err != nil {
			return err
		}
		_, err := cfg.AddServer("user-test", appconfig.Server{Address: "127.0.0.1:12200", Project: "user"}, false)
		return err
	})
}

func registerDoctor(srv *mcpsdk.Server) {
	AddDoctor(srv, app.New(nil), true, io.Discard)
}

// checkStatuses flattens data.checks into name -> status for assertions.
func checkStatuses(t *testing.T, env resultEnvelope) map[string]string {
	t.Helper()
	checks, _ := env.Data["checks"].([]any)
	if len(checks) == 0 {
		t.Fatalf("data.checks missing: %+v", env.Data)
	}
	out := map[string]string{}
	for _, c := range checks {
		entry, _ := c.(map[string]any)
		name, _ := entry["name"].(string)
		status, _ := entry["status"].(string)
		out[name] = status
	}
	return out
}

// TestDoctorRecoveryToolSelection pins the failed-check -> recovery-tool mapping:
// server failures route to resolve, schema/describe failures to describe, and
// config/project (and anything unknown) to config_list.
func TestDoctorRecoveryToolSelection(t *testing.T) {
	cases := map[string]string{
		"server":        "sofarpc_resolve",
		"source_schema": "sofarpc_describe",
		"describe":      "sofarpc_describe",
		"config":        "sofarpc_config_list",
		"project":       "sofarpc_config_list",
		"anything-else": "sofarpc_config_list",
	}
	for check, want := range cases {
		if got := doctorRecoveryTool(check); got != want {
			t.Errorf("doctorRecoveryTool(%q) = %q, want %q", check, got, want)
		}
	}
}

// TestDoctorResultSDKShapes pins the two doctorResultSDK exits: all-ok checks are a
// SUCCESS result, while any failed check becomes an isError envelope that keeps
// data.checks, lists failedChecks, and picks nextTool from the first failure.
func TestDoctorResultSDKShapes(t *testing.T) {
	ok, summary := doctorResultSDK([]map[string]interface{}{
		{"name": "config", "status": "ok"},
	})
	if !ok.OK || ok.Code != app.CodeSuccess || summary != "Doctor completed." {
		t.Errorf("all-ok checks should be SUCCESS, got %+v / %q", ok, summary)
	}

	// server failing first → BAD_REQUEST (a user config/argument problem), with the
	// nextTool taken from that first failed check.
	failed, _ := doctorResultSDK([]map[string]interface{}{
		{"name": "config", "status": "ok"},
		{"name": "server", "status": "failed"},
		{"name": "source_schema", "status": "failed"},
	})
	if failed.OK || failed.Code != app.CodeBadRequest {
		t.Fatalf("server-first failure should be a BAD_REQUEST envelope, got %+v", failed)
	}
	if failed.Error == nil || failed.Error.NextTool != "sofarpc_resolve" {
		t.Errorf("nextTool must come from the first failed check, got %+v", failed.Error)
	}
	if len(failed.Data) == 0 {
		t.Error("failure must keep data.checks")
	}
	got, _ := failed.Error.Details["failedChecks"].([]string)
	if len(got) != 2 || got[0] != "server" || got[1] != "source_schema" {
		t.Errorf("failedChecks = %v", got)
	}

	// source_schema failing first → INTERNAL_ERROR (an environment problem).
	envFail, _ := doctorResultSDK([]map[string]interface{}{
		{"name": "source_schema", "status": "failed"},
	})
	if envFail.Code != app.CodeInternalError {
		t.Fatalf("source_schema-first failure should be INTERNAL_ERROR, got %+v", envFail)
	}
}

// TestDoctorAllChecksPass drives a fully healthy setup end to end: config, server,
// project, source_schema, and describe all report ok and the envelope is SUCCESS.
func TestDoctorAllChecksPass(t *testing.T) {
	seedProjectWithSource(t)
	cs := newToolClient(t, registerDoctor)
	res := callTool(t, cs, "sofarpc_doctor", map[string]any{
		"server": "user-test", "service": "com.example.user.UserFacade", "method": "getUser",
	})
	env := decodeEnvelope(t, res)
	if res.IsError || !env.OK || env.Code != app.CodeSuccess {
		t.Fatalf("healthy doctor run should succeed, got %+v", env)
	}
	statuses := checkStatuses(t, env)
	for _, name := range []string{"config", "server", "project", "source_schema", "describe"} {
		if statuses[name] != "ok" {
			t.Errorf("check %q = %q, want ok (all: %v)", name, statuses[name], statuses)
		}
	}
}

// TestDoctorUnknownServerFailsServerCheck pins the server-check failure path: the
// run is isError with the sofarpc_resolve recovery while the remaining checks
// still execute and data.checks survives.
func TestDoctorUnknownServerFailsServerCheck(t *testing.T) {
	seedProjectWithSource(t)
	cs := newToolClient(t, registerDoctor)
	res := callTool(t, cs, "sofarpc_doctor", map[string]any{"server": "ghost"})
	env := decodeEnvelope(t, res)
	if !res.IsError {
		t.Fatal("unknown server must make doctor isError")
	}
	statuses := checkStatuses(t, env)
	if statuses["server"] != "failed" {
		t.Errorf("server check should fail, got %v", statuses)
	}
	if env.Error == nil || env.Error.NextTool != "sofarpc_resolve" {
		t.Errorf("first failed check is server, so nextTool must be sofarpc_resolve: %+v", env.Error)
	}
}

// TestDoctorDescribeCheckFailure pins the describe-check failure: an unknown
// service FQN fails only the describe check and routes recovery to
// sofarpc_describe.
func TestDoctorDescribeCheckFailure(t *testing.T) {
	seedProjectWithSource(t)
	cs := newToolClient(t, registerDoctor)
	res := callTool(t, cs, "sofarpc_doctor", map[string]any{
		"server": "user-test", "service": "com.example.Missing",
	})
	env := decodeEnvelope(t, res)
	if !res.IsError {
		t.Fatal("unknown service must make doctor isError")
	}
	statuses := checkStatuses(t, env)
	if statuses["describe"] != "failed" || statuses["source_schema"] != "ok" {
		t.Errorf("only the describe check should fail, got %v", statuses)
	}
	if env.Error == nil || env.Error.NextTool != "sofarpc_describe" {
		t.Errorf("describe failure must route to sofarpc_describe: %+v", env.Error)
	}
}

// TestDoctorCorruptConfigStopsAtConfigCheck pins the earliest failure path: an
// unreadable config yields a single failed config check and the config_list
// recovery, with no later checks attempted.
func TestDoctorCorruptConfigStopsAtConfigCheck(t *testing.T) {
	t.Setenv("SOFARPC_HOME", t.TempDir())
	path, err := appconfig.DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt config: %v", err)
	}
	cs := newToolClient(t, registerDoctor)
	res := callTool(t, cs, "sofarpc_doctor", nil)
	env := decodeEnvelope(t, res)
	if !res.IsError {
		t.Fatal("corrupt config must make doctor isError")
	}
	checks, _ := env.Data["checks"].([]any)
	if len(checks) != 1 {
		t.Errorf("doctor should stop after the failed config check, got %d checks", len(checks))
	}
	if env.Error == nil || env.Error.NextTool != "sofarpc_config_list" {
		t.Errorf("config failure must route to sofarpc_config_list: %+v", env.Error)
	}
}
