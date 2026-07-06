package tools

import (
	"io"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

// TestSaveServerSurfacesActiveProfile pins the fix for the silent-activeProfile
// trap: the first saved profile becomes active and says so; a second, inactive
// profile carries an explicit warning instead of silently not routing; and
// setActive=true switches the active profile in the same save.
func TestSaveServerSurfacesActiveProfile(t *testing.T) {
	seedConfig(t, func(cfg *appconfig.Config) error {
		_, err := cfg.AddProject("user", t.TempDir(), nil, false)
		return err
	})
	cs := newToolClient(t, func(srv *mcpsdk.Server) { AddConfigSaveServer(srv, io.Discard) })

	first := decodeEnvelope(t, callTool(t, cs, "sofarpc_config_save_server", map[string]any{
		"name": "user-a", "address": "127.0.0.1:1", "project": "user",
	}))
	if !first.OK {
		t.Fatalf("first save failed: %+v", first)
	}
	if first.Data["activeProfile"] != "a" {
		t.Fatalf("first save must report activeProfile=a, got %v", first.Data["activeProfile"])
	}
	if changed, _ := first.Data["activeProfileChanged"].(bool); !changed {
		t.Fatal("first save implicitly sets activeProfile and must say so")
	}
	if _, ok := first.Data["warning"]; ok {
		t.Fatalf("active save must not warn: %v", first.Data["warning"])
	}

	second := decodeEnvelope(t, callTool(t, cs, "sofarpc_config_save_server", map[string]any{
		"name": "user-b", "address": "127.0.0.1:2", "project": "user",
	}))
	if !second.OK {
		t.Fatalf("second save failed: %+v", second)
	}
	if second.Data["activeProfile"] != "a" {
		t.Fatalf("second save must keep activeProfile=a, got %v", second.Data["activeProfile"])
	}
	if _, ok := second.Data["activeProfileChanged"]; ok {
		t.Fatal("second save must not report a change")
	}
	warning, _ := second.Data["warning"].(string)
	if !strings.Contains(warning, `"b"`) || !strings.Contains(warning, "sofarpc project use user b") {
		t.Fatalf("inactive save must warn with the switch commands, got %q", warning)
	}

	switched := decodeEnvelope(t, callTool(t, cs, "sofarpc_config_save_server", map[string]any{
		"name": "user-b", "address": "127.0.0.1:2", "project": "user",
		"overwrite": true, "setActive": true,
	}))
	if !switched.OK {
		t.Fatalf("setActive save failed: %+v", switched)
	}
	if switched.Data["activeProfile"] != "b" {
		t.Fatalf("setActive must switch activeProfile to b, got %v", switched.Data["activeProfile"])
	}
	if changed, _ := switched.Data["activeProfileChanged"].(bool); !changed {
		t.Fatal("setActive switch must report the change")
	}
	if _, ok := switched.Data["warning"]; ok {
		t.Fatalf("setActive save must not warn: %v", switched.Data["warning"])
	}
}

// TestSaveServerDryRunPreviewsActiveProfileWithoutWriting pins that dryRun
// simulates the activeProfile outcome (including setActive) but leaves the
// stored config untouched.
func TestSaveServerDryRunPreviewsActiveProfileWithoutWriting(t *testing.T) {
	seedConfig(t, func(cfg *appconfig.Config) error {
		if _, err := cfg.AddProject("user", t.TempDir(), nil, false); err != nil {
			return err
		}
		_, err := cfg.AddServer("user-a", appconfig.Server{Address: "127.0.0.1:1", Project: "user"}, false)
		return err
	})
	cs := newToolClient(t, func(srv *mcpsdk.Server) { AddConfigSaveServer(srv, io.Discard) })

	dry := decodeEnvelope(t, callTool(t, cs, "sofarpc_config_save_server", map[string]any{
		"name": "user-b", "address": "127.0.0.1:2", "project": "user",
		"dryRun": true, "setActive": true,
	}))
	if !dry.OK {
		t.Fatalf("dry run failed: %+v", dry)
	}
	if dry.Data["activeProfile"] != "b" {
		t.Fatalf("dry run must preview the switched activeProfile, got %v", dry.Data["activeProfile"])
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if cfg.Projects["user"].ActiveProfile != "a" {
		t.Fatalf("dry run must not write: activeProfile = %q", cfg.Projects["user"].ActiveProfile)
	}
	if _, ok := cfg.Servers["user-b"]; ok {
		t.Fatal("dry run must not persist the server")
	}
}

// TestSaveServerSetActiveWithoutProfileFails pins the guard: setActive on a
// server whose name infers no profile is a BAD_REQUEST, not a silent no-op.
func TestSaveServerSetActiveWithoutProfileFails(t *testing.T) {
	seedConfig(t, func(cfg *appconfig.Config) error {
		_, err := cfg.AddProject("user", t.TempDir(), nil, false)
		return err
	})
	cs := newToolClient(t, func(srv *mcpsdk.Server) { AddConfigSaveServer(srv, io.Discard) })

	res := callTool(t, cs, "sofarpc_config_save_server", map[string]any{
		"name": "standalone", "address": "127.0.0.1:1", "project": "user",
		"setActive": true,
	})
	env := decodeEnvelope(t, res)
	if !res.IsError || env.OK || env.Error == nil {
		t.Fatalf("setActive without a profile must fail: %+v", env)
	}
	if !strings.Contains(env.Error.Message, "setActive requires a profile") {
		t.Fatalf("unexpected error message: %q", env.Error.Message)
	}
}
