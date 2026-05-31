package proto

import (
	"bytes"
	"encoding/json"
)

// JSON can represent integers exactly only within ±2^53; a progressToken number
// outside this range (or with a fractional part) is rejected rather than echoed
// back with silent precision loss.
const (
	maxSafeInteger = 1 << 53
	minSafeInteger = -(1 << 53)
)

// progressTokenFromParams extracts params._meta.progressToken, returning false
// when the client did not request progress (progress must not be sent then) or
// when the token is not a valid string/integer.
func progressTokenFromParams(params json.RawMessage) (json.RawMessage, bool) {
	var p struct {
		Meta struct {
			ProgressToken json.RawMessage `json:"progressToken"`
		} `json:"_meta"`
	}
	if err := DecodeParams(params, &p); err != nil {
		return nil, false
	}
	if len(p.Meta.ProgressToken) == 0 || !validProgressToken(p.Meta.ProgressToken) {
		return nil, false
	}
	return p.Meta.ProgressToken, true
}

// validProgressToken enforces the MCP rule that a progressToken is a string or an
// integer. A JSON number is accepted only as a canonical integer literal within
// ±2^53 (Int64 parses it exactly); fractional or exponent forms (e.g. 1.2, 2e3,
// 9007199254740993e0) are rejected rather than range-checked through a lossy
// Float64, and any other JSON type is rejected, so a bad token is ignored instead
// of breaking later progress correlation.
func validProgressToken(raw json.RawMessage) bool {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v interface{}
	if err := dec.Decode(&v); err != nil {
		return false
	}
	switch t := v.(type) {
	case string:
		return true
	case json.Number:
		i, err := t.Int64()
		return err == nil && i >= minSafeInteger && i <= maxSafeInteger
	default:
		return false
	}
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
