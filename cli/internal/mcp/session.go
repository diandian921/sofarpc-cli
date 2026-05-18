package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

type session struct {
	server *Server
	reader *bufio.Reader
	out    io.Writer
	stderr io.Writer

	outMu     sync.Mutex
	wg        sync.WaitGroup
	running   map[string]context.CancelFunc
	runningMu sync.Mutex
}

func newSession(server *Server, in io.Reader, out io.Writer, stderr io.Writer) *session {
	return &session{
		server:  server,
		reader:  bufio.NewReaderSize(in, 64*1024),
		out:     out,
		stderr:  stderr,
		running: map[string]context.CancelFunc{},
	}
}

func (s *session) run() int {
	for {
		line, err := readLineLimited(s.reader, maxJSONRPCLineBytes)
		if err != nil {
			if err == io.EOF {
				s.wg.Wait()
				return 0
			}
			if err == errJSONRPCLineTooLong {
				s.write(response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: err.Error()}})
				continue
			}
			fmt.Fprintln(s.stderr, "mcp:", err)
			s.wg.Wait()
			return 1
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var req request
		if err := decodeJSON(line, &req); err != nil {
			s.write(response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: err.Error()}})
			continue
		}
		if req.Method == "tools/call" {
			var call toolCallParams
			if err := decodeJSON(req.Params, &call); err == nil {
				req.toolCall = &call
			}
		}
		s.dispatch(req)
	}
}

func (s *session) dispatch(req request) {
	if req.Method == "notifications/cancelled" {
		s.cancel(req.Params)
		return
	}
	run := func(ctx context.Context) {
		resp, shouldReply := handleWithRecover(req, func() (response, bool) {
			return s.server.handle(ctx, req)
		})
		if shouldReply && !req.isNotification() {
			s.write(resp)
		}
	}
	if shouldRunAsync(req) {
		ctx, cancel := context.WithCancel(context.Background())
		key := requestIDKey(req.ID)
		if key != "" {
			s.runningMu.Lock()
			s.running[key] = cancel
			s.runningMu.Unlock()
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer cancel()
			defer func() {
				if key != "" {
					s.runningMu.Lock()
					delete(s.running, key)
					s.runningMu.Unlock()
				}
			}()
			run(ctx)
		}()
		return
	}
	run(context.Background())
}

func (s *session) cancel(params json.RawMessage) {
	var payload struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if err := decodeJSON(params, &payload); err != nil || len(payload.RequestID) == 0 {
		return
	}
	key := requestIDKey(payload.RequestID)
	s.runningMu.Lock()
	cancel := s.running[key]
	s.runningMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *session) write(resp response) {
	s.outMu.Lock()
	defer s.outMu.Unlock()
	_ = write(s.out, resp)
}
