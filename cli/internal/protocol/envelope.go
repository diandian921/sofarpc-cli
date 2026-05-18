// Package protocol defines the JSON envelope used by CLI exec and direct invocation.
//
// The envelope must stay byte-compatible with protocol/schema/*.json. Tests under
// internal/protocol pin the contract by round-tripping golden fixtures.
package protocol

import "encoding/json"

const (
	OpInvoke = "invoke"
	OpPing   = "ping"
)

const (
	CodeSuccess         = "SUCCESS"
	CodeBadRequest      = "BAD_REQUEST"
	CodeConnectFailed   = "CONNECT_FAILED"
	CodeRPCTimeout      = "RPC_TIMEOUT"
	CodeInvokeFailed    = "INVOKE_FAILED"
	CodeAssertionFailed = "ASSERTION_FAILED"
	CodeInternalError   = "INTERNAL_ERROR"
)

// Request is the top-level wrapper accepted by `sofarpc-cli exec --stdin`.
type Request struct {
	RequestID string                 `json:"requestId"`
	Op        string                 `json:"op"`
	Meta      map[string]interface{} `json:"meta,omitempty"`
	Payload   json.RawMessage        `json:"payload"`
}

// Response is the top-level wrapper returned by direct invocation.
type Response struct {
	RequestID string                 `json:"requestId"`
	OK        bool                   `json:"ok"`
	Code      string                 `json:"code"`
	Data      json.RawMessage        `json:"data,omitempty"`
	Error     *ResponseError         `json:"error,omitempty"`
	Meta      map[string]interface{} `json:"meta,omitempty"`
}

// ResponseError carries diagnostic info on non-SUCCESS responses.
type ResponseError struct {
	Message string                 `json:"message"`
	Cause   string                 `json:"cause,omitempty"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// InvokePayload mirrors protocol/schema/invoke.request.schema.json.
type InvokePayload struct {
	Address      string          `json:"address"`
	Service      string          `json:"service"`
	Method       string          `json:"method"`
	ArgTypes     []string        `json:"argTypes"`
	Args         []interface{}   `json:"args"`
	Assertions   []AssertionSpec `json:"assertions,omitempty"`
	RPCTimeoutMS int             `json:"rpcTimeoutMs,omitempty"`
}

// AssertionSpec is a single assertion descriptor.
type AssertionSpec struct {
	Path   string      `json:"path"`
	Equals interface{} `json:"equals,omitempty"`
	Exists *bool       `json:"exists,omitempty"`
}

// PingPayload mirrors protocol/schema/ping.request.schema.json.
type PingPayload struct {
	Address      string `json:"address"`
	Service      string `json:"service,omitempty"`
	RPCTimeoutMS int    `json:"rpcTimeoutMs,omitempty"`
}
