package mcp

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
)

// connectSDK wires an in-memory client to the SDK server and returns the live
// client session, so tests exercise the real initialize / tools handshake rather
// than calling handlers directly.
func connectSDK(t *testing.T, writeEnabled bool) *mcpsdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverT, clientT := mcpsdk.NewInMemoryTransports()

	srv := newSDKServer(app.New(nil), "test", writeEnabled, io.Discard)
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
func TestSDKAllToolsListed(t *testing.T) {
	cs := connectSDK(t, true)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	got := map[string]*mcpsdk.Tool{}
	for _, tool := range res.Tools {
		got[tool.Name] = tool
	}
	want := []string{
		"sofarpc_resolve", "sofarpc_probe", "sofarpc_describe", "sofarpc_doctor",
		"sofarpc_config_list", "sofarpc_invoke_plan", "sofarpc_invoke",
		"sofarpc_config_save_project", "sofarpc_config_save_server",
		"sofarpc_config_remove_project", "sofarpc_config_remove_server",
	}
	if len(res.Tools) != len(want) {
		t.Errorf("expected %d tools, got %d", len(want), len(res.Tools))
	}
	for _, name := range want {
		tool, ok := got[name]
		if !ok {
			t.Errorf("missing tool %q", name)
			continue
		}
		// Every tool emits the unified app.Result envelope, so all must advertise it.
		if tool.OutputSchema == nil {
			t.Errorf("tool %q missing outputSchema", name)
		}
	}

	// Read-only tools must advertise the *bool hints (destructive/openWorld) as an
	// explicit false: the SDK omits nil pointers and clients default those hints to
	// true, which would wrongly mark a local read-only tool as open-world.
	if r := got["sofarpc_resolve"]; r != nil {
		if r.Annotations == nil || r.Annotations.OpenWorldHint == nil || *r.Annotations.OpenWorldHint {
			t.Errorf("sofarpc_resolve must advertise openWorldHint:false, got %+v", r.Annotations)
		}
		if r.Annotations.DestructiveHint == nil || *r.Annotations.DestructiveHint {
			t.Errorf("sofarpc_resolve must advertise destructiveHint:false")
		}
	}
}

// TestSDKConfigWriteFriendlyValidation pins finding #2: a config-write tool with a
// missing required field returns the friendly app.Result envelope (isError +
// BAD_REQUEST), not a bare SDK protocol error — preserving the legacy contract.
func TestSDKConfigWriteFriendlyValidation(t *testing.T) {
	cs := connectSDK(t, true)
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "sofarpc_config_save_project",
		Arguments: map[string]any{"workspaceRoot": "/tmp/x"}, // missing required "name"
	})
	if err != nil {
		t.Fatalf("missing required field must be a friendly result, not a protocol error: %v", err)
	}
	if !res.IsError {
		t.Error("expected isError result for missing name")
	}
	structured, _ := json.Marshal(res.StructuredContent)
	if !strings.Contains(string(structured), app.CodeBadRequest) {
		t.Errorf("expected a BAD_REQUEST envelope, got %s", structured)
	}
}

// TestSDKRejectsUnknownArgument pins finding #1: an unknown argument (typo) is
// rejected, not silently ignored, and surfaces as the friendly envelope.
func TestSDKRejectsUnknownArgument(t *testing.T) {
	cs := connectSDK(t, true)
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "sofarpc_resolve",
		Arguments: map[string]any{"projektt": "x"}, // typo of "project"
	})
	if err != nil {
		t.Fatalf("unknown argument must be a friendly result, not a protocol error: %v", err)
	}
	if !res.IsError {
		t.Error("expected isError result for an unknown argument")
	}
	structured, _ := json.Marshal(res.StructuredContent)
	if !strings.Contains(string(structured), "invalid arguments") {
		t.Errorf("expected an invalid-arguments envelope, got %s", structured)
	}
}

// TestSDKDisableConfigWriteHidesWriteTools pins the DisableConfigWrite gating: the
// four config-write tools vanish from tools/list, the seven read tools remain.
func TestSDKDisableConfigWriteHidesWriteTools(t *testing.T) {
	cs := connectSDK(t, false)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	for _, name := range []string{
		"sofarpc_config_save_project", "sofarpc_config_save_server",
		"sofarpc_config_remove_project", "sofarpc_config_remove_server",
	} {
		if got[name] {
			t.Errorf("write tool %q should be hidden when config-write is disabled", name)
		}
	}
	if !got["sofarpc_config_list"] || !got["sofarpc_probe"] {
		t.Error("read tools should remain when config-write is disabled")
	}
}

// TestSDKProbeWireShape pins the tools/call envelope across the real transport: a
// structured app.Result, a mirrored JSON text block, _meta (elapsedMs / summary),
// and isError with a recovery nextTool on failure.
func TestSDKProbeWireShape(t *testing.T) {
	cs := connectSDK(t, true)
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
		Meta map[string]any `json:"meta"`
	}
	structured, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(structured, &env); err != nil {
		t.Fatalf("structuredContent not an app.Result: %v", err)
	}
	if env.Code == "" || env.RequestID == "" {
		t.Errorf("envelope missing code/requestId: %s", structured)
	}

	// app.Result.Meta (runtime/transport) belongs in structuredContent.meta and must
	// NOT leak into the wire _meta — this matches the legacy wrapResult, which only
	// copied requestId into _meta. (Guards against a "merge r.Meta into _meta"
	// regression that would diverge from the old shape.)
	if env.Meta["transport"] != "tcp-dial" || env.Meta["runtime"] != "go" {
		t.Errorf("structuredContent.meta should carry runtime/transport, got %v", env.Meta)
	}
	if _, leaked := res.Meta["transport"]; leaked {
		t.Error("wire _meta must not carry transport")
	}
	if _, leaked := res.Meta["runtime"]; leaked {
		t.Error("wire _meta must not carry runtime")
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
