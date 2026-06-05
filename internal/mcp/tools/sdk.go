package tools

import (
	"context"
	"fmt"
	"io"
	"runtime/debug"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
)

// This file is the SDK-native adapter layer used by the migration to the official
// modelcontextprotocol/go-sdk. It replaces the hand-written server.Result wrapping
// (server/result.go) and server.Runtime: tools become plain functions that return
// the unified app.Result envelope, and the helpers here fold that envelope into the
// SDK's CallToolResult while preserving the established wire shape.

// boolPtr returns a pointer to b. The SDK types some tool-annotation hints as *bool
// (tri-state) and others as plain bool; this is for the pointer-typed ones.
func boolPtr(b bool) *bool { return &b }

// appToolFunc is the app-facing shape of an SDK tool body: it consumes typed
// arguments (and the request, for progress notifications) and returns the unified
// app.Result envelope plus an optional human-readable summary.
type appToolFunc[In any] func(ctx context.Context, req *mcpsdk.CallToolRequest, in In) (app.Result, string)

// adaptTool turns an appToolFunc into the SDK's generic ToolHandlerFor. It is the
// single place that owns the three concerns every tool shares, so individual tools
// stay free of protocol plumbing:
//   - timing: stamps elapsedMs into _meta;
//   - panic safety: a panic is logged to stderr under a generated errorId and folded
//     into a fixed, detail-free failure, so nothing sensitive reaches the agent;
//   - wire shape: the returned Out (app.Result) becomes structuredContent plus a JSON
//     text block (filled by the SDK), while _meta and isError are set here.
func adaptTool[In any](stderr io.Writer, run appToolFunc[In]) mcpsdk.ToolHandlerFor[In, app.Result] {
	return func(ctx context.Context, req *mcpsdk.CallToolRequest, in In) (result *mcpsdk.CallToolResult, out app.Result, err error) {
		start := time.Now()
		defer func() {
			if recovered := recover(); recovered != nil {
				out = sanitizePanic(recovered, stderr)
				result = finish(out, "", time.Since(start))
			}
		}()
		out, summary := run(ctx, req, in)
		return finish(out, summary, time.Since(start)), out, nil
	}
}

// finish renders an app.Result into the tools/call envelope. It only sets _meta
// (elapsedMs / requestId / summary) and isError; structuredContent and the mirrored
// JSON text block are populated by the SDK from the returned Out value, so the wire
// shape matches the historical CallResult exactly.
func finish(r app.Result, summary string, elapsed time.Duration) *mcpsdk.CallToolResult {
	if summary == "" && r.Error != nil {
		summary = r.Error.Message
	}
	meta := mcpsdk.Meta{"elapsedMs": elapsed.Milliseconds()}
	if r.RequestID != "" {
		meta["requestId"] = r.RequestID
	}
	if summary != "" {
		meta["summary"] = summary
	}
	return &mcpsdk.CallToolResult{Meta: meta, IsError: !r.OK}
}

// sanitizePanic logs the panic value and stack to stderr under a generated errorId
// and returns a fixed internal-error result, mirroring the previous proto-layer
// recover so no paths, payloads, or stack frames leak to the agent.
func sanitizePanic(recovered any, stderr io.Writer) app.Result {
	errorID := app.NewRequestID("panic")
	if stderr != nil {
		fmt.Fprintf(stderr, "mcp panic [%s]: %v\n%s\n", errorID, recovered, debug.Stack())
	}
	return app.RenderFailure(app.CodeInternalError, "internal error", map[string]interface{}{"errorId": errorID})
}
