package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/diandian921/sofarpc-cli/internal/mcp/proto"
)

// handleWithRecover runs fn and converts a panic into a JSON-RPC internal error
// (suppressed for notifications, which never receive a response).
func handleWithRecover(req proto.Request, fn func() (proto.Response, bool)) (resp proto.Response, shouldReply bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if req.IsNotification() {
				resp = proto.Response{}
				shouldReply = false
				return
			}
			resp = proto.Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &proto.Error{
					Code:    proto.CodeInternalError,
					Message: fmt.Sprintf("internal error: %v", recovered),
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
