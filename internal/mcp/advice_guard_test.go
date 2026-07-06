package mcp

import (
	"context"
	"encoding/json"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

// TestFailureNextToolNeverPointsBackAtItself pins the recovery-advice invariant
// across the whole tool surface: whatever way a tool fails, error.nextTool must
// name a different tool (or be empty) — an agent told to "retry the tool that
// just failed, unchanged" can only loop.
func TestFailureNextToolNeverPointsBackAtItself(t *testing.T) {
	t.Setenv("SOFARPC_HOME", t.TempDir())
	path, err := appconfig.DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	lock, err := appconfig.DefaultLockPath()
	if err != nil {
		t.Fatalf("lock path: %v", err)
	}
	if _, err := appconfig.Update(path, lock, func(c *appconfig.Config) error {
		if _, err := c.AddProject("user", t.TempDir(), nil, false); err != nil {
			return err
		}
		_, err := c.AddServer("user-test", appconfig.Server{Address: "127.0.0.1:9", Project: "user", TimeoutMS: 300}, false)
		return err
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cs := connectSDK(t, true)
	ctx := context.Background()

	cases := []struct {
		tool string
		args map[string]any
	}{
		{"sofarpc_resolve", map[string]any{"server": "missing"}},
		{"sofarpc_probe", map[string]any{"server": "user-test", "timeoutMs": 300}},
		{"sofarpc_describe", map[string]any{}},
		{"sofarpc_invoke", map[string]any{"server": "missing", "service": "com.example.S", "method": "m"}},
		{"sofarpc_invoke_plan", map[string]any{"server": "missing", "service": "com.example.S", "method": "m"}},
		{"sofarpc_config_save_project", map[string]any{"name": "", "workspaceRoot": ""}},
		{"sofarpc_config_save_server", map[string]any{"name": "", "address": "", "project": ""}},
		{"sofarpc_config_remove_project", map[string]any{"name": ""}},
		{"sofarpc_config_remove_server", map[string]any{"name": ""}},
		{"sofarpc_doctor", map[string]any{"server": "does-not-exist"}},
	}
	for _, c := range cases {
		res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: c.tool, Arguments: c.args})
		if err != nil {
			t.Fatalf("%s: protocol error: %v", c.tool, err)
		}
		if !res.IsError {
			t.Errorf("%s: expected a failure result for %v", c.tool, c.args)
			continue
		}
		structured, _ := json.Marshal(res.StructuredContent)
		var env struct {
			Error *struct {
				NextTool string `json:"nextTool"`
			} `json:"error"`
		}
		if err := json.Unmarshal(structured, &env); err != nil {
			t.Fatalf("%s: structuredContent not an app.Result: %v", c.tool, err)
		}
		if env.Error == nil {
			t.Errorf("%s: failure without error envelope: %s", c.tool, structured)
			continue
		}
		if env.Error.NextTool == c.tool {
			t.Errorf("%s: error.nextTool points back at the failing tool itself: %s", c.tool, structured)
		}
	}
}
