package tools

import (
	"io"
	"os"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

func registerConfigErrorRoutingTools(srv *mcpsdk.Server) {
	appSvc := app.New(nil)
	AddConfigList(srv, true, io.Discard)
	AddResolve(srv, appSvc, io.Discard)
	AddInvokePlan(srv, appSvc, io.Discard)
	AddDescribe(srv, appSvc, io.Discard)
}

func TestOrdinaryToolsShareConfigErrorRouting(t *testing.T) {
	configCases := []struct {
		name string
		body string
		code string
	}{
		{name: "corrupt", body: "{not json", code: appconfig.CodeConfigInvalid},
		{name: "future version", body: `{"version":999}`, code: appconfig.CodeConfigUnsupported},
	}
	toolCases := []struct {
		name string
		args map[string]any
	}{
		{name: "sofarpc_config_list"},
		{name: "sofarpc_resolve"},
		{name: "sofarpc_invoke_plan", args: map[string]any{
			"service": "com.example.Service", "method": "ping",
		}},
		{name: "sofarpc_describe", args: map[string]any{"query": "ping"}},
	}

	for _, configCase := range configCases {
		t.Run(configCase.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv(appconfig.EnvHome, home)
			path, err := appconfig.DefaultPath()
			if err != nil {
				t.Fatalf("default path: %v", err)
			}
			if err := os.WriteFile(path, []byte(configCase.body), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			cs := newToolClient(t, registerConfigErrorRoutingTools)
			for _, toolCase := range toolCases {
				t.Run(toolCase.name, func(t *testing.T) {
					res := callTool(t, cs, toolCase.name, toolCase.args)
					env := decodeEnvelope(t, res)
					if !res.IsError || env.Code != configCase.code {
						t.Fatalf("%s must surface %s, got %+v", toolCase.name, configCase.code, env)
					}
					if env.Error == nil || env.Error.Details["configPath"] != path {
						t.Errorf("%s configPath = %+v, want %q", toolCase.name, env.Error, path)
					}
					if env.Error.NextTool != "sofarpc_doctor" || env.Error.Recovery == "" {
						t.Errorf("%s recovery must route to doctor: %+v", toolCase.name, env.Error)
					}
				})
			}
		})
	}
}
