package proto

import "encoding/json"

// progressTokenFromParams extracts params._meta.progressToken, returning false
// when the client did not request progress (progress must not be sent then).
func progressTokenFromParams(params json.RawMessage) (json.RawMessage, bool) {
	var p struct {
		Meta struct {
			ProgressToken json.RawMessage `json:"progressToken"`
		} `json:"_meta"`
	}
	if err := DecodeParams(params, &p); err != nil {
		return nil, false
	}
	if len(p.Meta.ProgressToken) == 0 {
		return nil, false
	}
	return p.Meta.ProgressToken, true
}

// SendProgress emits a notifications/progress for token. It is a no-op when the
// token is empty (the client did not opt into progress).
func (s *Session) SendProgress(token json.RawMessage, progress float64, message string) {
	if len(token) == 0 {
		return
	}
	params := map[string]interface{}{"progressToken": token, "progress": progress}
	if message != "" {
		params["message"] = message
	}
	_ = s.transport.Write(outNotification{JSONRPC: "2.0", Method: "notifications/progress", Params: params})
}
