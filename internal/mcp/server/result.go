package server

import (
	"encoding/json"
	"time"
)

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// CallResult is the tools/call result payload: a text summary, the structured
// business payload, _meta (requestId / elapsedMs), and the error flag.
type CallResult struct {
	Content           []contentBlock         `json:"content"`
	StructuredContent interface{}            `json:"structuredContent,omitempty"`
	Meta              map[string]interface{} `json:"_meta,omitempty"`
	IsError           bool                   `json:"isError,omitempty"`
}

// wrapResult turns a tool Result into the MCP tools/call payload, stamping
// elapsedMs into _meta without mutating the tool's own meta map. The text block
// carries the serialized structured payload, not just the summary: the MCP tools
// spec says a tool returning structuredContent SHOULD also return that JSON in a
// TextContent block, so a client that does not read structuredContent still sees
// the full result. The human summary is preserved in _meta.
func wrapResult(res Result, elapsed time.Duration) CallResult {
	meta := map[string]interface{}{"elapsedMs": elapsed.Milliseconds()}
	for k, v := range res.Meta {
		meta[k] = v
	}
	if res.Summary != "" {
		meta["summary"] = res.Summary
	}
	text := res.Summary
	if res.Structured != nil {
		if body, err := json.Marshal(res.Structured); err == nil {
			text = string(body)
		}
	}
	return CallResult{
		Content:           []contentBlock{{Type: "text", Text: text}},
		StructuredContent: res.Structured,
		Meta:              meta,
		IsError:           res.IsError,
	}
}
