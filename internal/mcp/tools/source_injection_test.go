package tools

import (
	"context"
	"io"
	"os"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
	"github.com/diandian921/sofarpc-mcp/internal/schema"
)

type injectedConfigStore struct {
	cfg appconfig.Config
}

func (s injectedConfigStore) Load() (appconfig.Config, error) {
	return s.cfg, nil
}

type injectedSourceIndex struct {
	idx   *schema.Index
	calls *int
}

func (s injectedSourceIndex) Load(ctx context.Context, projectName string, project appconfig.Project) (*schema.Index, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	*s.calls++
	return s.idx, nil
}

func TestDescribeAndDoctorUseSharedAppServiceSources(t *testing.T) {
	home := t.TempDir()
	t.Setenv(appconfig.EnvHome, home)
	path, err := appconfig.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{corrupt global config"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := appconfig.Config{
		Version: appconfig.CurrentConfigVersion,
		Projects: map[string]appconfig.Project{
			"injected": {WorkspaceRoot: "/injected/source", ServicePrefixes: []string{"com.injected."}},
		},
		Servers: map[string]appconfig.Server{
			"injected-test": {Address: "127.0.0.1:12200", Project: "injected", Protocol: "bolt", TimeoutMS: 5000},
		},
	}
	idx := &schema.Index{
		Project: schema.Project{Name: "injected", WorkspaceRoot: "/injected/source", ServicePrefixes: []string{"com.injected."}},
		Methods: []schema.Method{{
			Service: "com.injected.UserFacade", Package: "com.injected", Method: "ping", ReturnType: "String",
		}},
		Types: map[string]schema.TypeSchema{},
	}
	calls := 0
	appSvc := app.New(injectedConfigStore{cfg: cfg})
	appSvc.Source = injectedSourceIndex{idx: idx, calls: &calls}
	register := func(srv *mcpsdk.Server) {
		AddDescribe(srv, appSvc, io.Discard)
		AddDoctor(srv, appSvc, true, io.Discard)
	}
	cs := newToolClient(t, register)

	describe := decodeEnvelope(t, callTool(t, cs, "sofarpc_describe", map[string]any{
		"project": "injected", "query": "ping",
	}))
	if !describe.OK || describe.Data["project"] != "injected" {
		t.Fatalf("describe bypassed injected app service: %+v", describe)
	}
	candidates, _ := describe.Data["candidates"].([]any)
	if len(candidates) != 1 {
		t.Fatalf("describe candidates = %+v, want injected source hit", candidates)
	}

	doctor := decodeEnvelope(t, callTool(t, cs, "sofarpc_doctor", map[string]any{
		"project": "injected", "server": "injected-test", "service": "com.injected.UserFacade", "method": "ping",
	}))
	if !doctor.OK {
		t.Fatalf("doctor bypassed injected app service: %+v", doctor)
	}
	statuses := checkStatuses(t, doctor)
	if statuses["config"] != "ok" || statuses["source_schema"] != "ok" || statuses["describe"] != "ok" {
		t.Fatalf("doctor statuses = %+v", statuses)
	}
	if calls != 2 {
		t.Fatalf("SourceIndex.Load calls = %d, want describe + doctor", calls)
	}
}
