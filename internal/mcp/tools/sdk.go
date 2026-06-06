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

// adaptTool turns a typed appToolFunc into a raw SDK ToolHandler installed via
// Server.AddTool. It deliberately avoids the generic mcp.AddTool path, whose input
// validation (applySchema) re-marshals arguments through float64 — corrupting Java
// long values — and turns missing-required / unknown fields into protocol errors
// instead of the friendly app.Result envelope. This adapter mirrors the legacy
// framework instead, and is the single place that owns what every tool shares:
//   - decode arguments with UseNumber + DisallowUnknownFields (precision kept,
//     typos/unknown fields rejected); all other validation stays in the handler;
//   - stamp elapsedMs into _meta and fold app.Result into structuredContent + a JSON
//     text block (manualResult);
//   - recover a panic into a fixed, detail-free failure (sanitizePanic), so nothing
//     sensitive reaches the agent.
func adaptTool[In any](stderr io.Writer, run appToolFunc[In]) mcpsdk.ToolHandler {
	return func(ctx context.Context, req *mcpsdk.CallToolRequest) (result *mcpsdk.CallToolResult, err error) {
		start := time.Now()
		defer func() {
			if recovered := recover(); recovered != nil {
				result = manualResult(sanitizePanic(recovered, stderr), "", time.Since(start))
			}
		}()
		var in In
		if derr := decodeStrict(req.Params.Arguments, &in); derr != nil {
			bad := app.RenderFailure(app.CodeBadRequest, "invalid arguments: "+derr.Error(), nil)
			return manualResult(bad, "", time.Since(start)), nil
		}
		out, summary := run(ctx, req, in)
		return manualResult(out, summary, time.Since(start)), nil
	}
}

// finish stamps the _meta (elapsedMs / requestId / summary) and isError fields of a
// tools/call result from an app.Result. manualResult then adds structuredContent and
// the mirrored JSON text block, so the wire shape matches the historical CallResult.
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

// manualResult builds the complete CallToolResult: structuredContent plus a JSON
// text block from the app.Result, plus the _meta and isError that finish() stamps.
// (The raw Server.AddTool path does no auto folding, so the adapter does it here.)
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
	// Every tool's data.* output schema assumes data is a JSON object; refuse a scalar,
	// array, or null payload rather than emit a result that breaks that contract.
	if trimmed := bytes.TrimSpace(body); len(trimmed) == 0 || trimmed[0] != '{' {
		return app.RenderFailure(app.CodeInternalError, "tool data must be a JSON object", nil)
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

// decodeStrict decodes raw tool arguments exactly like the legacy decodeArgs:
// UseNumber keeps large Java long values as json.Number (no float64 rounding), and
// DisallowUnknownFields rejects typos and unsupported keys instead of silently
// ignoring them. Server-side aliases stay decodable by being declared struct fields.
func decodeStrict(raw json.RawMessage, out interface{}) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	dec.DisallowUnknownFields()
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
