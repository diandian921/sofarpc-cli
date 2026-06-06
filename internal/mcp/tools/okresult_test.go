package tools

import (
	"encoding/json"
	"testing"
)

// TestOkResultRejectsNonObjectData guards the per-tool output contract at the source:
// every tool's data.* schema assumes data is a JSON object, so okResult must refuse a
// scalar or array payload instead of silently emitting a result that breaks the
// advertised schema. (The raw Server.AddTool path does not validate output, so this
// invariant has to be enforced here.)
func TestOkResultRejectsNonObjectData(t *testing.T) {
	for _, bad := range []interface{}{[]int{1, 2}, "scalar", 42, true, nil} {
		r := okResult(bad)
		if r.OK {
			t.Errorf("okResult(%#v) must not be OK with non-object data", bad)
		}
	}
}

// TestOkResultKeepsObjectData confirms the normal path: an object payload is wrapped
// as a successful result whose data is that JSON object.
func TestOkResultKeepsObjectData(t *testing.T) {
	r := okResult(map[string]interface{}{"k": "v"})
	if !r.OK {
		t.Fatalf("okResult(object) should be OK, got %+v", r)
	}
	var v any
	if err := json.Unmarshal(r.Data, &v); err != nil {
		t.Fatalf("data not valid JSON: %v", err)
	}
	if _, ok := v.(map[string]interface{}); !ok {
		t.Errorf("data should be a JSON object, got %T", v)
	}
}
