package tools

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestManualResultMirrorsSmallResults pins the historical wire shape for normal
// payloads: the text block is the exact structuredContent JSON.
func TestManualResultMirrorsSmallResults(t *testing.T) {
	res := manualResult(okResult(map[string]interface{}{"a": "b"}), "s", time.Millisecond)
	text := res.Content[0].(*mcpsdk.TextContent).Text
	structured, _ := json.Marshal(res.StructuredContent)
	if text != string(structured) {
		t.Fatalf("small result text mirror must equal structuredContent:\n%s\n%s", text, structured)
	}
}

// TestManualResultSummarizesLargeResults: above the byte threshold the text block
// becomes a short pointer instead of a second full copy, while structuredContent
// keeps the complete envelope.
func TestManualResultSummarizesLargeResults(t *testing.T) {
	big := strings.Repeat("x", textMirrorMaxBytes)
	res := manualResult(okResult(map[string]interface{}{"blob": big}), "s", time.Millisecond)

	structured, _ := json.Marshal(res.StructuredContent)
	if len(structured) <= textMirrorMaxBytes {
		t.Fatalf("test payload too small: %d", len(structured))
	}
	if !strings.Contains(string(structured), big) {
		t.Fatal("structuredContent must keep the full payload")
	}
	text := res.Content[0].(*mcpsdk.TextContent).Text
	if len(text) > 512 {
		t.Fatalf("large result text block must be a short pointer, got %d bytes", len(text))
	}
	if !strings.Contains(text, "structuredContent") || !strings.Contains(text, "resultPath") {
		t.Fatalf("pointer text must direct the agent to structuredContent/resultPath: %q", text)
	}
}
