package proto

import (
	"bytes"
	"encoding/json"
	"math"
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
// integer. A JSON number is accepted only when it is integral and within the safe
// integer range; fractional numbers and any other JSON type are rejected so a
// bad token is ignored instead of breaking later progress correlation.
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
		if _, err := t.Int64(); err == nil {
			return true
		}
		f, err := t.Float64()
		if err != nil {
			return false
		}
		return f == math.Trunc(f) && f >= minSafeInteger && f <= maxSafeInteger
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
