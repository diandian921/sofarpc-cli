package tools

import (
	"encoding/json"
	"testing"
)

// TestDecodeStrictPreservesLongPrecision proves orderedArguments survive as
// json.Number, so a Java long beyond 2^53 is not rounded through float64 before
// Hessian encoding. This is why invoke/invoke_plan run on the raw Server.AddTool
// path (decodeStrict on untouched wire bytes) rather than the generic AddTool, whose
// applySchema step roundtrips arguments through float64.
func TestDecodeStrictPreservesLongPrecision(t *testing.T) {
	const literal = "9007199254740993" // 2^53 + 1, not representable as float64
	var a InvokeArgs
	if err := decodeStrict(json.RawMessage(`{"service":"S","method":"m","orderedArguments":[`+literal+`]}`), &a); err != nil {
		t.Fatalf("decode failed: %v", err)
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

// TestDecodeStrictRejectsUnknownField proves typos / unsupported keys are rejected
// rather than silently ignored, matching the legacy decodeArgs (DisallowUnknownFields).
func TestDecodeStrictRejectsUnknownField(t *testing.T) {
	var a InvokeArgs
	// "orderedArgument" (missing the trailing s) is not a struct field.
	err := decodeStrict(json.RawMessage(`{"service":"S","method":"m","orderedArgument":[1]}`), &a)
	if err == nil {
		t.Error("expected unknown field to be rejected, got nil error")
	}
}

// TestDecodeStrictRejectsCaseVariant proves a case-variant of a declared field is
// rejected. encoding/json matches keys case-insensitively, so without the exact-key
// guard {"SERVICE":...} would silently populate `service` against the lowercase-only
// input schema (the same class of mismatch the SDK fixed at the protocol layer).
func TestDecodeStrictRejectsCaseVariant(t *testing.T) {
	for _, variant := range []string{
		`{"SERVICE":"S","method":"m"}`,
		`{"Service":"S","method":"m"}`,
		`{"service":"S","Method":"m"}`,
		`{"service":"S","method":"m","TimeoutMs":1}`,
	} {
		var a InvokeArgs
		if err := decodeStrict(json.RawMessage(variant), &a); err == nil {
			t.Errorf("expected case-variant key to be rejected: %s", variant)
		}
	}
	// The exact lowercase/camelCase contract still decodes cleanly.
	var ok InvokeArgs
	if err := decodeStrict(json.RawMessage(`{"service":"S","method":"m","timeoutMs":1}`), &ok); err != nil {
		t.Fatalf("exact keys must decode: %v", err)
	}
	if ok.Service != "S" || ok.Method != "m" || ok.TimeoutMS != 1 {
		t.Fatalf("exact decode lost data: %+v", ok)
	}
}
