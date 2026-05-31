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
