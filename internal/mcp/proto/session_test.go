package proto

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockDispatcher answers tools/list immediately and blocks "slow" until ctx done.
type mockDispatcher struct {
	started chan struct{}
}

func (m *mockDispatcher) Async(req Request) bool { return req.Method == "slow" }

func (m *mockDispatcher) Handle(ctx context.Context, req Request) (Response, bool) {
	base := Response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "tools/list":
		base.Result = map[string]interface{}{"tools": []string{}}
		return base, true
	case "slow":
		if m.started != nil {
			close(m.started)
		}
		<-ctx.Done()
		base.Error = &Error{Code: CodeInternalError, Message: "ctx done"}
		return base, true
	default:
		base.Error = &Error{Code: CodeMethodNotFound, Message: "nope"}
		return base, true
	}
}

func newTestSession(in io.Reader, out io.Writer) *Session {
	return NewSession(Config{
		In: in, Out: out, Stderr: io.Discard,
		Info:         ServerInfo{Name: "test", Version: "test"},
		Capabilities: Capabilities{Tools: &ToolsCapability{}, Logging: &struct{}{}},
		Dispatcher:   &mockDispatcher{},
	})
}

func handshakeFrames() string {
	return `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"
}

func TestSessionRejectsCallBeforeInitialize(t *testing.T) {
	out := &bytes.Buffer{}
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n")
	if code := newTestSession(in, out).Run(); code != 0 {
		t.Fatalf("Run = %d", code)
	}
	if !strings.Contains(out.String(), `"code":-32002`) {
		t.Fatalf("call before initialize must be -32002: %s", out.String())
	}
}

func TestSessionNegotiatesAndDispatchesAfterHandshake(t *testing.T) {
	out := &bytes.Buffer{}
	in := strings.NewReader(handshakeFrames() + `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n")
	if code := newTestSession(in, out).Run(); code != 0 {
		t.Fatalf("Run = %d", code)
	}
	if !strings.Contains(out.String(), `"protocolVersion":"2025-06-18"`) {
		t.Fatalf("missing negotiated version: %s", out.String())
	}
	if !strings.Contains(out.String(), `"tools"`) {
		t.Fatalf("missing tools/list result: %s", out.String())
	}
}

// frameChan collects each Write as a separate frame for interactive assertions.
type frameChan struct {
	mu sync.Mutex
	ch chan string
}

func (w *frameChan) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ch <- string(append([]byte(nil), p...))
	return len(p), nil
}

func TestSessionCancelledCallSendsNoResponse(t *testing.T) {
	started := make(chan struct{})
	stdinR, stdinW := io.Pipe()
	out := &frameChan{ch: make(chan string, 32)}
	s := NewSession(Config{
		In: stdinR, Out: out, Stderr: io.Discard,
		Info:         ServerInfo{Name: "test", Version: "test"},
		Capabilities: Capabilities{Tools: &ToolsCapability{}},
		Dispatcher:   &mockDispatcher{started: started},
	})
	done := make(chan int, 1)
	go func() { done <- s.Run() }()

	write := func(line string) {
		if _, err := stdinW.Write([]byte(line + "\n")); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	write(`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	write(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	write(`{"jsonrpc":"2.0","id":"slow-1","method":"slow","params":{}}`)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("slow handler never started")
	}
	write(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"slow-1"}}`)
	// Give the cancelled handler time to return and (wrongly) try to respond.
	time.Sleep(100 * time.Millisecond)
	_ = stdinW.Close()
	<-done

	for {
		select {
		case frame := <-out.ch:
			var resp map[string]interface{}
			if err := json.Unmarshal([]byte(frame), &resp); err != nil {
				continue
			}
			if resp["id"] == "slow-1" {
				t.Fatalf("cancelled request must not produce a response: %s", frame)
			}
		default:
			return
		}
	}
}
