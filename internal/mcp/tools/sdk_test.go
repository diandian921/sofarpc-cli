package tools

import (
	"context"
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

	result, out, err := handler(context.Background(), &mcpsdk.CallToolRequest{}, struct{}{})
	if err != nil {
		t.Fatalf("panic surfaced as protocol error: %v", err)
	}
	if out.OK || out.Code != app.CodeInternalError {
		t.Fatalf("expected sanitized internal error, got ok=%v code=%q", out.OK, out.Code)
	}
	if out.Error == nil || out.Error.Message != "internal error" {
		t.Fatalf("expected fixed %q message, got %+v", "internal error", out.Error)
	}
	if result == nil || !result.IsError {
		t.Fatal("expected IsError result")
	}
	if strings.Contains(out.Error.Message, secret) {
		t.Error("sensitive panic detail leaked into the client-facing message")
	}
	if !strings.Contains(stderr.String(), secret) || !strings.Contains(stderr.String(), "mcp panic") {
		t.Errorf("panic detail should be logged to stderr, got %q", stderr.String())
	}
}

// TestFinishWireShape pins the _meta and isError fields finish() is responsible for;
// structuredContent and the text block are the SDK's job and are covered by the
// server-level in-memory test.
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
