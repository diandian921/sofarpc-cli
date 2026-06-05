package mcp

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
)

// connectSDK wires an in-memory client to the SDK server and returns the live
// client session, so tests exercise the real initialize / tools handshake rather
// than calling handlers directly.
func connectSDK(t *testing.T) *mcpsdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverT, clientT := mcpsdk.NewInMemoryTransports()

	srv := newSDKServer(app.New(nil), "test", io.Discard)
	if _, err := srv.Connect(ctx, serverT, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// TestSDKProbeListed checks the piloted tool is advertised with its output schema,
// preserving the "every tool declares an outputSchema" invariant.
func TestSDKProbeListed(t *testing.T) {
	cs := connectSDK(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	var probe *mcpsdk.Tool
	for _, tool := range res.Tools {
		if tool.Name == "sofarpc_probe" {
			probe = tool
		}
	}
	if probe == nil {
		t.Fatal("sofarpc_probe not advertised")
	}
	if probe.OutputSchema == nil {
		t.Error("sofarpc_probe missing outputSchema")
	}
}

// TestSDKProbeWireShape pins the tools/call envelope across the real transport: a
// structured app.Result, a mirrored JSON text block, _meta (elapsedMs / summary),
// and isError with a recovery nextTool on failure.
func TestSDKProbeWireShape(t *testing.T) {
	cs := connectSDK(t)
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "sofarpc_probe",
		Arguments: map[string]any{"address": "127.0.0.1:1", "timeoutMs": 200},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}

	var env struct {
		OK        bool   `json:"ok"`
		Code      string `json:"code"`
		RequestID string `json:"requestId"`
		Error     *struct {
			NextTool string `json:"nextTool"`
		} `json:"error"`
	}
	structured, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(structured, &env); err != nil {
		t.Fatalf("structuredContent not an app.Result: %v", err)
	}
	if env.Code == "" || env.RequestID == "" {
		t.Errorf("envelope missing code/requestId: %s", structured)
	}

	if len(res.Content) == 0 {
		t.Fatal("result has no content block")
	}
	text, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want *TextContent", res.Content[0])
	}
	if !json.Valid([]byte(text.Text)) {
		t.Error("content text block is not the structured JSON")
	}

	if _, has := res.Meta["elapsedMs"]; !has {
		t.Error("_meta missing elapsedMs")
	}
	if _, has := res.Meta["summary"]; !has {
		t.Error("_meta missing summary")
	}

	if !res.IsError {
		t.Error("unreachable address should yield isError=true")
	}
	if env.Error == nil || env.Error.NextTool == "" {
		t.Errorf("failure should carry a recovery nextTool: %s", structured)
	}
}
