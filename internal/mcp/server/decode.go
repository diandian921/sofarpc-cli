package server

import (
	"bytes"
	"encoding/json"

	"github.com/diandian921/sofarpc-mcp/internal/mcp/proto"
)

// decodeArgs strictly decodes raw tool arguments into out: unknown fields and
// type mismatches (e.g. the string "true" into a bool) are rejected as
// InvalidParams, and large integers are preserved as json.Number. Server-side
// aliases stay decodable by being declared struct fields that the input schema
// simply does not advertise.
func decodeArgs(raw json.RawMessage, out interface{}) *proto.Error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return &proto.Error{Code: proto.CodeInvalidParams, Message: "invalid arguments: " + err.Error()}
	}
	return nil
}
