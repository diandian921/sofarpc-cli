// Package proto is layer 1 of the MCP server: pure JSON-RPC 2.0 over stdio plus
// the session protocol (lifecycle, cancellation, progress, logging). It knows
// nothing about SofaRPC, tools, or the app layer and imports only the standard
// library.
package proto

import (
	"bytes"
	"encoding/json"
	"strings"
)

// JSON-RPC 2.0 + MCP reserved error codes.
const (
	CodeParseError           = -32700
	CodeInvalidRequest       = -32600
	CodeMethodNotFound       = -32601
	CodeInvalidParams        = -32602
	CodeInternalError        = -32603
	CodeServerNotInitialized = -32002
	CodeShuttingDown         = -32000
)

// Error is a JSON-RPC error object. Data is optional structured detail.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Request is an inbound JSON-RPC request or notification (notification has no id).
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// IsNotification reports whether the request carries no id (no response is due).
func (r Request) IsNotification() bool {
	return len(bytes.TrimSpace(r.ID)) == 0
}

// Response is an outbound JSON-RPC response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// outNotification is an outbound JSON-RPC notification (progress / logging).
type outNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// Decode strictly parses a single JSON-RPC request frame: it must be exactly one
// JSON value (trailing tokens are a parse error), carry jsonrpc == "2.0", and a
// non-empty method. The partially-decoded Request is returned alongside the
// error so the caller can echo the id when one was present.
func Decode(line []byte) (Request, *Error) {
	dec := json.NewDecoder(bytes.NewReader(line))
	var req Request
	if err := dec.Decode(&req); err != nil {
		return Request{}, &Error{Code: CodeParseError, Message: "parse error"}
	}
	if dec.More() {
		return req, &Error{Code: CodeParseError, Message: "parse error: trailing data after JSON value"}
	}
	if req.JSONRPC != "2.0" {
		return req, &Error{Code: CodeInvalidRequest, Message: `invalid request: jsonrpc must be "2.0"`}
	}
	if strings.TrimSpace(req.Method) == "" {
		return req, &Error{Code: CodeInvalidRequest, Message: "invalid request: method is required"}
	}
	return req, nil
}

// DecodeParams unmarshals a params/arguments payload while preserving large
// integers as json.Number (so 64-bit ids survive the float64 round-trip).
func DecodeParams(raw []byte, out interface{}) error {
	if len(raw) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(out)
}

// errorResponse builds a JSON-RPC error response for the given id.
func errorResponse(id json.RawMessage, code int, message string) Response {
	return Response{JSONRPC: "2.0", ID: id, Error: &Error{Code: code, Message: message}}
}
