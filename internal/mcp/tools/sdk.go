package tools

import (
	"bytes"
	"context"
	"encoding/json"
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

// rawToolFunc is the app-facing shape of a tool that needs the raw request, e.g.
// to decode arguments with number-precision control. Like appToolFunc, it returns
// the unified envelope plus a summary.
type rawToolFunc func(ctx context.Context, req *mcpsdk.CallToolRequest) (app.Result, string)

// adaptRawTool is adaptTool for the raw Server.AddTool path: same timing and panic
// sanitization, but it owns structuredContent/content generation (via manualResult)
// because the raw path does no auto folding. invoke/invoke_plan use it so their
// arguments bypass the SDK's generic float64 roundtrip and keep Java long precision.
func adaptRawTool(stderr io.Writer, run rawToolFunc) mcpsdk.ToolHandler {
	return func(ctx context.Context, req *mcpsdk.CallToolRequest) (result *mcpsdk.CallToolResult, err error) {
		start := time.Now()
		defer func() {
			if recovered := recover(); recovered != nil {
				result = manualResult(sanitizePanic(recovered, stderr), "", time.Since(start))
			}
		}()
		out, summary := run(ctx, req)
		return manualResult(out, summary, time.Since(start)), nil
	}
}

// manualResult builds a complete CallToolResult for the raw Server.AddTool path,
// mirroring what generic AddTool produces: structuredContent plus a JSON text block
// from the app.Result, plus the _meta and isError that finish() stamps.
func manualResult(r app.Result, summary string, elapsed time.Duration) *mcpsdk.CallToolResult {
	body, err := json.Marshal(r)
	if err != nil {
		r = app.RenderFailure(app.CodeInternalError, err.Error(), nil)
		summary = ""
		body, _ = json.Marshal(r)
	}
	res := finish(r, summary, elapsed)
	res.StructuredContent = json.RawMessage(body)
	res.Content = []mcpsdk.Content{&mcpsdk.TextContent{Text: string(body)}}
	return res
}

// okResult builds a successful app.Result whose data is the marshaled business
// payload, mirroring the legacy success() helper but returning the bare envelope
// (the summary is carried separately by the appToolFunc).
func okResult(data interface{}) app.Result {
	body, err := json.Marshal(data)
	if err != nil {
		return app.RenderFailure(app.CodeInternalError, err.Error(), nil)
	}
	return app.Result{OK: true, Code: app.CodeSuccess, Data: body}
}

// notifyProgress emits a progress notification, but only when the caller supplied
// a progress token (per MCP). A nil token means the client did not ask for
// progress, so we stay silent rather than send unsolicited notifications.
func notifyProgress(ctx context.Context, req *mcpsdk.CallToolRequest, message string, progress float64) {
	token := req.Params.GetProgressToken()
	if token == nil {
		return
	}
	_ = req.Session.NotifyProgress(ctx, &mcpsdk.ProgressNotificationParams{
		ProgressToken: token,
		Message:       message,
		Progress:      progress,
	})
}

// decodeStrict decodes raw tool arguments with UseNumber, so large Java long
// values survive as json.Number instead of being rounded through float64. The
// SDK's generic decoding does not do this, so invoke/invoke_plan take their
// arguments as json.RawMessage and decode here before Hessian encoding.
func decodeStrict(raw json.RawMessage, out interface{}) error {
	if len(raw) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(out)
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
