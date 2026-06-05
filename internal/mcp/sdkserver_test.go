package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
)

// syncBuf is a goroutine-safe io.Writer for capturing server output written from
// Run's background session while the test reads it.
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

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

// TestRunStdioHandshake drives the real Run() path over injected streams (the
// production transport), exercising a full initialize → tools/list → tools/call
// handshake and a clean exit 0 on EOF — the stdio-level integration coverage that
// the in-memory client tests do not give.
func TestRunStdioHandshake(t *testing.T) {
	t.Setenv("SOFARPC_HOME", t.TempDir())
	frames := strings.Join([]string{
		`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"sofarpc_config_list","arguments":{}}}`,
		"",
	}, "\n")
	inR, inW := io.Pipe()
	out := &syncBuf{}
	s := &Server{
		BuildVersion:       "test",
		Stdin:              inR,
		Stdout:             out,
		Stderr:             io.Discard,
		DisableConfigWrite: true,
	}
	done := make(chan int, 1)
	go func() { done <- s.Run() }()

	for _, frame := range strings.Split(strings.TrimRight(frames, "\n"), "\n") {
		if _, err := io.WriteString(inW, frame+"\n"); err != nil {
			t.Fatalf("write frame: %v", err)
		}
	}

	// Wait for the tools/call response before closing stdin, so the async handler is
	// not raced against EOF.
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), `"structuredContent"`) {
		if time.Now().After(deadline) {
			_ = inW.Close()
			t.Fatalf("timed out waiting for tools/call response; got:\n%s", out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = inW.Close()

	if code := <-done; code != 0 {
		t.Fatalf("Run exit = %d", code)
	}
	got := out.String()
	if !strings.Contains(got, `"protocolVersion"`) {
		t.Errorf("missing initialize response: %s", got)
	}
	if !strings.Contains(got, `"tools"`) {
		t.Errorf("missing tools/list response: %s", got)
	}
}

// TestConfigRoundtripViaSDK exercises a config write then read end-to-end through the
// SDK server, replacing the equivalent coverage from the retired server_test.go.
func TestConfigRoundtripViaSDK(t *testing.T) {
	t.Setenv("SOFARPC_HOME", t.TempDir())
	cs := connectSDK(t, true)
	ctx := context.Background()

	save, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "sofarpc_config_save_project",
		Arguments: map[string]any{"name": "demo", "workspaceRoot": t.TempDir()},
	})
	if err != nil {
		t.Fatalf("save project: %v", err)
	}
	if save.IsError {
		body, _ := json.Marshal(save.StructuredContent)
		t.Fatalf("save project returned error: %s", body)
	}

	list, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: "sofarpc_config_list"})
	if err != nil {
		t.Fatalf("config list: %v", err)
	}
	body, _ := json.Marshal(list.StructuredContent)
	if !strings.Contains(string(body), `"demo"`) {
		t.Errorf("saved project not listed: %s", body)
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
