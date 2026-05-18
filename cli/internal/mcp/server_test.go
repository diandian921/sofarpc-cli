package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sofarpc/cli/internal/schema"
)

type frameWriter struct {
	mu     sync.Mutex
	frames []string
	ch     chan string
}

func newFrameWriter() *frameWriter {
	return &frameWriter{ch: make(chan string, 32)}
}

func (w *frameWriter) Write(p []byte) (int, error) {
	frame := string(append([]byte(nil), p...))
	w.mu.Lock()
	w.frames = append(w.frames, frame)
	w.mu.Unlock()
	w.ch <- frame
	return len(p), nil
}

func TestToolsListRegistersWorkflowTools(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	out := &bytes.Buffer{}
	s := &Server{
		BuildVersion:       "test",
		Stdin:              strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n"),
		Stdout:             out,
		Stderr:             &bytes.Buffer{},
		DisableConfigWrite: true,
	}
	if code := s.Run(); code != 0 {
		t.Fatalf("Run exit = %d", code)
	}
	var resp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}
	names := make([]string, 0, len(resp.Result.Tools))
	for _, tool := range resp.Result.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	want := []string{
		"sofarpc_config",
		"sofarpc_describe",
		"sofarpc_doctor",
		"sofarpc_invoke",
		"sofarpc_probe",
		"sofarpc_resolve",
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("tools = %v, want %v", names, want)
	}
	for _, legacy := range []string{"add" + "_project", "list" + "_projects", "invoke" + "_method", "ping" + "_service"} {
		if strings.Contains(out.String(), legacy) {
			t.Fatalf("legacy tool %q should not be listed: %s", legacy, out.String())
		}
	}
}

func TestNotificationsDoNotReply(t *testing.T) {
	out := &bytes.Buffer{}
	s := &Server{
		BuildVersion: "test",
		Stdin:        strings.NewReader(`{"jsonrpc":"2.0","method":"unknown/notification","params":{}}` + "\n"),
		Stdout:       out,
		Stderr:       &bytes.Buffer{},
	}
	if code := s.Run(); code != 0 {
		t.Fatalf("Run exit = %d", code)
	}
	if out.Len() != 0 {
		t.Fatalf("notification should not produce a response: %s", out.String())
	}
}

func TestRunAcceptsLongJSONRPCLine(t *testing.T) {
	out := &bytes.Buffer{}
	large := strings.Repeat("x", 17*1024*1024)
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"blob":"` + large + `"}}` + "\n"
	s := &Server{
		BuildVersion: "test",
		Stdin:        strings.NewReader(input),
		Stdout:       out,
		Stderr:       &bytes.Buffer{},
	}
	if code := s.Run(); code != 0 {
		t.Fatalf("Run exit = %d", code)
	}
	if !strings.Contains(out.String(), `"tools"`) {
		t.Fatalf("tools/list response missing: %s", out.String())
	}
}

func TestReadLineLimitedRejectsAndResyncs(t *testing.T) {
	reader := bufio.NewReaderSize(strings.NewReader(strings.Repeat("x", 12)+"\n{}\n"), 4)
	if _, err := readLineLimited(reader, 8); err == nil || !strings.Contains(err.Error(), "maximum line size") {
		t.Fatalf("first read err = %v, want line-too-long", err)
	}
	line, err := readLineLimited(reader, 8)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if string(line) != "{}\n" {
		t.Fatalf("second line = %q", line)
	}
}

func TestRunAsyncInvokeCanBeCancelledWhileToolsListResponds(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	addr, accepted, stop := hangingTCPServer(t)
	defer stop()

	stdinR, stdinW := io.Pipe()
	stdout := newFrameWriter()
	stderr := &bytes.Buffer{}
	s := &Server{BuildVersion: "test", Stdin: stdinR, Stdout: stdout, Stderr: stderr}
	done := make(chan int, 1)
	go func() {
		done <- s.Run()
	}()
	writeFrame := func(line string) {
		t.Helper()
		if _, err := stdinW.Write([]byte(line + "\n")); err != nil {
			t.Fatalf("write stdin: %v", err)
		}
	}

	writeFrame(`{"jsonrpc":"2.0","id":"save-project","method":"tools/call","params":{"name":"sofarpc_config","arguments":{"action":"save_project","name":"user","workspaceRoot":"` + workspace + `"}}}`)
	waitResponseID(t, stdout.ch, "save-project", 2*time.Second)
	writeFrame(`{"jsonrpc":"2.0","id":"save-server","method":"tools/call","params":{"name":"sofarpc_config","arguments":{"action":"save_server","name":"user-test","address":"` + addr + `","project":"user"}}}`)
	waitResponseID(t, stdout.ch, "save-server", 2*time.Second)

	writeFrame(`{"jsonrpc":"2.0","id":"invoke-1","method":"tools/call","params":{"name":"sofarpc_invoke","arguments":{"server":"user-test","service":"com.example.UserService","method":"getUser","paramTypes":["java.lang.String"],"args":["u001"],"timeoutMs":20000}}}`)
	select {
	case <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatalf("invoke did not reach hanging server")
	}

	writeFrame(`{"jsonrpc":"2.0","id":"list-while-invoke","method":"tools/list","params":{}}`)
	list := waitResponseID(t, stdout.ch, "list-while-invoke", 2*time.Second)
	if _, ok := list["result"]; !ok {
		t.Fatalf("tools/list response missing result: %#v", list)
	}

	writeFrame(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"invoke-1","reason":"test"}}`)
	invoke := waitResponseID(t, stdout.ch, "invoke-1", 2*time.Second)
	result, _ := invoke["result"].(map[string]interface{})
	body, _ := json.Marshal(result)
	if !strings.Contains(string(body), "context canceled") {
		t.Fatalf("invoke response was not cancelled: %s", body)
	}

	if err := stdinW.Close(); err != nil {
		t.Fatalf("close stdin: %v", err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("Run exit = %d stderr=%s", code, stderr.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Run did not exit after stdin close")
	}
	assertFramesAreJSON(t, stdout.frames)
}

func TestInitializeEchoesClientProtocolVersion(t *testing.T) {
	out := &bytes.Buffer{}
	s := &Server{
		BuildVersion: "test",
		Stdin:        strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n"),
		Stdout:       out,
		Stderr:       &bytes.Buffer{},
	}
	if code := s.Run(); code != 0 {
		t.Fatalf("Run exit = %d", code)
	}
	if !strings.Contains(out.String(), `"protocolVersion":"2025-06-18"`) {
		t.Fatalf("initialize response did not echo protocol version: %s", out.String())
	}
}

func TestHandleWithRecoverReturnsInternalError(t *testing.T) {
	req := request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/list"}
	resp, shouldReply := handleWithRecover(req, func() (response, bool) {
		panic("boom")
	})
	if !shouldReply {
		t.Fatalf("expected panic response")
	}
	if resp.Error == nil || resp.Error.Code != -32603 || !strings.Contains(resp.Error.Message, "boom") {
		t.Fatalf("unexpected panic response: %+v", resp)
	}
}

func TestHandleWithRecoverSuppressesNotificationPanic(t *testing.T) {
	req := request{JSONRPC: "2.0", Method: "notifications/test"}
	_, shouldReply := handleWithRecover(req, func() (response, bool) {
		panic("boom")
	})
	if shouldReply {
		t.Fatalf("notification panic should not produce a response")
	}
}

func TestConfigWriteCanBeDisabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sofarpc_config","arguments":{"action":"save_project","name":"user","workspaceRoot":"` + workspace + `"}}}` + "\n"
	out := &bytes.Buffer{}
	s := &Server{BuildVersion: "test", Stdin: strings.NewReader(input), Stdout: out, Stderr: &bytes.Buffer{}, DisableConfigWrite: true}
	if code := s.Run(); code != 0 {
		t.Fatalf("Run exit = %d", code)
	}
	if !strings.Contains(out.String(), "config write tools are disabled") {
		t.Fatalf("expected disabled write error: %s", out.String())
	}
}

func TestConfigSaveAndListProjectTool(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sofarpc_config","arguments":{"action":"save_project","name":"user","workspaceRoot":"` + workspace + `","servicePrefixes":["com.example"]}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"sofarpc_config","arguments":{"action":"list"}}}`,
		"",
	}, "\n")
	out := &bytes.Buffer{}
	s := &Server{BuildVersion: "test", Stdin: strings.NewReader(input), Stdout: out, Stderr: &bytes.Buffer{}}
	if code := s.Run(); code != 0 {
		t.Fatalf("Run exit = %d", code)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 responses, got %d: %s", len(lines), out.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(lines[1]), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	body, _ := json.Marshal(resp["result"])
	if !strings.Contains(string(body), `"name":"user"`) {
		t.Fatalf("list response missing project: %s", string(body))
	}
}

func TestResolveAndInvokeDryRunUseWorkflowTools(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sofarpc_config","arguments":{"action":"save_project","name":"user","workspaceRoot":"` + workspace + `"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"sofarpc_config","arguments":{"action":"save_server","name":"user-test","address":"127.0.0.1:12200","project":"user"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"sofarpc_resolve","arguments":{"server":"user-test"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"sofarpc_invoke","arguments":{"server":"user-test","service":"com.example.UserService","method":"getUser","paramTypes":["java.lang.String"],"args":["u001"],"dryRun":true}}}`,
		"",
	}, "\n")
	out := &bytes.Buffer{}
	s := &Server{BuildVersion: "test", Stdin: strings.NewReader(input), Stdout: out, Stderr: &bytes.Buffer{}}
	if code := s.Run(); code != 0 {
		t.Fatalf("Run exit = %d", code)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 responses, got %d: %s", len(lines), out.String())
	}
	if !strings.Contains(lines[2], `"endpoint"`) || !strings.Contains(lines[2], `"user-test"`) {
		t.Fatalf("resolve response missing endpoint: %s", lines[2])
	}
	if !strings.Contains(lines[3], `"dryRun":true`) || !strings.Contains(lines[3], `"argTypes":["java.lang.String"]`) {
		t.Fatalf("dry run response missing plan: %s", lines[3])
	}
}

func TestRPCParamTypeForMethodExpandsImportedDTO(t *testing.T) {
	method := schema.Method{
		Package: "com.example.facade",
		Imports: map[string]string{
			"UserRequest": "com.example.model.UserRequest",
		},
		Parameters: []schema.Parameter{{Name: "request", Type: "UserRequest"}},
	}

	if got := rpcParamTypeForMethod("UserRequest", method); got != "com.example.model.UserRequest" {
		t.Fatalf("rpcParamTypeForMethod imported DTO = %q", got)
	}
	if got := rpcParamTypeForMethod("SamePackageRequest", method); got != "com.example.facade.SamePackageRequest" {
		t.Fatalf("rpcParamTypeForMethod same package DTO = %q", got)
	}
	if got := rpcParamTypeForMethod("Long", method); got != "java.lang.Long" {
		t.Fatalf("rpcParamTypeForMethod Long = %q", got)
	}
	if !sameParamTypes(method, []string{"com.example.model.UserRequest"}) {
		t.Fatalf("sameParamTypes should match FQN parameter")
	}
}

func TestMethodSignaturesIncludesOverloadCandidates(t *testing.T) {
	methods := []schema.Method{
		{
			Package:    "com.example",
			Method:     "query",
			Parameters: []schema.Parameter{{Name: "id", Type: "String"}},
		},
		{
			Package:    "com.example",
			Method:     "query",
			Parameters: []schema.Parameter{{Name: "request", Type: "QueryRequest"}},
		},
	}
	got := methodSignatures(methods)
	if !strings.Contains(got, "query(java.lang.String id)") || !strings.Contains(got, "query(com.example.QueryRequest request)") {
		t.Fatalf("signatures = %q", got)
	}
}

func TestAnnotateArgumentForParamAddsDTOFieldTypes(t *testing.T) {
	method := schema.Method{
		Package: "com.example.api",
		Imports: map[string]string{
			"UserRequest": "com.example.model.UserRequest",
		},
		Parameters: []schema.Parameter{{Name: "request", Type: "UserRequest"}},
	}
	desc := schema.Description{Types: map[string]schema.TypeSchema{
		"com.example.model.UserRequest": {
			Type: "com.example.model.UserRequest",
			Kind: "class",
			Fields: []schema.Field{
				{Name: "id", Type: "Long"},
				{Name: "ratio", Type: "Double"},
			},
		},
	}}
	annotated := annotateArgumentForParam(map[string]interface{}{"id": json.Number("5"), "ratio": json.Number("2.0")}, "UserRequest", method, desc)
	m, ok := annotated.(map[string]interface{})
	if !ok {
		t.Fatalf("annotated type = %T", annotated)
	}
	if m["@type"] != "com.example.model.UserRequest" {
		t.Fatalf("@type = %#v", m["@type"])
	}
	fieldTypes, ok := m["__fieldTypes"].(map[string]string)
	if !ok {
		t.Fatalf("__fieldTypes = %#v", m["__fieldTypes"])
	}
	if fieldTypes["id"] != "java.lang.Long" || fieldTypes["ratio"] != "java.lang.Double" {
		t.Fatalf("fieldTypes = %#v", fieldTypes)
	}
}

func TestAnnotateValueForJavaTypeHasDepthGuard(t *testing.T) {
	types := map[string]schema.TypeSchema{
		"com.example.Node": {
			Type:   "com.example.Node",
			Kind:   "class",
			Fields: []schema.Field{{Name: "next", Type: "Node"}},
		},
	}
	root := map[string]interface{}{}
	current := root
	for i := 0; i < maxTypeAnnotationDepth+16; i++ {
		next := map[string]interface{}{}
		current["next"] = next
		current = next
	}
	got := annotateValueForJavaType(root, "com.example.Node", types)
	if _, ok := got.(map[string]interface{}); !ok {
		t.Fatalf("annotated type = %T", got)
	}
}

func TestDecodeJSONPreservesLargeNumbers(t *testing.T) {
	var payload struct {
		Arguments map[string]interface{} `json:"arguments"`
	}
	err := decodeJSON([]byte(`{"arguments":{"mpCode":433905635109773312}}`), &payload)
	if err != nil {
		t.Fatalf("decodeJSON: %v", err)
	}
	n, ok := payload.Arguments["mpCode"].(json.Number)
	if !ok {
		t.Fatalf("mpCode type = %T, want json.Number", payload.Arguments["mpCode"])
	}
	if n.String() != "433905635109773312" {
		t.Fatalf("mpCode = %s", n.String())
	}
}

func waitResponseID(t *testing.T, ch <-chan string, want string, timeout time.Duration) map[string]interface{} {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case frame := <-ch:
			var resp map[string]interface{}
			if err := json.Unmarshal([]byte(frame), &resp); err != nil {
				t.Fatalf("bad JSON-RPC frame %q: %v", frame, err)
			}
			if resp["id"] == want {
				return resp
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for response id %q", want)
		}
	}
}

func assertFramesAreJSON(t *testing.T, frames []string) {
	t.Helper()
	for i, frame := range frames {
		var resp map[string]interface{}
		if err := json.Unmarshal([]byte(frame), &resp); err != nil {
			t.Fatalf("frame %d is not a complete JSON object: %q: %v", i, frame, err)
		}
		if !strings.HasSuffix(frame, "\n") {
			t.Fatalf("frame %d missing newline terminator: %q", i, frame)
		}
	}
}

func hangingTCPServer(t *testing.T) (addr string, accepted <-chan struct{}, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	acceptedCh := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		close(acceptedCh)
		_, _ = io.Copy(io.Discard, conn)
		_ = conn.Close()
	}()
	stop = func() {
		_ = ln.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("hangingTCPServer did not stop")
		}
	}
	return ln.Addr().String(), acceptedCh, stop
}
