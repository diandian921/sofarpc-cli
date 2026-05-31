package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diandian921/sofarpc-cli/internal/app"
	"github.com/diandian921/sofarpc-cli/internal/appconfig"
)

type nopRuntime struct{}

func (nopRuntime) Progress(context.Context, string, float64) {}
func (nopRuntime) Log(context.Context, string, string)       {}

func seedConfig(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SOFARPC_HOME", home)
	ws := filepath.Join(home, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir ws: %v", err)
	}
	path, err := appconfig.DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	lock, err := appconfig.DefaultLockPath()
	if err != nil {
		t.Fatalf("lock path: %v", err)
	}
	if _, err := appconfig.Update(path, lock, func(c *appconfig.Config) error {
		if _, err := c.AddProject("user", ws, nil, false); err != nil {
			return err
		}
		_, err := c.AddServer("user-test", appconfig.Server{Address: "127.0.0.1:12200", Project: "user"}, false)
		return err
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	return ws
}

func asResult(t *testing.T, res interface{}) app.Result {
	t.Helper()
	r, ok := res.(app.Result)
	if !ok {
		t.Fatalf("structured content must be app.Result, got %T", res)
	}
	return r
}

func TestResolveToolReturnsEndpoint(t *testing.T) {
	seedConfig(t)
	out := ResolveTool(app.New(nil)).Run(context.Background(), nopRuntime{}, ResolveArgs{Server: "user-test"})
	r := asResult(t, out.Structured)
	if !r.OK {
		t.Fatalf("resolve must succeed: %+v", r)
	}
	if !strings.Contains(string(r.Data), "127.0.0.1:12200") {
		t.Fatalf("resolve data missing endpoint: %s", r.Data)
	}
}

func TestProbeToolConnectFailedCarriesNextTool(t *testing.T) {
	out := ProbeTool(app.New(nil)).Run(context.Background(), nopRuntime{}, ProbeArgs{Address: "127.0.0.1:1"})
	r := asResult(t, out.Structured)
	if r.OK || r.Code != app.CodeConnectFailed {
		t.Fatalf("probe to refused port must fail with CONNECT_FAILED: %+v", r)
	}
	if r.Error == nil || r.Error.NextTool != "sofarpc_probe" {
		t.Fatalf("probe failure must carry nextTool sofarpc_probe: %+v", r.Error)
	}
	if !out.IsError {
		t.Fatal("probe failure must set IsError")
	}
}

func TestDescribeToolRequiresQueryOrService(t *testing.T) {
	seedConfig(t)
	out := DescribeTool(app.New(nil)).Run(context.Background(), nopRuntime{}, DescribeArgs{})
	r := asResult(t, out.Structured)
	if r.OK || r.Code != app.CodeBadRequest {
		t.Fatalf("describe with no query/service must be BAD_REQUEST: %+v", r)
	}
}

func TestDoctorToolReportsChecks(t *testing.T) {
	seedConfig(t)
	out := DoctorTool(app.New(nil), true).Run(context.Background(), nopRuntime{}, DoctorArgs{Server: "user-test"})
	r := asResult(t, out.Structured)
	if !strings.Contains(string(r.Data), `"checks"`) {
		t.Fatalf("doctor must emit checks: %s", r.Data)
	}
}
