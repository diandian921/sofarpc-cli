package presentation

import (
	"encoding/json"
	"testing"
)

func TestEvaluateAssertions(t *testing.T) {
	result := map[string]interface{}{"status": "INACTIVE", "name": "alice"}
	exists := true
	out, failed := EvaluateAssertions(result, []Assertion{
		{Path: "$.status", Equals: "ACTIVE"},
		{Path: "$.name", Exists: &exists},
	})
	if failed != 1 || len(out) != 2 || out[0].Passed || !out[1].Passed {
		t.Fatalf("unexpected assertions: failed=%d out=%+v", failed, out)
	}
}

func TestFlattenJDKValueTypes(t *testing.T) {
	date := Flatten(map[string]interface{}{
		"type":   "java.util.Date",
		"fields": map[string]interface{}{"fastTime": int64(0)},
	}).(map[string]interface{})
	if date["epochMillis"] != int64(0) || date["iso"] != "1970-01-01T00:00:00Z" {
		t.Fatalf("date = %#v", date)
	}

	optional := Flatten(map[string]interface{}{
		"type":   "java.util.Optional",
		"fields": map[string]interface{}{"present": true, "value": "ok"},
	})
	if optional != "ok" {
		t.Fatalf("optional = %#v", optional)
	}

	emptyOptional := Flatten(map[string]interface{}{
		"type":   "java.util.Optional",
		"fields": map[string]interface{}{"present": false},
	})
	if emptyOptional != nil {
		t.Fatalf("empty optional = %#v", emptyOptional)
	}

	enum := Flatten(map[string]interface{}{
		"type":   "com.example.StatusEnum",
		"fields": map[string]interface{}{"name": "ACTIVE"},
	})
	if enum != "ACTIVE" {
		t.Fatalf("enum = %#v", enum)
	}

	dto := Flatten(map[string]interface{}{
		"type":   "com.example.Name",
		"fields": map[string]interface{}{"name": "alice"},
	}).(map[string]interface{})
	if dto["name"] != "alice" {
		t.Fatalf("single-field DTO should not flatten as enum: %#v", dto)
	}
}

func TestFlattenJDKTimeTypes(t *testing.T) {
	localDate := Flatten(map[string]interface{}{
		"type":   "com.caucho.hessian.io.jdk8.LocalDateHandle",
		"fields": map[string]interface{}{"year": 2024, "month": 1, "day": 15},
	})
	if localDate != "2024-01-15" {
		t.Fatalf("localDate = %#v", localDate)
	}

	localDateTime := Flatten(map[string]interface{}{
		"type": "com.caucho.hessian.io.jdk8.LocalDateTimeHandle",
		"fields": map[string]interface{}{
			"date": map[string]interface{}{
				"type":   "com.caucho.hessian.io.jdk8.LocalDateHandle",
				"fields": map[string]interface{}{"year": 2024, "month": 1, "day": 15},
			},
			"time": map[string]interface{}{
				"type":   "com.caucho.hessian.io.jdk8.LocalTimeHandle",
				"fields": map[string]interface{}{"hour": 10, "minute": 30, "second": 0, "nano": 0},
			},
		},
	})
	if localDateTime != "2024-01-15T10:30:00" {
		t.Fatalf("localDateTime = %#v", localDateTime)
	}

	instant := Flatten(map[string]interface{}{
		"type":   "com.caucho.hessian.io.jdk8.InstantHandle",
		"fields": map[string]interface{}{"seconds": int64(1705314600), "nanos": 0},
	})
	if instant != "2024-01-15T10:30:00Z" {
		t.Fatalf("instant = %#v", instant)
	}
}

func TestFlattenCyclicObjectGraphTerminates(t *testing.T) {
	// a.next = b, b.next = a — the shape the reader produces for a Hessian
	// back-reference. Flatten must cut the cycle, not overflow the stack.
	a := map[string]interface{}{"type": "Node", "fields": map[string]interface{}{}, "fieldNames": []interface{}{"name", "next"}}
	b := map[string]interface{}{"type": "Node", "fields": map[string]interface{}{}, "fieldNames": []interface{}{"name", "next"}}
	a["fields"].(map[string]interface{})["name"] = "a"
	a["fields"].(map[string]interface{})["next"] = b
	b["fields"].(map[string]interface{})["name"] = "b"
	b["fields"].(map[string]interface{})["next"] = a

	out := Flatten(a).(map[string]interface{})
	if out["name"] != "a" {
		t.Fatalf("out = %#v", out)
	}
	bOut := out["next"].(map[string]interface{})
	if bOut["name"] != "b" {
		t.Fatalf("bOut = %#v", bOut)
	}
	cut, ok := bOut["next"].(map[string]interface{})
	if !ok || cut["$circularRef"] != true {
		t.Fatalf("cycle not cut at back-edge: %#v", bOut["next"])
	}
}

func TestFlattenCyclicListTerminates(t *testing.T) {
	list := make([]interface{}, 1)
	list[0] = list

	out := Flatten(list).([]interface{})
	cut, ok := out[0].(map[string]interface{})
	if !ok || cut["$circularRef"] != true {
		t.Fatalf("self list = %#v, want circular marker", out[0])
	}
}

func TestJSONSafePreservesRawShapeAndCutsCycles(t *testing.T) {
	raw := map[string]interface{}{"type": "Node", "fields": map[string]interface{}{"name": "root"}}
	raw["fields"].(map[string]interface{})["self"] = raw
	safe := JSONSafe(raw).(map[string]interface{})
	if safe["type"] != "Node" {
		t.Fatalf("raw type lost: %#v", safe)
	}
	cut := safe["fields"].(map[string]interface{})["self"].(map[string]interface{})
	if cut["$circularRef"] != true {
		t.Fatalf("cycle not cut: %#v", cut)
	}
	if _, err := json.Marshal(safe); err != nil {
		t.Fatalf("JSONSafe result must marshal: %v", err)
	}
}

func TestAssertionsAreTypeStrictButNumericallyEquivalent(t *testing.T) {
	result := map[string]interface{}{"number": json.Number("1.0"), "text": "1", "truth": true}
	out, failed := EvaluateAssertions(result, []Assertion{
		{Path: "$.number", Equals: json.Number("1")},
		{Path: "$.number", Equals: "1"},
		{Path: "$.truth", Equals: "true"},
	})
	if failed != 2 || !out[0].Passed || out[1].Passed || out[2].Passed {
		t.Fatalf("type-strict outcomes = %#v, failed=%d", out, failed)
	}
}

func TestLookupPathSupportsArrayIndexes(t *testing.T) {
	root := map[string]interface{}{"items": []interface{}{"zero", map[string]interface{}{"name": "one"}}}
	if got, ok := LookupPath(root, "$.items.1.name"); !ok || got != "one" {
		t.Fatalf("lookup = %#v, %v", got, ok)
	}
	for _, path := range []string{"$.items.-1", "$.items.2", "$.items.nope"} {
		if got, ok := LookupPath(root, path); ok {
			t.Fatalf("%s unexpectedly matched %#v", path, got)
		}
	}
}

func TestFlattenMapKeysAndBigIntegerKnownGap(t *testing.T) {
	out := Flatten(map[string]interface{}{
		"type": "java.util.LinkedHashMap",
		"entries": map[string]interface{}{
			"7": map[string]interface{}{
				"type":   "java.math.BigInteger",
				"fields": map[string]interface{}{"signum": int64(1)},
			},
		},
	}).(map[string]interface{})
	if _, ok := out["7"].(map[string]interface{}); !ok {
		t.Fatalf("BigInteger without value should stay inspectable as raw fields: %#v", out)
	}
}

func TestFlattenBigDecimalJSONNumber(t *testing.T) {
	got := Flatten(map[string]interface{}{
		"type":   "java.math.BigDecimal",
		"fields": map[string]interface{}{"value": "113795.2485"},
	})
	n, ok := got.(json.Number)
	if !ok || n.String() != "113795.2485" {
		t.Fatalf("got %#v", got)
	}
}
