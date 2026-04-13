package protocol

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// NewRequest builds a Request envelope, serialising payload to RawMessage.
func NewRequest(op string, payload interface{}) (Request, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Request{}, fmt.Errorf("marshal payload: %w", err)
	}
	return Request{
		RequestID: NewRequestID(op),
		Op:        op,
		Payload:   raw,
	}, nil
}

// NewRequestID returns a short, unique-ish identifier prefixed with the op.
func NewRequestID(op string) string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return op + "-fallback"
	}
	return op + "-" + hex.EncodeToString(buf[:])
}
