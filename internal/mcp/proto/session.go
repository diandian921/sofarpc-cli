package proto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// Dispatcher handles ready-state methods. The session owns framing, lifecycle,
// cancellation, and progress/logging; the dispatcher owns method semantics
// (tools/list, tools/call, ...) and decides which calls run asynchronously.
type Dispatcher interface {
	Handle(ctx context.Context, req Request) (Response, bool)
	Async(req Request) bool
}

// Config wires a session to its streams, identity, and dispatcher.
type Config struct {
	In           io.Reader
	Out          io.Writer
	Stderr       io.Writer
	Info         ServerInfo
	Capabilities Capabilities
	Instructions string
	Dispatcher   Dispatcher
}

// Session is the stateful stdio MCP protocol engine.
type Session struct {
	transport    *Transport
	stderr       io.Writer
	info         ServerInfo
	caps         Capabilities
	instructions string
	dispatcher   Dispatcher

	stateMu  sync.Mutex
	state    State
	inflight *inFlight
	wg       sync.WaitGroup
}

// NewSession builds a session from cfg.
func NewSession(cfg Config) *Session {
	stderr := cfg.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	return &Session{
		transport:    NewTransport(cfg.In, cfg.Out),
		stderr:       stderr,
		info:         cfg.Info,
		caps:         cfg.Capabilities,
		instructions: cfg.Instructions,
		dispatcher:   cfg.Dispatcher,
		state:        StateNew,
		inflight:     newInFlight(),
	}
}

// Run serves the stdio read loop until EOF, returning a process exit code.
func (s *Session) Run() int {
	for {
		frame, err := s.transport.ReadFrame()
		if err != nil {
			if err == io.EOF {
				s.setState(StateClosing)
				s.wg.Wait()
				return 0
			}
			if errors.Is(err, ErrFrameTooLong) {
				_ = s.transport.Write(errorResponse(nil, CodeInvalidRequest, err.Error()))
				continue
			}
			fmt.Fprintln(s.stderr, "mcp:", err)
			s.wg.Wait()
			return 1
		}
		req, derr := Decode(frame)
		if derr != nil {
			if len(frame) == 0 {
				continue
			}
			_ = s.transport.Write(errorResponse(req.ID, derr.Code, derr.Message))
			continue
		}
		s.handleRequest(req)
	}
}

func (s *Session) handleRequest(req Request) {
	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
		return
	case "notifications/initialized":
		s.transitionReady()
		return
	case "notifications/cancelled":
		s.handleCancel(req)
		return
	}
	if s.getState() != StateReady {
		if !req.IsNotification() {
			_ = s.transport.Write(errorResponse(req.ID, CodeServerNotInitialized, "server not initialized"))
		}
		return
	}
	s.dispatchReady(req)
}

func (s *Session) handleInitialize(req Request) {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = DecodeParams(req.Params, &p)
	version, verr := NegotiateVersion(p.ProtocolVersion)
	if verr != nil {
		_ = s.transport.Write(errorResponse(req.ID, verr.Code, verr.Message))
		return
	}
	s.setState(StateInitializing)
	_ = s.transport.Write(Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: initializeResult{
			ProtocolVersion: version,
			Capabilities:    s.caps,
			ServerInfo:      s.info,
			Instructions:    s.instructions,
		},
	})
}

func (s *Session) handleCancel(req Request) {
	var payload struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if err := DecodeParams(req.Params, &payload); err != nil || len(payload.RequestID) == 0 {
		return
	}
	s.inflight.cancel(payload.RequestID)
}

// dispatchReady runs a ready-state request, asynchronously when the dispatcher
// flags it so. Async runs register in the in-flight table; a cancelled run's
// final response is dropped.
func (s *Session) dispatchReady(req Request) {
	if s.dispatcher.Async(req) && !req.IsNotification() {
		ctx, cancel := context.WithCancel(s.requestContext(req))
		s.inflight.register(req.ID, req.Method, cancel)
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer cancel()
			defer s.inflight.unregister(req.ID, req.Method)
			resp, reply := s.dispatcher.Handle(ctx, req)
			if reply && !s.inflight.wasCancelled(req.ID, req.Method) {
				_ = s.transport.Write(resp)
			}
		}()
		return
	}
	resp, reply := s.dispatcher.Handle(s.requestContext(req), req)
	if reply && !req.IsNotification() {
		_ = s.transport.Write(resp)
	}
}

// requestContext binds the live session and (when present) the progress token,
// so a Runtime can emit progress/logging for this request.
func (s *Session) requestContext(req Request) context.Context {
	ctx := withSession(context.Background(), s)
	if token, ok := progressTokenFromParams(req.Params); ok {
		ctx = WithProgressToken(ctx, token)
	}
	return ctx
}

func (s *Session) setState(state State) {
	s.stateMu.Lock()
	s.state = state
	s.stateMu.Unlock()
}

func (s *Session) getState() State {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.state
}

// transitionReady advances to StateReady only from StateInitializing.
func (s *Session) transitionReady() {
	s.stateMu.Lock()
	if s.state == StateInitializing {
		s.state = StateReady
	}
	s.stateMu.Unlock()
}
