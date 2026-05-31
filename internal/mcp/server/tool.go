// Package server is layer 2 of the MCP server: tool registration and dispatch.
// It turns the protocol primitives in internal/mcp/proto into MCP tool
// semantics (tools/list, tools/call, strict argument decoding, annotations,
// result wrapping). It knows nothing about SofaRPC and never imports the app
// layer; tools reach progress/logging through the Runtime interface here.
package server

import (
	"context"
	"encoding/json"
)

// Runtime exposes per-request progress and logging to a tool's Run function
// without the tool importing the proto layer. It is satisfied by SessionRuntime,
// which forwards to the live proto.Session bound to the context.
type Runtime interface {
	Progress(ctx context.Context, message string, percent float64)
	Log(ctx context.Context, level, message string)
}

// Annotations are MCP tool hints. Every field is set explicitly per tool (no
// implicit defaults) so the host can risk-classify each tool.
type Annotations struct {
	ReadOnlyHint    bool
	DestructiveHint bool
	IdempotentHint  bool
	OpenWorldHint   bool
}

// ToolSpec is the static description of a tool. Schemas are hand-written
// literals carried verbatim into tools/list.
type ToolSpec struct {
	Name         string
	Title        string
	Description  string
	Annotations  Annotations
	InputSchema  json.RawMessage
	OutputSchema json.RawMessage
	// Async runs the tool on a goroutine so it can be cancelled and emit
	// progress without blocking the read loop. Fast, pure-local tools leave it false.
	Async bool
}

// Result is what a tool produces. Failures are folded into Result (Structured
// carries the rendered failure payload, IsError is true) rather than raised as
// Go errors — failure is part of the contract, not an exception.
type Result struct {
	Structured interface{}
	Summary    string
	IsError    bool
	Meta       map[string]interface{}
}

// Tool binds a static spec to a typed handler. A is the tool's argument struct;
// the registry strict-decodes raw arguments into A before calling Run.
type Tool[A any] struct {
	Spec ToolSpec
	Run  func(ctx context.Context, rt Runtime, args A) Result
}
