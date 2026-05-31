package server

import (
	"context"
	"encoding/json"
	"testing"
)

type emptyArgs struct{}

func registerSpec(r *Registry, spec ToolSpec) {
	Register(r, Tool[emptyArgs]{
		Spec: spec,
		Run:  func(_ context.Context, _ Runtime, _ emptyArgs) Result { return Result{Summary: "ok"} },
	})
}

func TestValidateRequiresOutputSchemaForEveryTool(t *testing.T) {
	r := NewRegistry()
	registerSpec(r, ToolSpec{Name: "sofarpc_resolve", InputSchema: json.RawMessage(`{"type":"object"}`)})
	if err := r.Validate(); err == nil {
		t.Fatal("a tool without outputSchema must fail Validate")
	}

	r2 := NewRegistry()
	registerSpec(r2, ToolSpec{
		Name:         "sofarpc_resolve",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	})
	if err := r2.Validate(); err != nil {
		t.Fatalf("a tool with outputSchema should pass Validate: %v", err)
	}
}

func TestValidateRejectsPlainToolsToo(t *testing.T) {
	r := NewRegistry()
	registerSpec(r, ToolSpec{Name: "sofarpc_probe", InputSchema: json.RawMessage(`{"type":"object"}`)})
	if err := r.Validate(); err == nil {
		t.Fatal("every registered tool must declare an outputSchema, even sofarpc_probe")
	}
}
