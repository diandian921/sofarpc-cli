// Package tools is layer 3 of the MCP server: the SofaRPC business tools. Each
// tool is a typed server.Tool with a hand-written schema that calls the app /
// schema / appconfig packages. Tools reach progress and logging through
// server.Runtime and never import the proto layer.
package tools

import (
	"encoding/json"

	"github.com/diandian921/sofarpc-cli/internal/app"
	"github.com/diandian921/sofarpc-cli/internal/mcp/server"
)

// resultOutputSchema describes the unified app.Result envelope every tool emits.
var resultOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "ok": {"type": "boolean"},
    "code": {"type": "string"},
    "requestId": {"type": "string"},
    "data": {"type": "object"},
    "error": {
      "type": "object",
      "properties": {
        "message": {"type": "string"},
        "cause": {"type": "string"},
        "nextTool": {"type": "string"},
        "recovery": {"type": "string"},
        "details": {"type": "object"}
      }
    },
    "meta": {"type": "object"}
  }
}`)

// rendered wraps a fully-formed app.Result as a tool result, carrying ok / error
// through to isError and surfacing requestId in _meta.
func rendered(r app.Result, summary string) server.Result {
	if summary == "" && r.Error != nil {
		summary = r.Error.Message
	}
	var meta map[string]interface{}
	if r.RequestID != "" {
		meta = map[string]interface{}{"requestId": r.RequestID}
	}
	return server.Result{Structured: r, Summary: summary, IsError: !r.OK, Meta: meta}
}

// success builds an OK app.Result whose data is the marshaled business payload,
// so every successful tool emits the same envelope shape.
func success(summary string, data interface{}) server.Result {
	body, err := json.Marshal(data)
	if err != nil {
		return rendered(app.RenderFailure(app.CodeInternalError, err.Error(), nil), "")
	}
	return rendered(app.Result{OK: true, Code: app.CodeSuccess, Data: body}, summary)
}

// failure wraps a local failure as a tool result with a recovery hint.
func failure(code, message string, details map[string]interface{}) server.Result {
	return rendered(app.RenderFailure(code, message, details), "")
}
