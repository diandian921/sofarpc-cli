package server

import "time"

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
// elapsedMs into _meta without mutating the tool's own meta map.
func wrapResult(res Result, elapsed time.Duration) CallResult {
	meta := map[string]interface{}{"elapsedMs": elapsed.Milliseconds()}
	for k, v := range res.Meta {
		meta[k] = v
	}
	return CallResult{
		Content:           []contentBlock{{Type: "text", Text: res.Summary}},
		StructuredContent: res.Structured,
		Meta:              meta,
		IsError:           res.IsError,
	}
}
