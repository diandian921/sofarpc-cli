package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
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

// TestServerInstructionsIncludeFailurePath pins the failure-recovery guidance in the
// initialize instructions: the entry guidance must point agents at the machine-readable
// recovery fields (error.nextTool / error.recovery) and the diagnostic tools, not just
// the happy path, so a first failure does not send the agent guessing from prose.
func TestServerInstructionsIncludeFailurePath(t *testing.T) {
	cs := connectSDK(t, true)
	instructions := cs.InitializeResult().Instructions
	for _, want := range []string{"nextTool", "recovery", "sofarpc_doctor", "sofarpc_probe"} {
		if !strings.Contains(instructions, want) {
			t.Errorf("initialize instructions missing %q; got: %s", want, instructions)
		}
	}
}

// TestInvokeRejectsLegacyAliases pins the invoke contract收敛: the undocumented
// `types` / `args` aliases are removed from InvokeArgs, so passing them is an
// unknown-field error (the friendly invalid-arguments envelope) instead of a silently
// accepted hidden contract that tools/list never advertised.
func TestInvokeRejectsLegacyAliases(t *testing.T) {
	cs := connectSDK(t, true)
	for _, alias := range []string{"types", "args"} {
		res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
			Name: "sofarpc_invoke_plan",
			Arguments: map[string]any{
				"service": "com.example.S", "method": "m", alias: []any{"x"},
			},
		})
		if err != nil {
			t.Fatalf("%s: unexpected protocol error: %v", alias, err)
		}
		structured, _ := json.Marshal(res.StructuredContent)
		if !res.IsError || !strings.Contains(string(structured), "invalid arguments") {
			t.Errorf("alias %q must be rejected as an unknown field, got %s", alias, structured)
		}
	}
}

// TestEachToolDataSchemaIsToolSpecific pins the per-tool output contract: every tool
// must describe its real data.* shape, not the shared bare `data: object`. Without it,
// tools/list hides what an agent should expect to read back from each tool.
func TestEachToolDataSchemaIsToolSpecific(t *testing.T) {
	cs := connectSDK(t, true)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, tool := range res.Tools {
		raw, _ := json.Marshal(tool.OutputSchema)
		var parsed struct {
			Properties struct {
				Data struct {
					Type       string         `json:"type"`
					Properties map[string]any `json:"properties"`
				} `json:"data"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("%s outputSchema not parseable: %v", tool.Name, err)
		}
		if parsed.Properties.Data.Type != "object" {
			t.Errorf("%s: data schema should be type object, got %q", tool.Name, parsed.Properties.Data.Type)
		}
		if len(parsed.Properties.Data.Properties) == 0 {
			t.Errorf("%s: data still uses the shared bare object schema; declare tool-specific properties", tool.Name)
		}
	}
}

// TestToolSuccessOutputsMatchTheirSchema is the semantic guard behind the structural
// one: it drives each tool that can succeed without a live RPC to a real result and
// validates that result against the tool's own advertised OutputSchema, so a wrong or
// too-strict per-tool schema is caught (raw Server.AddTool does not validate output).
func TestToolSuccessOutputsMatchTheirSchema(t *testing.T) {
	t.Setenv("SOFARPC_HOME", t.TempDir())
	path, err := appconfig.DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	lock, err := appconfig.DefaultLockPath()
	if err != nil {
		t.Fatalf("lock path: %v", err)
	}
	if _, err := appconfig.Update(path, lock, func(c *appconfig.Config) error {
		if _, err := c.AddProject("user", t.TempDir(), nil, false); err != nil {
			return err
		}
		_, err := c.AddServer("user-test", appconfig.Server{Address: "127.0.0.1:12200", Project: "user"}, false)
		return err
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cs := connectSDK(t, true)
	ctx := context.Background()

	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	validators := map[string]*jsonschema.Resolved{}
	for _, tl := range listed.Tools {
		raw, _ := json.Marshal(tl.OutputSchema)
		var s jsonschema.Schema
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatalf("%s outputSchema unmarshal: %v", tl.Name, err)
		}
		rs, err := s.Resolve(nil)
		if err != nil {
			t.Fatalf("%s outputSchema resolve: %v", tl.Name, err)
		}
		validators[tl.Name] = rs
	}

	cases := []struct {
		name        string
		args        map[string]any
		wantSuccess bool
	}{
		{"sofarpc_config_list", nil, true},
		{"sofarpc_resolve", map[string]any{"server": "user-test"}, true},
		{"sofarpc_describe", map[string]any{"project": "user", "query": "anything"}, true},
		{"sofarpc_invoke_plan", map[string]any{
			"server": "user-test", "service": "com.example.S", "method": "m",
			"paramTypes": []any{"java.lang.String"}, "orderedArguments": []any{"x"},
		}, true},
		{"sofarpc_doctor", map[string]any{"server": "user-test"}, false},
		{"sofarpc_config_save_project", map[string]any{"name": "p2", "workspaceRoot": t.TempDir()}, true},
		{"sofarpc_config_save_server", map[string]any{"name": "s2", "address": "127.0.0.1:1", "project": "user"}, true},
		{"sofarpc_config_remove_server", map[string]any{"name": "s2", "confirm": true}, true},
		{"sofarpc_config_remove_project", map[string]any{"name": "p2", "confirm": true}, true},
	}
	for _, c := range cases {
		res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: c.name, Arguments: c.args})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		raw, _ := json.Marshal(res.StructuredContent)
		if c.wantSuccess && res.IsError {
			t.Errorf("%s expected success, got error: %s", c.name, raw)
		}
		var inst any
		if err := json.Unmarshal(raw, &inst); err != nil {
			t.Fatalf("%s structuredContent: %v", c.name, err)
		}
		if err := validators[c.name].Validate(inst); err != nil {
			t.Errorf("%s output violates its advertised schema: %v\n%s", c.name, err, raw)
		}
	}
}

// TestDoctorFailureCarriesRecoveryEnvelope pins that a doctor run with a failing check
// is a real isError result that still honors the universal contract: an error envelope
// with nextTool + recovery, while preserving data.checks (so the new initialize
// guidance and README's "isError plus a recovery nextTool/recovery" hold for doctor too).
func TestDoctorFailureCarriesRecoveryEnvelope(t *testing.T) {
	t.Setenv("SOFARPC_HOME", t.TempDir())
	cs := connectSDK(t, true)
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "sofarpc_doctor",
		Arguments: map[string]any{"server": "does-not-exist"},
	})
	if err != nil {
		t.Fatalf("call doctor: %v", err)
	}
	if !res.IsError {
		t.Fatalf("doctor with a failing check should be isError")
	}
	structured, _ := json.Marshal(res.StructuredContent)
	var env struct {
		Error *struct {
			NextTool string `json:"nextTool"`
			Recovery string `json:"recovery"`
		} `json:"error"`
		Data struct {
			Checks []map[string]any `json:"checks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(structured, &env); err != nil {
		t.Fatalf("structuredContent: %v", err)
	}
	if env.Error == nil || env.Error.NextTool == "" || env.Error.Recovery == "" {
		t.Errorf("doctor failure must carry error.nextTool + recovery: %s", structured)
	}
	if len(env.Data.Checks) == 0 {
		t.Errorf("doctor failure must still carry data.checks: %s", structured)
	}
}

// TestDescribeCandidatesAreFlattenedAndScored pins the Sprint 2 describe enhancement:
// query-mode candidates carry agent-ready paramTypes / parameterNames (flattened out of
// the parameters array so they can be copied straight into an invoke) plus the score
// and a reason, instead of only the raw schema.Method shape.
func TestDescribeCandidatesAreFlattenedAndScored(t *testing.T) {
	t.Setenv("SOFARPC_HOME", t.TempDir())
	ws := t.TempDir()
	src := filepath.Join(ws, "src", "main", "java", "com", "example", "user")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	java := "package com.example.user;\n\npublic interface UserFacade {\n    String getUser(String userId);\n}\n"
	if err := os.WriteFile(filepath.Join(src, "UserFacade.java"), []byte(java), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := appconfig.DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	lock, err := appconfig.DefaultLockPath()
	if err != nil {
		t.Fatalf("lock path: %v", err)
	}
	if _, err := appconfig.Update(path, lock, func(c *appconfig.Config) error {
		_, err := c.AddProject("user", ws, []string{"com.example."}, false)
		return err
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cs := connectSDK(t, true)
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "sofarpc_describe",
		Arguments: map[string]any{"project": "user", "query": "get user"},
	})
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	structured, _ := json.Marshal(res.StructuredContent)
	var env struct {
		Data struct {
			Candidates []struct {
				Service        string   `json:"service"`
				Method         string   `json:"method"`
				ParamTypes     []string `json:"paramTypes"`
				ParameterNames []string `json:"parameterNames"`
				Score          int      `json:"score"`
				Reason         string   `json:"reason"`
			} `json:"candidates"`
		} `json:"data"`
	}
	if err := json.Unmarshal(structured, &env); err != nil {
		t.Fatalf("structuredContent: %v", err)
	}
	if len(env.Data.Candidates) == 0 {
		t.Fatalf("expected a candidate for getUser: %s", structured)
	}
	c := env.Data.Candidates[0]
	if c.Method != "getUser" {
		t.Errorf("candidate method = %q, want getUser", c.Method)
	}
	if len(c.ParameterNames) != 1 || c.ParameterNames[0] != "userId" {
		t.Errorf("parameterNames = %v, want [userId]", c.ParameterNames)
	}
	if len(c.ParamTypes) != 1 || c.ParamTypes[0] != "java.lang.String" {
		t.Errorf("paramTypes = %v, want normalized [java.lang.String] so it is copyable into invoke", c.ParamTypes)
	}
	if c.Score <= 0 {
		t.Errorf("score = %d, want > 0", c.Score)
	}
	if c.Reason == "" {
		t.Errorf("candidate reason should explain the match: %s", structured)
	}
}

// TestAmbiguousServerErrorListsCandidates pins the Sprint 2 resolve enhancement: when a
// server is required but several match, the ENDPOINT_NOT_FOUND error names the candidate
// servers (server/project/address) so the agent can pick one, instead of only reporting
// a count. No score is fabricated — there is no query to rank by — and attachments are
// never included.
func TestAmbiguousServerErrorListsCandidates(t *testing.T) {
	t.Setenv("SOFARPC_HOME", t.TempDir())
	path, err := appconfig.DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	lock, err := appconfig.DefaultLockPath()
	if err != nil {
		t.Fatalf("lock path: %v", err)
	}
	if _, err := appconfig.Update(path, lock, func(c *appconfig.Config) error {
		if _, err := c.AddProject("user", t.TempDir(), nil, false); err != nil {
			return err
		}
		if _, err := c.AddServer("user-a", appconfig.Server{Address: "127.0.0.1:12200", Project: "user"}, false); err != nil {
			return err
		}
		_, err := c.AddServer("user-b", appconfig.Server{Address: "127.0.0.1:12300", Project: "user"}, false)
		return err
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cs := connectSDK(t, true)
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "sofarpc_invoke_plan",
		Arguments: map[string]any{
			"service": "com.example.S", "method": "m",
			"paramTypes": []any{"java.lang.String"}, "orderedArguments": []any{"x"},
		},
	})
	if err != nil {
		t.Fatalf("invoke_plan: %v", err)
	}
	if !res.IsError {
		t.Fatalf("ambiguous server should be an error result")
	}
	structured, _ := json.Marshal(res.StructuredContent)
	var env struct {
		Error *struct {
			Details struct {
				Kind       string `json:"kind"`
				Candidates []struct {
					Server  string `json:"server"`
					Project string `json:"project"`
					Address string `json:"address"`
				} `json:"candidates"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(structured, &env); err != nil {
		t.Fatalf("structuredContent: %v", err)
	}
	if env.Error == nil || env.Error.Details.Kind != "ENDPOINT_NOT_FOUND" {
		t.Fatalf("expected ENDPOINT_NOT_FOUND: %s", structured)
	}
	got := map[string]string{}
	for _, c := range env.Error.Details.Candidates {
		got[c.Server] = c.Address
	}
	if got["user-a"] != "127.0.0.1:12200" || got["user-b"] != "127.0.0.1:12300" {
		t.Errorf("error.details.candidates must name both servers with addresses, got %s", structured)
	}
}

// TestInvokeWorkflowPromptListed pins the Sprint 3 prompt: the server advertises the
// prompts capability and the sofarpc.invoke_workflow template with a required `intent`.
func TestInvokeWorkflowPromptListed(t *testing.T) {
	cs := connectSDK(t, true)
	res, err := cs.ListPrompts(context.Background(), nil)
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}
	var found *mcpsdk.Prompt
	for _, p := range res.Prompts {
		if p.Name == "sofarpc.invoke_workflow" {
			found = p
		}
	}
	if found == nil {
		t.Fatalf("sofarpc.invoke_workflow not listed: %+v", res.Prompts)
	}
	var intent *mcpsdk.PromptArgument
	for _, a := range found.Arguments {
		if a.Name == "intent" {
			intent = a
		}
	}
	if intent == nil || !intent.Required {
		t.Errorf("intent must be a required prompt argument, got %+v", found.Arguments)
	}
}

// TestInvokeWorkflowPromptGet pins that prompts/get returns a user-role workflow message
// templated with the caller's intent and naming the recommended tools + failure path.
func TestInvokeWorkflowPromptGet(t *testing.T) {
	cs := connectSDK(t, true)
	res, err := cs.GetPrompt(context.Background(), &mcpsdk.GetPromptParams{
		Name:      "sofarpc.invoke_workflow",
		Arguments: map[string]string{"intent": "look up user u001", "server": "user-test"},
	})
	if err != nil {
		t.Fatalf("get prompt: %v", err)
	}
	if len(res.Messages) == 0 {
		t.Fatalf("prompt returned no messages")
	}
	var text string
	for _, m := range res.Messages {
		if tc, ok := m.Content.(*mcpsdk.TextContent); ok {
			text += tc.Text
		}
	}
	for _, want := range []string{"look up user u001", "sofarpc_resolve", "sofarpc_invoke_plan", "nextTool"} {
		if !strings.Contains(text, want) {
			t.Errorf("workflow message missing %q; got:\n%s", want, text)
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

// TestNoToolLeaksAttachmentValue is the end-to-end redaction net: drive every tool
// that can emit a configured server/endpoint and assert none surfaces the sentinel
// attachment value (ported from the retired tools/view_test.go server-path cases).
func TestNoToolLeaksAttachmentValue(t *testing.T) {
	const sentinelKey, sentinelValue = "_sofa_token", "SENTINEL_ATTACHMENT_VALUE_mcp"
	t.Setenv("SOFARPC_HOME", t.TempDir())
	path, err := appconfig.DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	lock, err := appconfig.DefaultLockPath()
	if err != nil {
		t.Fatalf("lock path: %v", err)
	}
	if _, err := appconfig.Update(path, lock, func(c *appconfig.Config) error {
		if _, err := c.AddProject("user", t.TempDir(), nil, false); err != nil {
			return err
		}
		_, err := c.AddServer("user-test", appconfig.Server{
			Address:     "127.0.0.1:12200",
			Project:     "user",
			Attachments: map[string]string{sentinelKey: sentinelValue},
		}, false)
		return err
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cs := connectSDK(t, true)
	ctx := context.Background()
	for _, params := range []*mcpsdk.CallToolParams{
		{Name: "sofarpc_resolve", Arguments: map[string]any{"server": "user-test"}},
		{Name: "sofarpc_resolve", Arguments: map[string]any{"project": "user"}},
		{Name: "sofarpc_config_list"},
		{Name: "sofarpc_doctor", Arguments: map[string]any{"server": "user-test"}},
	} {
		res, err := cs.CallTool(ctx, params)
		if err != nil {
			t.Fatalf("%s: %v", params.Name, err)
		}
		body, _ := json.Marshal(res.StructuredContent)
		if strings.Contains(string(body), sentinelValue) {
			t.Errorf("%s leaked attachment value: %s", params.Name, body)
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
