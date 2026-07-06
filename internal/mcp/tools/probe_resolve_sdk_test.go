package tools

import (
	"io"
	"net"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

func registerProbe(srv *mcpsdk.Server) {
	AddProbe(srv, app.New(nil), io.Discard)
}

func registerResolve(srv *mcpsdk.Server) {
	AddResolve(srv, app.New(nil), io.Discard)
}

// TestProbeReachableAddress pins the success shape: probing a live listener is OK
// with reachable=true, a ping-prefixed requestId, and the fixed "TCP only" caveat
// as the summary.
func TestProbeReachableAddress(t *testing.T) {
	seedConfig(t, nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	cs := newToolClient(t, registerProbe)
	res := callTool(t, cs, "sofarpc_probe", map[string]any{"address": ln.Addr().String(), "timeoutMs": 2000})
	env := decodeEnvelope(t, res)
	if res.IsError || !env.OK || env.Code != app.CodeSuccess {
		t.Fatalf("probe of a live listener should succeed, got %+v", env)
	}
	if env.Data["reachable"] != true || env.Data["address"] != ln.Addr().String() {
		t.Errorf("data should report the probed address as reachable: %+v", env.Data)
	}
	if !strings.HasPrefix(env.RequestID, "ping-") {
		t.Errorf("requestId should be ping-prefixed, got %q", env.RequestID)
	}
	if res.Meta["summary"] != probeSummary {
		t.Errorf("summary must carry the TCP-only caveat, got %v", res.Meta["summary"])
	}
}

// TestProbeUnreachableAddress pins the failure envelope: a dead port is isError
// with CONNECT_FAILED, and recovery routes to sofarpc_doctor — a failed probe must
// never point back at sofarpc_probe itself.
func TestProbeUnreachableAddress(t *testing.T) {
	seedConfig(t, nil)
	cs := newToolClient(t, registerProbe)
	res := callTool(t, cs, "sofarpc_probe", map[string]any{"address": "127.0.0.1:1", "timeoutMs": 200})
	env := decodeEnvelope(t, res)
	if !res.IsError || env.Code != app.CodeConnectFailed {
		t.Fatalf("dead port must be CONNECT_FAILED, got %+v", env)
	}
	if env.Error == nil || env.Error.NextTool != "sofarpc_doctor" {
		t.Errorf("failed probe must route recovery to sofarpc_doctor, got %+v", env.Error)
	}
}

// TestProbeConfiguredServerLabels pins config-driven probing: with server=...,
// the probe result is labeled with the configured server/project and uses the
// configured address.
func TestProbeConfiguredServerLabels(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	seedConfig(t, func(cfg *appconfig.Config) error {
		if _, err := cfg.AddProject("user", t.TempDir(), nil, false); err != nil {
			return err
		}
		_, err := cfg.AddServer("user-test", appconfig.Server{Address: ln.Addr().String(), Project: "user"}, false)
		return err
	})
	cs := newToolClient(t, registerProbe)
	env := decodeEnvelope(t, callTool(t, cs, "sofarpc_probe", map[string]any{"server": "user-test"}))
	if !env.OK {
		t.Fatalf("configured-server probe failed: %+v", env)
	}
	if env.Data["server"] != "user-test" || env.Data["project"] != "user" || env.Data["address"] != ln.Addr().String() {
		t.Errorf("probe should be labeled with the configured server/project/address: %+v", env.Data)
	}
}

// TestResolveEndpointBranch pins the single-endpoint success shape: project,
// server, and a redacted endpoint — and none of the retired network/diagnostics
// fields, which were removed from the tool payload to save tokens.
func TestResolveEndpointBranch(t *testing.T) {
	seedConfig(t, func(cfg *appconfig.Config) error {
		if _, err := cfg.AddProject("user", t.TempDir(), nil, false); err != nil {
			return err
		}
		_, err := cfg.AddServer("user-test", appconfig.Server{
			Address:     "127.0.0.1:12200",
			Project:     "user",
			Attachments: map[string]string{"token": "SECRET-VALUE"},
		}, false)
		return err
	})
	cs := newToolClient(t, registerResolve)
	env := decodeEnvelope(t, callTool(t, cs, "sofarpc_resolve", map[string]any{"server": "user-test"}))
	if !env.OK {
		t.Fatalf("resolve failed: %+v", env)
	}
	if env.Data["project"] != "user" || env.Data["server"] != "user-test" {
		t.Errorf("project/server missing: %+v", env.Data)
	}
	endpoint, _ := env.Data["endpoint"].(map[string]any)
	if endpoint == nil || endpoint["address"] != "127.0.0.1:12200" {
		t.Fatalf("endpoint missing or wrong: %+v", env.Data)
	}
	if atts, _ := endpoint["attachments"].(map[string]any); atts["token"] != redactedValue {
		t.Errorf("endpoint attachments must be redacted: %v", endpoint["attachments"])
	}
	for _, retired := range []string{"network", "diagnostics"} {
		if _, has := env.Data[retired]; has {
			t.Errorf("success output must not carry the retired %q field", retired)
		}
	}
}

// TestResolveMultiServerBranch pins the no-single-endpoint branch: with two bound
// servers and no hints at all, resolve succeeds with a redacted servers list and
// no endpoint. (Passing project= would resolve via the project's activeProfile,
// which profile-style seeding sets automatically — so this test passes nothing.)
func TestResolveMultiServerBranch(t *testing.T) {
	seedConfig(t, func(cfg *appconfig.Config) error {
		if _, err := cfg.AddProject("user", t.TempDir(), nil, false); err != nil {
			return err
		}
		if _, err := cfg.AddServer("user-a", appconfig.Server{Address: "127.0.0.1:12200", Project: "user", Attachments: map[string]string{"token": "SECRET-VALUE"}}, false); err != nil {
			return err
		}
		_, err := cfg.AddServer("user-b", appconfig.Server{Address: "127.0.0.1:12300", Project: "user"}, false)
		return err
	})
	cs := newToolClient(t, registerResolve)
	env := decodeEnvelope(t, callTool(t, cs, "sofarpc_resolve", nil))
	if !env.OK {
		t.Fatalf("resolve failed: %+v", env)
	}
	if _, has := env.Data["endpoint"]; has {
		t.Error("multi-server resolve must not pick an endpoint")
	}
	servers, _ := env.Data["servers"].([]any)
	if len(servers) != 2 {
		t.Fatalf("expected 2 bound servers, got %+v", env.Data)
	}
	for _, entry := range servers {
		m, _ := entry.(map[string]any)
		server, _ := m["server"].(map[string]any)
		if atts, ok := server["attachments"].(map[string]any); ok {
			for k, v := range atts {
				if v != redactedValue {
					t.Errorf("servers list leaked attachment %q=%v", k, v)
				}
			}
		}
	}
}

// TestResolveUnknownServer pins the failure envelope: an unknown server is
// BAD_REQUEST with the SERVER_NOT_FOUND kind and the config_list recovery.
func TestResolveUnknownServer(t *testing.T) {
	seedConfig(t, nil)
	cs := newToolClient(t, registerResolve)
	res := callTool(t, cs, "sofarpc_resolve", map[string]any{"server": "ghost"})
	env := decodeEnvelope(t, res)
	if !res.IsError || env.Code != app.CodeBadRequest {
		t.Fatalf("unknown server must be BAD_REQUEST, got %+v", env)
	}
	if env.Error == nil || env.Error.Details["kind"] != "SERVER_NOT_FOUND" {
		t.Errorf("error.details.kind should be SERVER_NOT_FOUND: %+v", env.Error)
	}
	if env.Error.NextTool != "sofarpc_config_list" {
		t.Errorf("recovery should route to sofarpc_config_list, got %+v", env.Error)
	}
}
