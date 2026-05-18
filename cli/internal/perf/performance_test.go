//go:build perf

package perf

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sofarpc/cli/internal/app"
	"github.com/sofarpc/cli/internal/mcp"
	"github.com/sofarpc/cli/internal/presentation"
	"github.com/sofarpc/cli/internal/schema"
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
	t.Setenv("HOME", t.TempDir())
	out := &bytes.Buffer{}
	server := &mcp.Server{
		BuildVersion:       "perf",
		Stdin:              strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n"),
		Stdout:             out,
		Stderr:             &bytes.Buffer{},
		DisableConfigWrite: true,
	}
	if code := server.Run(); code != 0 {
		t.Fatalf("Run exit = %d", code)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"tools"`)) {
		t.Fatalf("tools/list response missing tools: %s", out.String())
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
