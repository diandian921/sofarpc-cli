package tools

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

// seedFacadeProject seeds a project whose workspace holds n distinct facades, each
// exposing a getUser method, so query-mode limits can be observed.
func seedFacadeProject(t *testing.T, n int) {
	t.Helper()
	ws := t.TempDir()
	src := filepath.Join(ws, "src", "main", "java", "com", "example", "user")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= n; i++ {
		name := fmt.Sprintf("UserFacade%d", i)
		java := fmt.Sprintf("package com.example.user;\n\npublic interface %s {\n    String getUser(String userId);\n}\n", name)
		if err := os.WriteFile(filepath.Join(src, name+".java"), []byte(java), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	seedConfig(t, func(cfg *appconfig.Config) error {
		_, err := cfg.AddProject("user", ws, []string{"com.example."}, false)
		return err
	})
}

func registerDescribe(srv *mcpsdk.Server) {
	AddDescribe(srv, app.New(nil), io.Discard)
}

func describeCandidates(t *testing.T, env resultEnvelope) []any {
	t.Helper()
	candidates, _ := env.Data["candidates"].([]any)
	return candidates
}

// TestDescribeRequiresQueryOrService pins the argument gate: with neither query
// nor service the tool fails BAD_REQUEST and the recovery text names both modes.
func TestDescribeRequiresQueryOrService(t *testing.T) {
	seedConfig(t, nil)
	cs := newToolClient(t, registerDescribe)
	res := callTool(t, cs, "sofarpc_describe", nil)
	env := decodeEnvelope(t, res)
	if !res.IsError || env.Code != app.CodeBadRequest {
		t.Fatalf("missing query and service must be BAD_REQUEST, got %+v", env)
	}
	if env.Error == nil || !strings.Contains(env.Error.Recovery, "query") || !strings.Contains(env.Error.Recovery, "service") {
		t.Errorf("recovery should explain both modes, got %+v", env.Error)
	}
}

// TestDescribeQueryLimitHandling pins the limit policy in query mode: omitted
// limit falls back to the default of 5, an in-range limit is honored, and any
// value above 20 is capped to 20 instead of rejected.
func TestDescribeQueryLimitHandling(t *testing.T) {
	seedFacadeProject(t, describeMaxLimit+5)
	cs := newToolClient(t, registerDescribe)

	def := decodeEnvelope(t, callTool(t, cs, "sofarpc_describe",
		map[string]any{"project": "user", "query": "get user"}))
	if !def.OK {
		t.Fatalf("query mode failed: %+v", def)
	}
	if got := len(describeCandidates(t, def)); got != describeDefaultLimit {
		t.Errorf("omitted limit should return %d candidates, got %d", describeDefaultLimit, got)
	}

	seven := decodeEnvelope(t, callTool(t, cs, "sofarpc_describe",
		map[string]any{"project": "user", "query": "get user", "limit": 7}))
	if got := len(describeCandidates(t, seven)); got != 7 {
		t.Errorf("limit=7 should return 7 candidates, got %d", got)
	}

	capped := decodeEnvelope(t, callTool(t, cs, "sofarpc_describe",
		map[string]any{"project": "user", "query": "get user", "limit": 100}))
	if got := len(describeCandidates(t, capped)); got != describeMaxLimit {
		t.Errorf("limit above %d must be capped to it, got %d candidates", describeMaxLimit, got)
	}
	if capped.Data["query"] != "get user" {
		t.Errorf("data.query should echo the query, got %v", capped.Data["query"])
	}
}

// TestDescribeServiceMode pins describe mode: a known FQN returns its methods, a
// method filter narrows them, and an unknown FQN or method is a BAD_REQUEST error
// rather than an empty success.
func TestDescribeServiceMode(t *testing.T) {
	seedFacadeProject(t, 1)
	cs := newToolClient(t, registerDescribe)

	env := decodeEnvelope(t, callTool(t, cs, "sofarpc_describe",
		map[string]any{"project": "user", "service": "com.example.user.UserFacade1"}))
	if !env.OK {
		t.Fatalf("service mode failed: %+v", env)
	}
	description, _ := env.Data["description"].(map[string]any)
	if description == nil {
		t.Fatalf("data.description missing: %+v", env.Data)
	}
	methods, _ := description["methods"].([]any)
	if len(methods) != 1 {
		t.Fatalf("expected 1 described method, got %d", len(methods))
	}
	if _, hasCandidates := env.Data["candidates"]; hasCandidates {
		t.Error("service-only mode must not emit candidates")
	}

	filtered := decodeEnvelope(t, callTool(t, cs, "sofarpc_describe",
		map[string]any{"project": "user", "service": "com.example.user.UserFacade1", "method": "getUser"}))
	if !filtered.OK {
		t.Errorf("method filter on an existing method should succeed: %+v", filtered)
	}

	unknown := callTool(t, cs, "sofarpc_describe",
		map[string]any{"project": "user", "service": "com.example.user.Ghost"})
	if env := decodeEnvelope(t, unknown); !unknown.IsError || env.Code != app.CodeBadRequest {
		t.Errorf("unknown service must be BAD_REQUEST, got %+v", env)
	}
}

// TestDescribeUnknownProject pins project resolution failure: an explicit project
// that is not configured surfaces as BAD_REQUEST naming the project.
func TestDescribeUnknownProject(t *testing.T) {
	seedConfig(t, nil)
	cs := newToolClient(t, registerDescribe)
	res := callTool(t, cs, "sofarpc_describe", map[string]any{"project": "ghost", "query": "x"})
	env := decodeEnvelope(t, res)
	if !res.IsError || env.Code != app.CodeBadRequest {
		t.Fatalf("unknown project must be BAD_REQUEST, got %+v", env)
	}
	if env.Error == nil || !strings.Contains(env.Error.Message, "ghost") {
		t.Errorf("error should name the unknown project, got %+v", env.Error)
	}
}
