package server

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestWrapResultSerializesStructuredIntoText pins the MCP backwards-compat rule:
// when a tool returns structuredContent, the same JSON must also appear in a
// TextContent block so a client that ignores structuredContent still sees the
// full result. The human summary moves to _meta.
func TestWrapResultSerializesStructuredIntoText(t *testing.T) {
	structured := map[string]interface{}{"ok": true, "data": "x"}
	got := wrapResult(Result{Structured: structured, Summary: "done"}, 0)

	if len(got.Content) != 1 || got.Content[0].Type != "text" {
		t.Fatalf("want one text content block, got %#v", got.Content)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(got.Content[0].Text), &decoded); err != nil {
		t.Fatalf("text block is not valid JSON: %q (%v)", got.Content[0].Text, err)
	}
	if !reflect.DeepEqual(decoded, structured) {
		t.Fatalf("text JSON = %#v, want it to mirror structuredContent %#v", decoded, structured)
	}
	if !reflect.DeepEqual(got.StructuredContent, structured) {
		t.Fatalf("structuredContent dropped: %#v", got.StructuredContent)
	}
	if got.Meta["summary"] != "done" {
		t.Fatalf("summary must be preserved in _meta, got %#v", got.Meta["summary"])
	}
}

// TestWrapResultFallsBackToSummary covers tools that return no structured payload:
// the text block keeps the summary rather than emitting an empty or "null" body.
func TestWrapResultFallsBackToSummary(t *testing.T) {
	got := wrapResult(Result{Summary: "just text"}, 0)
	if len(got.Content) != 1 || got.Content[0].Text != "just text" {
		t.Fatalf("want summary fallback text, got %#v", got.Content)
	}
}
