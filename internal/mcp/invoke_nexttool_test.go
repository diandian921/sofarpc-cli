package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/diandian921/sofarpc-cli/internal/app"
)

// TestInvokePlanningFailureCarriesNextTool guards the regression where MCP
// planning errors short-circuited to a bare {ok,message,error} map without a
// stable code or nextTool — exactly the path agents most need to recover from.
func TestInvokePlanningFailureCarriesNextTool(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SOFARPC_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.json"),
		[]byte(`{"version":1,"projects":{},"servers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &Server{BuildVersion: "test"}
	res := s.invoke(context.Background(), map[string]interface{}{
		"service": "x.Y",
		"method":  "m",
		"server":  "no-such-server",
	})

	if !res.IsError {
		t.Fatal("planning failure must produce IsError=true")
	}
	rendered, ok := res.StructuredContent.(app.Result)
	if !ok {
		t.Fatalf("StructuredContent must be app.Result, got %T", res.StructuredContent)
	}
	if rendered.OK {
		t.Fatalf("rendered.ok must be false on planning failure, got %+v", rendered)
	}
	if rendered.Code != app.CodeBadRequest {
		t.Fatalf("rendered.code = %q, want %q", rendered.Code, app.CodeBadRequest)
	}
	if rendered.Error == nil || rendered.Error.NextTool == "" {
		t.Fatalf("planning failure must carry error.nextTool, got %+v", rendered.Error)
	}
}
