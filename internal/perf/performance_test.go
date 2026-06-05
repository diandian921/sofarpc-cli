//go:build perf

package perf

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/mcp"
	"github.com/diandian921/sofarpc-mcp/internal/presentation"
	"github.com/diandian921/sofarpc-mcp/internal/schema"
)

func TestPerformanceBudgets(t *testing.T) {
	enforce := os.Getenv("SOFARPC_ENFORCE_PERF_BUDGET") == "1"
	cases := []struct {
		name   string
		budget time.Duration
		run    func(t *testing.T)
	}{
		{
			name:   "mcp tools list startup",
			budget: 2 * time.Second,
			run:    runMCPToolsList,
		},
		{
			name:   "schema build golden modern",
			budget: 2 * time.Second,
			run:    runSchemaBuildGoldenModern,
		},
		{
			name:   "schema warm cache load",
			budget: time.Second,
			run:    runSchemaWarmCacheLoad,
		},
		{
			name:   "explicit invocation planning",
			budget: 250 * time.Millisecond,
			run:    runExplicitInvocationPlanning,
		},
		{
			name:   "presentation flatten nested response",
			budget: 100 * time.Millisecond,
			run:    runPresentationFlatten,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			tc.run(t)
			elapsed := time.Since(start)
			if elapsed > tc.budget {
				if enforce {
					t.Fatalf("%s took %s, budget %s", tc.name, elapsed, tc.budget)
				}
				t.Logf("%s took %s, over advisory budget %s", tc.name, elapsed, tc.budget)
				return
			}
			t.Logf("%s took %s (budget %s)", tc.name, elapsed, tc.budget)
		})
	}
}

func runMCPToolsList(t *testing.T) {
	t.Helper()
	t.Setenv("SOFARPC_HOME", t.TempDir())
	// SelfTest brings up the SDK server and drives initialize → tools/list →
	// tools/call over in-memory transports — the same startup path, measured
	// reliably (a fixed stdio reader would race the SDK's async handlers on EOF).
	server := &mcp.Server{BuildVersion: "perf", DisableConfigWrite: true}
	if err := server.SelfTest(); err != nil {
		t.Fatalf("selftest: %v", err)
	}
}

func runSchemaBuildGoldenModern(t *testing.T) {
	t.Helper()
	if _, err := schema.BuildIndex(modernGoldenProject()); err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
}

func runSchemaWarmCacheLoad(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	project := modernGoldenProject()
	if _, err := schema.LoadOrBuildIndex(project); err != nil {
		t.Fatalf("cold LoadOrBuildIndex: %v", err)
	}
	if _, err := schema.LoadOrBuildIndex(project); err != nil {
		t.Fatalf("warm LoadOrBuildIndex: %v", err)
	}
}

func runExplicitInvocationPlanning(t *testing.T) {
	t.Helper()
	service := app.New(nil)
	_, err := service.PlanInvocation(context.Background(), app.InvocationInput{
		Address:             "127.0.0.1:12200",
		Service:             "com.example.UserFacade",
		Method:              "getUser",
		ParamTypes:          []string{"java.lang.String"},
		OrderedArguments:    []interface{}{"u001"},
		HasOrderedArguments: true,
	})
	if err != nil {
		t.Fatalf("PlanInvocation: %v", err)
	}
}

func runPresentationFlatten(t *testing.T) {
	t.Helper()
	got := presentation.Flatten(map[string]interface{}{
		"type": "com.example.OperationResult",
		"fields": map[string]interface{}{
			"success": true,
			"data": map[string]interface{}{
				"type": "com.example.Payload",
				"fields": map[string]interface{}{
					"mpCode": int64(433905635109773312),
					"totalAssets": map[string]interface{}{
						"type":   "java.math.BigDecimal",
						"fields": map[string]interface{}{"value": "113795.2485"},
					},
				},
			},
		},
	})
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal flattened response: %v", err)
	}
	if !bytes.Contains(body, []byte(`"totalAssets":113795.2485`)) {
		t.Fatalf("unexpected flattened response: %s", body)
	}
}

func modernGoldenProject() schema.Project {
	return schema.Project{
		Name:            "modern",
		WorkspaceRoot:   filepath.Join("..", "schema", "testdata", "golden", "modern"),
		ServicePrefixes: []string{"com.acme.modern.facade."},
	}
}
