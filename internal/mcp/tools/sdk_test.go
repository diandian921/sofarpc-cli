package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
)

// TestAdaptToolSanitizesPanic verifies the shared adapter folds a tool panic into a
// fixed internal-error result (not a propagated panic or protocol error), logs the
// detail to stderr under an errorId, and leaks nothing sensitive to the caller.
func TestAdaptToolSanitizesPanic(t *testing.T) {
	const secret = "/secret/path leaked"
	var stderr strings.Builder

	handler := adaptTool(&stderr, func(context.Context, *mcpsdk.CallToolRequest, struct{}) (app.Result, string) {
		panic("boom: " + secret)
	})

	result, err := handler(context.Background(), &mcpsdk.CallToolRequest{Params: &mcpsdk.CallToolParamsRaw{}})
	if err != nil {
		t.Fatalf("panic surfaced as protocol error: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatal("expected an IsError result")
	}

	structured, _ := json.Marshal(result.StructuredContent)
	if !strings.Contains(string(structured), app.CodeInternalError) {
		t.Errorf("expected sanitized internal error envelope, got %s", structured)
	}
	if strings.Contains(string(structured), secret) {
		t.Error("sensitive panic detail leaked into the client-facing result")
	}
	if !strings.Contains(stderr.String(), secret) || !strings.Contains(stderr.String(), "mcp panic") {
		t.Errorf("panic detail should be logged to stderr, got %q", stderr.String())
	}
}

// TestFinishWireShape pins the _meta and isError fields finish() is responsible for.
func TestFinishWireShape(t *testing.T) {
	ok := app.Result{OK: true, Code: app.CodeSuccess, RequestID: "ping-1"}
	res := finish(ok, "done", 0)
	if res.IsError {
		t.Error("ok result must not be IsError")
	}
	if res.Meta["summary"] != "done" || res.Meta["requestId"] != "ping-1" {
		t.Errorf("missing _meta fields: %+v", res.Meta)
	}
	if _, has := res.Meta["elapsedMs"]; !has {
		t.Error("_meta missing elapsedMs")
	}

	failed := app.RenderFailure(app.CodeConnectFailed, "boom", nil)
	if got := finish(failed, "", 0); !got.IsError || got.Meta["summary"] != "boom" {
		t.Errorf("failure should be IsError with summary from error message, got %+v", got)
	}
}
