// Package tools is the SofaRPC business-tool layer of the MCP server. Each tool is
// registered on the official go-sdk server (see the *_sdk.go files) with a
// hand-written schema, decodes its typed arguments, and calls the app / schema /
// appconfig packages, returning the unified app.Result envelope.
package tools

import "encoding/json"

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
