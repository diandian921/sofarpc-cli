package server

import (
	"context"
	"encoding/json"
	"testing"
)

type echoArgs struct {
	Name   string `json:"name"`
	DryRun bool   `json:"dryRun,omitempty"`
}

func newEchoTool() Tool[echoArgs] {
	return Tool[echoArgs]{
		Spec: ToolSpec{
			Name:        "mock_echo",
			Title:       "Echo",
			Description: "test echo tool",
			Annotations: Annotations{ReadOnlyHint: true, IdempotentHint: true},
			InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"],"additionalProperties":false}`),
		},
		Run: func(_ context.Context, _ Runtime, a echoArgs) Result {
			return Result{Structured: map[string]interface{}{"echo": a.Name}, Summary: "ok"}
		},
	}
}

func newRegistryWithEcho() *Registry {
	r := NewRegistry()
	Register(r, newEchoTool())
	return r
}

func TestToolListEmitsSchemaAndAnnotations(t *testing.T) {
	list := newRegistryWithEcho().ToolList()
	if len(list) != 1 {
		t.Fatalf("want 1 tool, got %d", len(list))
	}
	entry := list[0]
	if entry["name"] != "mock_echo" {
		t.Fatalf("name = %v", entry["name"])
	}
	ann, ok := entry["annotations"].(map[string]interface{})
	if !ok {
		t.Fatalf("annotations must always be present: %#v", entry)
	}
	if ann["readOnlyHint"] != true || ann["destructiveHint"] != false {
		t.Fatalf("annotations not injected: %#v", ann)
	}
	if _, ok := entry["inputSchema"]; !ok {
		t.Fatalf("inputSchema missing")
	}
}

func TestCallDispatchesTypedArgs(t *testing.T) {
	res, derr := newRegistryWithEcho().Call(context.Background(), SessionRuntime{}, "mock_echo", json.RawMessage(`{"name":"x"}`))
	if derr != nil {
		t.Fatalf("unexpected jsonrpc error: %+v", derr)
	}
	structured, _ := res.StructuredContent.(map[string]interface{})
	if structured["echo"] != "x" {
		t.Fatalf("echo = %v", structured["echo"])
	}
	if res.Meta["elapsedMs"] == nil {
		t.Fatalf("_meta.elapsedMs missing")
	}
}

func TestCallRejectsUnknownField(t *testing.T) {
	_, derr := newRegistryWithEcho().Call(context.Background(), SessionRuntime{}, "mock_echo", json.RawMessage(`{"name":"x","bogus":1}`))
	if derr == nil || derr.Code != -32602 {
		t.Fatalf("unknown field must be -32602, got %+v", derr)
	}
}

func TestCallRejectsStringForBool(t *testing.T) {
	_, derr := newRegistryWithEcho().Call(context.Background(), SessionRuntime{}, "mock_echo", json.RawMessage(`{"name":"x","dryRun":"true"}`))
	if derr == nil || derr.Code != -32602 {
		t.Fatalf("string into bool must be -32602, got %+v", derr)
	}
}

func TestCallUnknownToolIsInvalidParams(t *testing.T) {
	_, derr := newRegistryWithEcho().Call(context.Background(), SessionRuntime{}, "nope", nil)
	if derr == nil || derr.Code != -32602 {
		t.Fatalf("unknown tool must be a -32602 jsonrpc error, got %+v", derr)
	}
}
