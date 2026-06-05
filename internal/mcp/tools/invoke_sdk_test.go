package tools

import (
	"encoding/json"
	"testing"
)

// TestDecodeInvokeArgsPreservesLongPrecision proves orderedArguments survive as
// json.Number, so a Java long beyond 2^53 is not rounded through float64 before
// Hessian encoding. This is the reason invoke/invoke_plan use the raw Server.AddTool
// (untouched wire bytes) instead of the generic AddTool, whose applySchema step
// roundtrips arguments through float64.
func TestDecodeInvokeArgsPreservesLongPrecision(t *testing.T) {
	const literal = "9007199254740993" // 2^53 + 1, not representable as float64
	raw := json.RawMessage(`{"service":"S","method":"m","orderedArguments":[` + literal + `]}`)

	a, derr := decodeInvokeArgs(raw)
	if derr != nil {
		t.Fatalf("decode failed: %+v", *derr)
	}
	if len(a.OrderedArguments) != 1 {
		t.Fatalf("expected 1 ordered argument, got %d", len(a.OrderedArguments))
	}
	num, ok := a.OrderedArguments[0].(json.Number)
	if !ok {
		t.Fatalf("argument decoded as %T, want json.Number (float64 would lose precision)", a.OrderedArguments[0])
	}
	if num.String() != literal {
		t.Errorf("long precision lost: got %s, want %s", num.String(), literal)
	}
}
