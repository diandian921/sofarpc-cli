package tools

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

// registerConfigTools registers all five config tools on one server so the flow
// tests can save, list, and remove through the same session.
func registerConfigTools(writeEnabled bool) func(srv *mcpsdk.Server) {
	return func(srv *mcpsdk.Server) {
		AddConfigList(srv, writeEnabled, io.Discard)
		AddConfigSaveProject(srv, io.Discard)
		AddConfigSaveServer(srv, io.Discard)
		AddConfigRemoveProject(srv, io.Discard)
		AddConfigRemoveServer(srv, io.Discard)
	}
}

// TestFailureResultPreservesConfigErrorCode pins the ConfigError passthrough
// at the unit level: a *appconfig.ConfigError keeps its stable code and surfaces the
// config path in details, while a plain error falls back to BAD_REQUEST.
func TestFailureResultPreservesConfigErrorCode(t *testing.T) {
	cfgErr := &appconfig.ConfigError{Code: appconfig.CodeConfigInvalid, Path: "/home/x/config.json", Err: errors.New("bad json")}
	r := failureResult(cfgErr, app.CodeInternalError)
	if r.OK || r.Code != appconfig.CodeConfigInvalid {
		t.Errorf("ConfigError code not preserved: %+v", r)
	}
	if r.Error == nil || r.Error.Details["configPath"] != "/home/x/config.json" {
		t.Errorf("ConfigError path not surfaced in details: %+v", r.Error)
	}

	plain := failureResult(errors.New("boom"), app.CodeBadRequest)
	if plain.OK || plain.Code != app.CodeBadRequest {
		t.Errorf("plain error should map to BAD_REQUEST: %+v", plain)
	}
}

// TestConfigListFiltersByProject pins the project filter: only the named project
// and its bound servers are listed, and configPath / writeEnabled / projectFilter
// are echoed for the agent.
func TestConfigListFiltersByProject(t *testing.T) {
	seedConfig(t, func(cfg *appconfig.Config) error {
		if _, err := cfg.AddProject("alpha", t.TempDir(), nil, false); err != nil {
			return err
		}
		if _, err := cfg.AddProject("beta", t.TempDir(), nil, false); err != nil {
			return err
		}
		if _, err := cfg.AddServer("alpha-test", appconfig.Server{Address: "127.0.0.1:12200", Project: "alpha"}, false); err != nil {
			return err
		}
		_, err := cfg.AddServer("beta-test", appconfig.Server{Address: "127.0.0.1:12300", Project: "beta"}, false)
		return err
	})
	cs := newToolClient(t, registerConfigTools(true))
	env := decodeEnvelope(t, callTool(t, cs, "sofarpc_config_list", map[string]any{"project": "alpha"}))
	if !env.OK {
		t.Fatalf("config_list failed: %+v", env)
	}
	projects, _ := env.Data["projects"].([]any)
	servers, _ := env.Data["servers"].([]any)
	if len(projects) != 1 || len(servers) != 1 {
		t.Errorf("filter should keep 1 project and 1 server, got %d/%d", len(projects), len(servers))
	}
	if env.Data["projectFilter"] != "alpha" {
		t.Errorf("projectFilter should echo the filter, got %v", env.Data["projectFilter"])
	}
	if env.Data["writeEnabled"] != true {
		t.Errorf("writeEnabled should reflect the registration flag, got %v", env.Data["writeEnabled"])
	}
	if path, _ := env.Data["configPath"].(string); path == "" {
		t.Error("configPath should be reported")
	}
}

// TestConfigListSurfacesConfigErrorCode pins the ConfigError code passthrough over
// the wire: a corrupt config.json yields the stable CONFIG_INVALID code (not a
// generic BAD_REQUEST) plus the offending path in error.details.configPath.
func TestConfigListSurfacesConfigErrorCode(t *testing.T) {
	t.Setenv("SOFARPC_HOME", t.TempDir())
	path, err := appconfig.DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt config: %v", err)
	}
	cs := newToolClient(t, registerConfigTools(true))
	res := callTool(t, cs, "sofarpc_config_list", nil)
	env := decodeEnvelope(t, res)
	if !res.IsError || env.Code != appconfig.CodeConfigInvalid {
		t.Fatalf("corrupt config must surface CONFIG_INVALID, got %+v", env)
	}
	if env.Error == nil || env.Error.Details["configPath"] != path {
		t.Errorf("error.details.configPath should name the corrupt file: %+v", env.Error)
	}
}

// TestConfigSaveProjectValidationPinnedRecovery pins the friendly BAD_REQUEST for a
// missing required field: the recovery text tells the agent what to provide and to
// preview with dryRun, instead of a bare protocol error.
func TestConfigSaveProjectValidationPinnedRecovery(t *testing.T) {
	seedConfig(t, nil)
	cs := newToolClient(t, registerConfigTools(true))
	res := callTool(t, cs, "sofarpc_config_save_project", map[string]any{"name": "p1"})
	env := decodeEnvelope(t, res)
	if !res.IsError || env.Code != app.CodeBadRequest {
		t.Fatalf("missing workspaceRoot must be BAD_REQUEST, got %+v", env)
	}
	if env.Error == nil || !strings.Contains(env.Error.Recovery, "dryRun=true") {
		t.Errorf("recovery should point at the dryRun preview, got %+v", env.Error)
	}
}

// TestConfigSaveProjectDryRunLifecycle pins the dryRun contract end to end: a dry
// run previews without persisting, a real save persists, and a later dry run of a
// duplicate without overwrite fails validation instead of previewing success.
func TestConfigSaveProjectDryRunLifecycle(t *testing.T) {
	seedConfig(t, nil)
	cs := newToolClient(t, registerConfigTools(true))
	ws := t.TempDir()

	dry := decodeEnvelope(t, callTool(t, cs, "sofarpc_config_save_project",
		map[string]any{"name": "p1", "workspaceRoot": ws, "dryRun": true}))
	if !dry.OK || dry.Data["dryRun"] != true {
		t.Fatalf("dry run should succeed with dryRun:true, got %+v", dry)
	}
	list := decodeEnvelope(t, callTool(t, cs, "sofarpc_config_list", nil))
	if projects, _ := list.Data["projects"].([]any); len(projects) != 0 {
		t.Fatalf("dry run must not persist the project: %+v", list.Data)
	}

	real := decodeEnvelope(t, callTool(t, cs, "sofarpc_config_save_project",
		map[string]any{"name": "p1", "workspaceRoot": ws}))
	if !real.OK {
		t.Fatalf("real save failed: %+v", real)
	}

	dup := callTool(t, cs, "sofarpc_config_save_project",
		map[string]any{"name": "p1", "workspaceRoot": ws, "dryRun": true})
	dupEnv := decodeEnvelope(t, dup)
	if !dup.IsError || dupEnv.Code != app.CodeBadRequest {
		t.Errorf("dry run of a duplicate without overwrite must fail validation, got %+v", dupEnv)
	}

	over := decodeEnvelope(t, callTool(t, cs, "sofarpc_config_save_project",
		map[string]any{"name": "p1", "workspaceRoot": ws, "dryRun": true, "overwrite": true}))
	if !over.OK {
		t.Errorf("dry run with overwrite should pass validation, got %+v", over)
	}
}

// TestConfigSaveServerAppliesDefaults pins the default filling: protocol, timeout
// and appName fall back to the appconfig defaults when omitted, and the saved
// server echoes them.
func TestConfigSaveServerAppliesDefaults(t *testing.T) {
	seedConfig(t, func(cfg *appconfig.Config) error {
		_, err := cfg.AddProject("user", t.TempDir(), nil, false)
		return err
	})
	cs := newToolClient(t, registerConfigTools(true))
	env := decodeEnvelope(t, callTool(t, cs, "sofarpc_config_save_server",
		map[string]any{"name": "standalone", "address": "127.0.0.1:12200", "project": "user"}))
	if !env.OK {
		t.Fatalf("save server failed: %+v", env)
	}
	server, _ := env.Data["server"].(map[string]any)
	if server == nil {
		t.Fatalf("data.server missing: %+v", env.Data)
	}
	if server["protocol"] != appconfig.DefaultServerProtocol {
		t.Errorf("protocol default not applied: %v", server["protocol"])
	}
	if timeout, _ := server["timeoutMs"].(float64); int(timeout) != appconfig.DefaultServerTimeoutMS {
		t.Errorf("timeoutMs default not applied: %v", server["timeoutMs"])
	}
	if server["appName"] != appconfig.DefaultServerAppName {
		t.Errorf("appName default not applied: %v", server["appName"])
	}
}

// TestConfigSaveServerValidationPaths pins the two validation exits: missing
// required fields return BAD_REQUEST with the pinned sofarpc_config_list recovery,
// and a malformed address is rejected by the shared appconfig validation.
func TestConfigSaveServerValidationPaths(t *testing.T) {
	seedConfig(t, func(cfg *appconfig.Config) error {
		_, err := cfg.AddProject("user", t.TempDir(), nil, false)
		return err
	})
	cs := newToolClient(t, registerConfigTools(true))

	missing := callTool(t, cs, "sofarpc_config_save_server", map[string]any{"name": "s1"})
	missingEnv := decodeEnvelope(t, missing)
	if !missing.IsError || missingEnv.Code != app.CodeBadRequest {
		t.Fatalf("missing fields must be BAD_REQUEST, got %+v", missingEnv)
	}
	if missingEnv.Error == nil || missingEnv.Error.NextTool != "sofarpc_config_list" {
		t.Errorf("missing-field recovery must point at sofarpc_config_list, got %+v", missingEnv.Error)
	}

	bad := callTool(t, cs, "sofarpc_config_save_server",
		map[string]any{"name": "s1", "address": "not-a-hostport", "project": "user", "dryRun": true})
	badEnv := decodeEnvelope(t, bad)
	if !bad.IsError || badEnv.Code != app.CodeBadRequest {
		t.Fatalf("invalid address must fail validation, got %+v", badEnv)
	}
	if badEnv.Error == nil || !strings.Contains(badEnv.Error.Message, "host:port") {
		t.Errorf("error should explain the expected address shape, got %+v", badEnv.Error)
	}
}

// TestConfigSaveServerDryRunDoesNotWrite pins write safety for the server tool: a
// dryRun save validates and echoes the redacted entry but leaves config.json alone.
func TestConfigSaveServerDryRunDoesNotWrite(t *testing.T) {
	seedConfig(t, func(cfg *appconfig.Config) error {
		_, err := cfg.AddProject("user", t.TempDir(), nil, false)
		return err
	})
	cs := newToolClient(t, registerConfigTools(true))
	dry := decodeEnvelope(t, callTool(t, cs, "sofarpc_config_save_server", map[string]any{
		"name": "standalone", "address": "127.0.0.1:12200", "project": "user",
		"attachments": map[string]any{"token": "SECRET-VALUE"}, "dryRun": true,
	}))
	if !dry.OK || dry.Data["dryRun"] != true {
		t.Fatalf("dry run should succeed with dryRun:true, got %+v", dry)
	}
	server, _ := dry.Data["server"].(map[string]any)
	if atts, _ := server["attachments"].(map[string]any); atts["token"] != redactedValue {
		t.Errorf("dry run echo must redact attachment values, got %v", server["attachments"])
	}
	list := decodeEnvelope(t, callTool(t, cs, "sofarpc_config_list", nil))
	if servers, _ := list.Data["servers"].([]any); len(servers) != 0 {
		t.Errorf("dry run must not persist the server: %+v", list.Data)
	}
}

// TestConfigRemoveProjectFlow pins the remove-project contract: name is required
// (with the config_list recovery), confirm=true is enforced, bound servers block
// removal without cascade, and cascade removes both project and servers.
func TestConfigRemoveProjectFlow(t *testing.T) {
	seedConfig(t, func(cfg *appconfig.Config) error {
		if _, err := cfg.AddProject("user", t.TempDir(), nil, false); err != nil {
			return err
		}
		_, err := cfg.AddServer("user-test", appconfig.Server{Address: "127.0.0.1:12200", Project: "user"}, false)
		return err
	})
	cs := newToolClient(t, registerConfigTools(true))

	missing := callTool(t, cs, "sofarpc_config_remove_project", map[string]any{})
	missingEnv := decodeEnvelope(t, missing)
	if !missing.IsError || missingEnv.Error == nil || missingEnv.Error.NextTool != "sofarpc_config_list" {
		t.Errorf("missing name must fail with the config_list recovery, got %+v", missingEnv)
	}

	unconfirmed := callTool(t, cs, "sofarpc_config_remove_project", map[string]any{"name": "user"})
	if !unconfirmed.IsError {
		t.Error("remove without confirm=true must fail")
	}

	blocked := callTool(t, cs, "sofarpc_config_remove_project", map[string]any{"name": "user", "confirm": true})
	blockedEnv := decodeEnvelope(t, blocked)
	if !blocked.IsError || blockedEnv.Error == nil || !strings.Contains(blockedEnv.Error.Message, "user-test") {
		t.Errorf("removal with bound servers must name them, got %+v", blockedEnv)
	}

	removed := decodeEnvelope(t, callTool(t, cs, "sofarpc_config_remove_project",
		map[string]any{"name": "user", "confirm": true, "cascade": true}))
	if !removed.OK || removed.Data["removed"] != "user" {
		t.Fatalf("cascade removal failed: %+v", removed)
	}
	list := decodeEnvelope(t, callTool(t, cs, "sofarpc_config_list", nil))
	projects, _ := list.Data["projects"].([]any)
	servers, _ := list.Data["servers"].([]any)
	if len(projects) != 0 || len(servers) != 0 {
		t.Errorf("cascade must remove the project and its servers: %+v", list.Data)
	}
}

// TestConfigRemoveServerFlow pins the remove-server contract: name required with
// the config_list recovery, confirm enforced, unknown name rejected, and a
// confirmed removal disappears from a later list.
func TestConfigRemoveServerFlow(t *testing.T) {
	seedConfig(t, func(cfg *appconfig.Config) error {
		if _, err := cfg.AddProject("user", t.TempDir(), nil, false); err != nil {
			return err
		}
		_, err := cfg.AddServer("user-test", appconfig.Server{Address: "127.0.0.1:12200", Project: "user"}, false)
		return err
	})
	cs := newToolClient(t, registerConfigTools(true))

	missing := callTool(t, cs, "sofarpc_config_remove_server", map[string]any{})
	missingEnv := decodeEnvelope(t, missing)
	if !missing.IsError || missingEnv.Error == nil || missingEnv.Error.NextTool != "sofarpc_config_list" {
		t.Errorf("missing name must fail with the config_list recovery, got %+v", missingEnv)
	}
	if unconfirmed := callTool(t, cs, "sofarpc_config_remove_server", map[string]any{"name": "user-test"}); !unconfirmed.IsError {
		t.Error("remove without confirm=true must fail")
	}
	if unknown := callTool(t, cs, "sofarpc_config_remove_server", map[string]any{"name": "ghost", "confirm": true}); !unknown.IsError {
		t.Error("removing an unknown server must fail")
	}

	removed := decodeEnvelope(t, callTool(t, cs, "sofarpc_config_remove_server",
		map[string]any{"name": "user-test", "confirm": true}))
	if !removed.OK || removed.Data["removed"] != "user-test" {
		t.Fatalf("confirmed removal failed: %+v", removed)
	}
	list := decodeEnvelope(t, callTool(t, cs, "sofarpc_config_list", nil))
	if servers, _ := list.Data["servers"].([]any); len(servers) != 0 {
		t.Errorf("removed server still listed: %+v", list.Data)
	}
}
