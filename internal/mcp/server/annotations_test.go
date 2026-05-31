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

func TestValidateRequiresOutputSchemaForRichTools(t *testing.T) {
	r := NewRegistry()
	registerSpec(r, ToolSpec{Name: "sofarpc_resolve", InputSchema: json.RawMessage(`{"type":"object"}`)})
	if err := r.Validate(); err == nil {
		t.Fatal("sofarpc_resolve without outputSchema must fail Validate")
	}

	r2 := NewRegistry()
	registerSpec(r2, ToolSpec{
		Name:         "sofarpc_resolve",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	})
	if err := r2.Validate(); err != nil {
		t.Fatalf("sofarpc_resolve with outputSchema should pass Validate: %v", err)
	}
}

func TestValidateIgnoresPlainTools(t *testing.T) {
	r := NewRegistry()
	registerSpec(r, ToolSpec{Name: "sofarpc_probe", InputSchema: json.RawMessage(`{"type":"object"}`)})
	if err := r.Validate(); err != nil {
		t.Fatalf("a tool without an outputSchema requirement should pass: %v", err)
	}
}
