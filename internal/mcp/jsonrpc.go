package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/diandian921/sofarpc-cli/internal/app"
	"github.com/diandian921/sofarpc-cli/internal/appconfig"
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

func toolOK(summary string, data interface{}) toolResult {
	return toolResult{Content: []content{{Type: "text", Text: summary}}, StructuredContent: data}
}

// toolErrRendered emits an MCP error result whose StructuredContent is the
// single app.Result rendering — same shape as success and exec-failure paths,
// so the agent always reads ok / code / error.nextTool from one contract.
func toolErrRendered(resp app.Result) toolResult {
	message := ""
	if resp.Error != nil {
		message = resp.Error.Message
	}
	return toolResult{
		Content:           []content{{Type: "text", Text: message}},
		StructuredContent: resp,
		IsError:           !resp.OK,
	}
}

func toolErr(summary string, err error) toolResult {
	data := map[string]interface{}{"ok": false, "message": summary}
	if err != nil {
		data["error"] = err.Error()
		var cfgErr *appconfig.ConfigError
		if errors.As(err, &cfgErr) {
			data["code"] = cfgErr.Code
			data["configPath"] = cfgErr.Path
		}
	}
	return toolResult{Content: []content{{Type: "text", Text: summary}}, StructuredContent: data, IsError: true}
}

func decodeJSON(raw []byte, out interface{}) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(out)
}
