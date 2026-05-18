package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/sofarpc/cli/internal/appconfig"
)

func handleWithRecover(req request, fn func() (response, bool)) (resp response, shouldReply bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if req.isNotification() {
				resp = response{}
				shouldReply = false
				return
			}
			resp = response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &rpcError{
					Code:    -32603,
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

func write(w io.Writer, resp response) error {
	body, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	_, err = w.Write(body)
	return err
}

func decodeJSON(raw []byte, out interface{}) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(out)
}
