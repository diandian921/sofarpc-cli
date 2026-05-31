package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/diandian921/sofarpc-cli/internal/mcp/proto"
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

// handshake returns the initialize + initialized frames every session needs
// before tools/* are accepted (lifecycle gating).
func handshake() string {
	return `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"
}

// responsesByID indexes id'd JSON-RPC responses from a buffered run.
func responsesByID(t *testing.T, out string) map[string]map[string]interface{} {
	t.Helper()
	res := map[string]map[string]interface{}{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("bad frame %q: %v", line, err)
		}
		if id, ok := m["id"]; ok {
			res[fmt.Sprint(id)] = m
		}
	}
	return res
}

func TestToolsListRegistersWorkflowTools(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	out := &bytes.Buffer{}
	s := &Server{
		BuildVersion: "test",
		Stdin:        strings.NewReader(handshake() + `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n"),
		Stdout:       out,
		Stderr:       &bytes.Buffer{},
	}
	if code := s.Run(); code != 0 {
		t.Fatalf("Run exit = %d", code)
	}
	names := toolNamesFromList(t, out.String())
	want := []string{
		"sofarpc_config_list",
		"sofarpc_config_remove_project",
		"sofarpc_config_remove_server",
		"sofarpc_config_save_project",
		"sofarpc_config_save_server",
		"sofarpc_describe",
		"sofarpc_doctor",
		"sofarpc_invoke",
		"sofarpc_invoke_plan",
		"sofarpc_probe",
		"sofarpc_resolve",
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("tools = %v, want %v", names, want)
	}
}

func TestAllToolsAdvertiseOutputSchema(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	out := &bytes.Buffer{}
	s := &Server{
		BuildVersion: "test",
		Stdin:        strings.NewReader(handshake() + `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n"),
		Stdout:       out,
		Stderr:       &bytes.Buffer{},
	}
	if code := s.Run(); code != 0 {
		t.Fatalf("Run exit = %d", code)
	}
	resp := responsesByID(t, out.String())["1"]
	if resp == nil {
		t.Fatalf("no tools/list response: %s", out.String())
	}
	result, _ := resp["result"].(map[string]interface{})
	rawTools, _ := result["tools"].([]interface{})
	if len(rawTools) == 0 {
		t.Fatalf("tools/list returned no tools: %s", out.String())
	}
	for _, item := range rawTools {
		tool, _ := item.(map[string]interface{})
		if tool["outputSchema"] == nil {
			t.Fatalf("tool %q must advertise outputSchema: %s", tool["name"], out.String())
		}
	}
}

func TestDisableConfigWriteHidesWriteTools(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	out := &bytes.Buffer{}
	s := &Server{
		BuildVersion:       "test",
		Stdin:              strings.NewReader(handshake() + `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n"),
		Stdout:             out,
		Stderr:             &bytes.Buffer{},
		DisableConfigWrite: true,
	}
	if code := s.Run(); code != 0 {
		t.Fatalf("Run exit = %d", code)
	}
	names := toolNamesFromList(t, out.String())
	want := []string{
		"sofarpc_config_list",
		"sofarpc_describe",
		"sofarpc_doctor",
		"sofarpc_invoke",
		"sofarpc_invoke_plan",
		"sofarpc_probe",
		"sofarpc_resolve",
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("disabled tools = %v, want %v (no write tools)", names, want)
	}
}

func toolNamesFromList(t *testing.T, out string) []string {
	t.Helper()
	resp := responsesByID(t, out)["1"]
	if resp == nil {
		t.Fatalf("no tools/list response: %s", out)
	}
	result, _ := resp["result"].(map[string]interface{})
	rawTools, _ := result["tools"].([]interface{})
	names := make([]string, 0, len(rawTools))
	for _, item := range rawTools {
		tool, _ := item.(map[string]interface{})
		names = append(names, fmt.Sprint(tool["name"]))
	}
	sort.Strings(names)
	return names
}

func TestCallBeforeInitializeIsRejected(t *testing.T) {
	out := &bytes.Buffer{}
	s := &Server{
		BuildVersion: "test",
		Stdin:        strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n"),
		Stdout:       out,
		Stderr:       &bytes.Buffer{},
	}
	if code := s.Run(); code != 0 {
		t.Fatalf("Run exit = %d", code)
	}
	if !strings.Contains(out.String(), `"code":-32002`) {
		t.Fatalf("call before initialize must be rejected with -32002: %s", out.String())
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

func TestRunRejectsOversizedJSONRPCLine(t *testing.T) {
	out := &bytes.Buffer{}
	large := strings.Repeat("x", 17*1024*1024)
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"blob":"` + large + `"}}` + "\n" +
		handshake() + `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n"
	s := &Server{
		BuildVersion: "test",
		Stdin:        strings.NewReader(input),
		Stdout:       out,
		Stderr:       &bytes.Buffer{},
	}
	if code := s.Run(); code != 0 {
		t.Fatalf("Run exit = %d", code)
	}
	if !strings.Contains(out.String(), `"code":-32600`) {
		t.Fatalf("oversized frame must be rejected with -32600: %s", out.String()[:min(len(out.String()), 200)])
	}
	// The reader must resync: the following valid tools/list still responds.
	if responsesByID(t, out.String())["2"] == nil {
		t.Fatalf("reader did not resync after oversized frame")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestInitializeNegotiatesProtocolVersion(t *testing.T) {
	cases := []struct {
		name    string
		params  string
		want    string
		wantErr bool
	}{
		{"supported", `{"protocolVersion":"2025-06-18"}`, "2025-06-18", false},
		{"unsupported-degrades-to-latest", `{"protocolVersion":"1.0.0"}`, "2025-11-25", false},
		{"missing-is-invalid-params", `{}`, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			in := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":` + c.params + `}` + "\n"
			s := &Server{BuildVersion: "test", Stdin: strings.NewReader(in), Stdout: out, Stderr: &bytes.Buffer{}}
			if code := s.Run(); code != 0 {
				t.Fatalf("Run exit = %d", code)
			}
			if c.wantErr {
				if !strings.Contains(out.String(), `"code":-32602`) {
					t.Fatalf("missing protocolVersion must be -32602: %s", out.String())
				}
				return
			}
			if !strings.Contains(out.String(), `"protocolVersion":"`+c.want+`"`) {
				t.Fatalf("negotiated version wrong: %s", out.String())
			}
		})
	}
}

func TestHandleWithRecoverSanitizesPanic(t *testing.T) {
	req := proto.Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/list"}
	stderr := &bytes.Buffer{}
	resp, shouldReply := handleWithRecover(req, stderr, func() (proto.Response, bool) {
		panic("boom secret /etc/passwd")
	})
	if !shouldReply {
		t.Fatalf("expected panic response")
	}
	if resp.Error == nil || resp.Error.Code != proto.CodeInternalError {
		t.Fatalf("unexpected panic response: %+v", resp)
	}
	if resp.Error.Message != "internal error" {
		t.Fatalf("panic message must be the fixed string, got %q", resp.Error.Message)
	}
	if strings.Contains(resp.Error.Message, "boom") || strings.Contains(string(resp.Error.Data), "boom") {
		t.Fatalf("panic detail must not leak to the client: %+v", resp.Error)
	}
	if !strings.Contains(stderr.String(), "boom") {
		t.Fatalf("panic detail must be written to stderr: %s", stderr.String())
	}
}

func TestHandleWithRecoverSuppressesNotificationPanic(t *testing.T) {
	req := proto.Request{JSONRPC: "2.0", Method: "notifications/test"}
	_, shouldReply := handleWithRecover(req, nil, func() (proto.Response, bool) {
		panic("boom")
	})
	if shouldReply {
		t.Fatalf("notification panic should not produce a response")
	}
}

func TestConfigSaveAndListProjectTool(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	input := handshake() + strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sofarpc_config_save_project","arguments":{"name":"user","workspaceRoot":"` + workspace + `","servicePrefixes":["com.example"]}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"sofarpc_config_list","arguments":{}}}`,
		"",
	}, "\n")
	out := &bytes.Buffer{}
	s := &Server{BuildVersion: "test", Stdin: strings.NewReader(input), Stdout: out, Stderr: &bytes.Buffer{}}
	if code := s.Run(); code != 0 {
		t.Fatalf("Run exit = %d", code)
	}
	resp := responsesByID(t, out.String())["2"]
	if resp == nil {
		t.Fatalf("no list response: %s", out.String())
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

	input := handshake() + strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sofarpc_config_save_project","arguments":{"name":"user","workspaceRoot":"` + workspace + `"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"sofarpc_config_save_server","arguments":{"name":"user-test","address":"127.0.0.1:12200","project":"user"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"sofarpc_resolve","arguments":{"server":"user-test"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"sofarpc_invoke_plan","arguments":{"server":"user-test","service":"com.example.UserService","method":"getUser","paramTypes":["java.lang.String"],"args":["u001"]}}}`,
		"",
	}, "\n")
	out := &bytes.Buffer{}
	s := &Server{BuildVersion: "test", Stdin: strings.NewReader(input), Stdout: out, Stderr: &bytes.Buffer{}}
	if code := s.Run(); code != 0 {
		t.Fatalf("Run exit = %d", code)
	}
	byID := responsesByID(t, out.String())
	resolve, _ := json.Marshal(byID["3"])
	if !strings.Contains(string(resolve), `"endpoint"`) || !strings.Contains(string(resolve), `"user-test"`) {
		t.Fatalf("resolve response missing endpoint: %s", resolve)
	}
	dry, _ := json.Marshal(byID["4"])
	if !strings.Contains(string(dry), `"dryRun":true`) || !strings.Contains(string(dry), `"argTypes":["java.lang.String"]`) {
		t.Fatalf("invoke_plan response missing plan: %s", dry)
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

func TestCancelledInvokeSendsNoFinalResponse(t *testing.T) {
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

	writeFrame(`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	waitResponseID(t, stdout.ch, "0", 2*time.Second)
	writeFrame(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	writeFrame(`{"jsonrpc":"2.0","id":"save-project","method":"tools/call","params":{"name":"sofarpc_config_save_project","arguments":{"name":"user","workspaceRoot":"` + workspace + `"}}}`)
	waitResponseID(t, stdout.ch, "save-project", 2*time.Second)
	writeFrame(`{"jsonrpc":"2.0","id":"save-server","method":"tools/call","params":{"name":"sofarpc_config_save_server","arguments":{"name":"user-test","address":"` + addr + `","project":"user"}}}`)
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
	assertNoResponseID(t, stdout.ch, "invoke-1", 500*time.Millisecond)

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
			if fmt.Sprint(resp["id"]) == want {
				return resp
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for response id %q", want)
		}
	}
}

// assertNoResponseID drains frames for the window and fails if a response with
// the given id appears (a cancelled request must not produce a final response).
func assertNoResponseID(t *testing.T, ch <-chan string, id string, within time.Duration) {
	t.Helper()
	timer := time.NewTimer(within)
	defer timer.Stop()
	for {
		select {
		case frame := <-ch:
			var resp map[string]interface{}
			if err := json.Unmarshal([]byte(frame), &resp); err != nil {
				continue
			}
			if fmt.Sprint(resp["id"]) == id {
				t.Fatalf("cancelled request produced a response: %s", frame)
			}
		case <-timer.C:
			return
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
