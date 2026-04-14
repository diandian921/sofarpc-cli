// Package protocol defines the wire envelope shared with the Java daemon.
//
// The envelope must stay byte-compatible with protocol/schema/*.json. Tests under
// internal/protocol pin the contract by round-tripping golden fixtures.
package protocol

import "encoding/json"

const (
	OpInvoke   = "invoke"
	OpPing     = "ping"
	OpHealth   = "health"
	OpShutdown = "shutdown"
)

const (
	CodeSuccess           = "SUCCESS"
	CodeBadRequest        = "BAD_REQUEST"
	CodeConnectFailed     = "CONNECT_FAILED"
	CodeRPCTimeout        = "RPC_TIMEOUT"
	CodeInvokeFailed      = "INVOKE_FAILED"
	CodeAssertionFailed   = "ASSERTION_FAILED"
	CodeDaemonUnavailable = "DAEMON_UNAVAILABLE"
	CodeInternalError     = "INTERNAL_ERROR"
)

// Request is the top-level wrapper sent to the daemon.
type Request struct {
	RequestID string                 `json:"requestId"`
	Op        string                 `json:"op"`
	Meta      map[string]interface{} `json:"meta,omitempty"`
	Payload   json.RawMessage        `json:"payload"`
}

// Response is the top-level wrapper returned by the daemon.
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
	Address      string             `json:"address"`
	Service      string             `json:"service"`
	Method       string             `json:"method"`
	ArgTypes     []string           `json:"argTypes"`
	Args         []interface{}      `json:"args"`
	Assertions   []AssertionSpec    `json:"assertions,omitempty"`
	RPCTimeoutMS int                `json:"rpcTimeoutMs,omitempty"`
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

// ShutdownPayload mirrors protocol/schema/shutdown.request.schema.json.
type ShutdownPayload struct {
	GraceMS int64 `json:"graceMs,omitempty"`
}

// HealthData mirrors protocol/schema/health.response.schema.json.
type HealthData struct {
	PID              int64  `json:"pid"`
	BuildVersion     string `json:"buildVersion"`
	StartedAtMS      int64  `json:"startedAtMs"`
	Port             int    `json:"port,omitempty"`
	Connections      int    `json:"connections,omitempty"`
	CachedRPCTargets int    `json:"cachedRpcTargets,omitempty"`
}
