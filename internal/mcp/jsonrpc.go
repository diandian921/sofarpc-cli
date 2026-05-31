package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"runtime/debug"
	"sync/atomic"

	"github.com/diandian921/sofarpc-cli/internal/app"
	"github.com/diandian921/sofarpc-cli/internal/mcp/proto"
)

var panicCounter uint64

// handleWithRecover runs fn and converts a panic into a JSON-RPC internal error.
// The client only ever sees a fixed "internal error" message plus an errorId;
// the panic value and stack go to stderr under that id, so nothing sensitive
// (paths, payloads, stack frames) leaks to the agent.
func handleWithRecover(req proto.Request, stderr io.Writer, fn func() (proto.Response, bool)) (resp proto.Response, shouldReply bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			errorID := app.NewRequestID("panic")
			if stderr != nil {
				fmt.Fprintf(stderr, "mcp panic [%s]: %v\n%s\n", errorID, recovered, debug.Stack())
			}
			atomic.AddUint64(&panicCounter, 1)
			if req.IsNotification() {
				resp = proto.Response{}
				shouldReply = false
				return
			}
			data, _ := json.Marshal(map[string]string{"errorId": errorID})
			resp = proto.Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &proto.Error{
					Code:    proto.CodeInternalError,
					Message: "internal error",
					Data:    data,
				},
			}
			shouldReply = true
		}
	}()
	return fn()
}

func decodeJSON(raw []byte, out interface{}) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(out)
}
