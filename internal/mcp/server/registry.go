package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/diandian921/sofarpc-mcp/internal/mcp/proto"
)

type registered struct {
	spec   ToolSpec
	invoke func(ctx context.Context, rt Runtime, raw json.RawMessage) (Result, *proto.Error)
}

// Registry holds the registered tools and dispatches tools/list and tools/call.
type Registry struct {
	order []string
	tools map[string]*registered
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{tools: map[string]*registered{}}
}

// Register adds a typed tool. Registration captures a closure that strict-decodes
// raw arguments into A before invoking Run, so the dispatch path stays untyped.
func Register[A any](r *Registry, t Tool[A]) {
	name := t.Spec.Name
	if _, exists := r.tools[name]; !exists {
		r.order = append(r.order, name)
	}
	r.tools[name] = &registered{
		spec: t.Spec,
		invoke: func(ctx context.Context, rt Runtime, raw json.RawMessage) (Result, *proto.Error) {
			var args A
			if derr := decodeArgs(raw, &args); derr != nil {
				return Result{}, derr
			}
			return t.Run(ctx, rt, args), nil
		},
	}
}

// Has reports whether a tool is registered.
func (r *Registry) Has(name string) bool {
	_, ok := r.tools[name]
	return ok
}

// Async reports whether a tool should run on the async dispatch path.
func (r *Registry) Async(name string) bool {
	t, ok := r.tools[name]
	return ok && t.spec.Async
}

// ToolList returns the tools/list payload, always emitting an annotations object.
func (r *Registry) ToolList() []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(r.order))
	for _, name := range r.order {
		t := r.tools[name]
		entry := map[string]interface{}{
			"name":        t.spec.Name,
			"description": t.spec.Description,
			"inputSchema": rawOrEmptyObject(t.spec.InputSchema),
			"annotations": annotationsMap(t.spec),
		}
		if t.spec.Title != "" {
			entry["title"] = t.spec.Title
		}
		if len(t.spec.OutputSchema) > 0 {
			entry["outputSchema"] = json.RawMessage(t.spec.OutputSchema)
		}
		out = append(out, entry)
	}
	return out
}

// Call dispatches a tools/call. An unknown tool name or a strict-decode failure
// is a JSON-RPC -32602 (the tools/call method exists; its params.name/arguments
// are invalid). A business failure is a CallResult with IsError set.
func (r *Registry) Call(ctx context.Context, rt Runtime, name string, rawArgs json.RawMessage) (CallResult, *proto.Error) {
	t, ok := r.tools[name]
	if !ok {
		return CallResult{}, &proto.Error{Code: proto.CodeInvalidParams, Message: "unknown tool: " + name}
	}
	start := time.Now()
	res, derr := t.invoke(ctx, rt, rawArgs)
	if derr != nil {
		return CallResult{}, derr
	}
	return wrapResult(res, time.Since(start)), nil
}

// Validate enforces invariants that must hold before serving: every registered
// tool must declare an outputSchema, since they all emit the structured app.Result
// envelope. This holds under --disable-config-write too, where only the read tools
// are registered.
func (r *Registry) Validate() error {
	for _, name := range r.order {
		if len(r.tools[name].spec.OutputSchema) == 0 {
			return fmt.Errorf("tool %q must declare an outputSchema", name)
		}
	}
	return nil
}

func annotationsMap(spec ToolSpec) map[string]interface{} {
	m := map[string]interface{}{
		"readOnlyHint":    spec.Annotations.ReadOnlyHint,
		"destructiveHint": spec.Annotations.DestructiveHint,
		"idempotentHint":  spec.Annotations.IdempotentHint,
		"openWorldHint":   spec.Annotations.OpenWorldHint,
	}
	if spec.Title != "" {
		m["title"] = spec.Title
	}
	return m
}

func rawOrEmptyObject(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{"type":"object"}`)
	}
	return json.RawMessage(raw)
}
