package direct

import (
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/diandian921/sofarpc-cli/internal/presentation"
)

func TestHessianJavaGoldenDecode(t *testing.T) {
	checks := map[string]func(t *testing.T, got interface{}){
		"string-emoji": func(t *testing.T, got interface{}) {
			if got != "a🙂b" {
				t.Fatalf("got %#v", got)
			}
		},
		"long": func(t *testing.T, got interface{}) {
			if got != int64(433905635109773312) {
				t.Fatalf("got %#v", got)
			}
		},
		"integer": func(t *testing.T, got interface{}) {
			if got != int64(5) {
				t.Fatalf("got %#v", got)
			}
		},
		"double-whole": func(t *testing.T, got interface{}) {
			if got != float64(2) {
				t.Fatalf("got %#v", got)
			}
		},
		"big-decimal": func(t *testing.T, got interface{}) {
			fields := goldenObjectFields(t, got, "java.math.BigDecimal")
			if fields["value"] != "1000.50" {
				t.Fatalf("fields = %#v", fields)
			}
		},
		"list-with-null": func(t *testing.T, got interface{}) {
			items := goldenListItems(t, got)
			if len(items) != 3 || items[0] != nil || items[1] != int64(1) || items[2] != "two" {
				t.Fatalf("items = %#v", items)
			}
		},
		"map-long-key": func(t *testing.T, got interface{}) {
			entries := goldenMapEntries(t, got)
			if entries["7"] != "seven" || entries["name"] != "alice" {
				t.Fatalf("entries = %#v", entries)
			}
		},
		"bytes": func(t *testing.T, got interface{}) {
			b, ok := got.([]byte)
			if !ok || hex.EncodeToString(b) != "0102ff" {
				t.Fatalf("got %#v", got)
			}
		},
		"enum": func(t *testing.T, got interface{}) {
			fields := goldenObjectFields(t, got, "HessianContractHelper$Status")
			if fields["name"] != "ACTIVE" {
				t.Fatalf("fields = %#v", fields)
			}
		},
		"query-response": func(t *testing.T, got interface{}) {
			fields := goldenObjectFields(t, got, "HessianContractHelper$QueryResponse")
			if fields["success"] != true {
				t.Fatalf("fields = %#v", fields)
			}
			amount := goldenObjectFields(t, fields["amount"], "java.math.BigDecimal")
			if amount["value"] != "113795.2485" {
				t.Fatalf("amount = %#v", amount)
			}
			tags := goldenListItems(t, fields["tags"])
			if len(tags) != 2 || tags[0] != "A" || tags[1] != "B" {
				t.Fatalf("tags = %#v", tags)
			}
		},
		"enum-response": func(t *testing.T, got interface{}) {
			fields := goldenObjectFields(t, got, "HessianContractHelper$EnumResponse")
			status := goldenObjectFields(t, fields["status"], "HessianContractHelper$Status")
			if status["name"] != "ACTIVE" {
				t.Fatalf("status = %#v", status)
			}
			history := goldenListItems(t, fields["history"])
			if len(history) != 1 {
				t.Fatalf("history = %#v", history)
			}
			item := goldenObjectFields(t, history[0], "HessianContractHelper$Status")
			if item["name"] != "INACTIVE" {
				t.Fatalf("history[0] = %#v", item)
			}
		},
		"nested-response": func(t *testing.T, got interface{}) {
			fields := goldenObjectFields(t, got, "HessianContractHelper$ComplexResponse")
			primary := goldenObjectFields(t, fields["primary"], "HessianContractHelper$QueryResponse")
			primaryAmount := goldenObjectFields(t, primary["amount"], "java.math.BigDecimal")
			if primary["success"] != true || primaryAmount["value"] != "1.23" {
				t.Fatalf("primary = %#v", primary)
			}
			history := goldenListItems(t, fields["history"])
			if len(history) != 1 {
				t.Fatalf("history = %#v", history)
			}
			firstHistory := goldenObjectFields(t, history[0], "HessianContractHelper$QueryResponse")
			historyAmount := goldenObjectFields(t, firstHistory["amount"], "java.math.BigDecimal")
			if firstHistory["success"] != false || historyAmount["value"] != "0.00" {
				t.Fatalf("history[0] = %#v", firstHistory)
			}
			attrs := goldenMapEntries(t, fields["attributes"])
			if attrs["mpCode"] != int64(433905635109773312) || attrs["nullable"] != nil || attrs["ratio"] != float64(2) {
				t.Fatalf("attributes = %#v", attrs)
			}
			mixed := goldenListItems(t, fields["mixed"])
			if len(mixed) != 3 || mixed[0] != nil || mixed[1] != "x" || mixed[2] != int64(9) {
				t.Fatalf("mixed = %#v", mixed)
			}
		},
		"date": func(t *testing.T, got interface{}) {
			if got != int64(0) {
				t.Fatalf("date = %#v, want epoch millis", got)
			}
		},
		"set": func(t *testing.T, got interface{}) {
			items := goldenListItems(t, got)
			if len(items) != 3 || items[0] != "x" || items[1] != "y" || items[2] != "z" {
				t.Fatalf("set items = %#v", items)
			}
		},
	}

	for _, tc := range hessianJavaGoldenCases {
		t.Run(tc.name, func(t *testing.T) {
			got := readGoldenHessian(t, tc.hex)
			check, ok := checks[tc.name]
			if !ok {
				t.Fatalf("missing semantic check for golden case %q", tc.name)
			}
			check(t, got)
			assertGoldenPresentationJSON(t, got, tc.wantPresentationJSON)
		})
	}
}

// TestHessianGoldenCircularReferenceResolves pins that a Hessian back-reference
// in a self-referential object graph (a.next=b, b.next=a) resolves to the SAME
// object — the reader registers an object before reading its fields, so cycles
// share identity instead of erroring or looping.
func TestHessianGoldenCircularReferenceResolves(t *testing.T) {
	got := readGoldenHessian(t, hessianCircularGoldenHex)
	topFields := goldenObjectFields(t, got, "HessianContractHelper$Node")
	if topFields["name"] != "a" {
		t.Fatalf("top name = %#v", topFields["name"])
	}
	bFields := goldenObjectFields(t, topFields["next"], "HessianContractHelper$Node")
	if bFields["name"] != "b" {
		t.Fatalf("next name = %#v", bFields["name"])
	}
	a2, ok := bFields["next"].(map[string]interface{})
	if !ok {
		t.Fatalf("b.next = %#v, want an object", bFields["next"])
	}
	topMap, _ := got.(map[string]interface{})
	// pointer identity, not DeepEqual — a cyclic value would recurse forever.
	if reflect.ValueOf(a2).Pointer() != reflect.ValueOf(topMap).Pointer() {
		t.Fatalf("b.next is not the same object as top — back-reference not resolved")
	}
}

func TestHessianJavaGoldenDocumentsKnownBigIntegerPresentationGap(t *testing.T) {
	got := readGoldenHessian(t, hessianBigIntegerGoldenHex)
	fields := goldenObjectFields(t, got, "java.math.BigInteger")
	if _, ok := fields["value"]; ok {
		t.Fatalf("BigInteger unexpectedly rendered as value; remove this known-gap test")
	}
	if fields["signum"] != int64(1) || fields["mag"] == nil {
		t.Fatalf("fields = %#v", fields)
	}
	rendered, ok := presentation.Flatten(got).(map[string]interface{})
	if !ok {
		t.Fatalf("rendered BigInteger = %#v", rendered)
	}
	if _, ok := rendered["value"]; ok {
		t.Fatalf("BigInteger unexpectedly rendered as value: %#v", rendered)
	}
	if rendered["signum"] != int64(1) || rendered["mag"] == nil {
		t.Fatalf("rendered BigInteger = %#v", rendered)
	}
}

func assertGoldenPresentationJSON(t *testing.T, got interface{}, want string) {
	t.Helper()
	rendered := presentation.Flatten(got)
	data, err := json.Marshal(rendered)
	if err != nil {
		t.Fatalf("marshal rendered value: %v", err)
	}
	if string(data) != want {
		t.Fatalf("rendered JSON = %s, want %s", data, want)
	}
}

func readGoldenHessian(t *testing.T, text string) interface{} {
	t.Helper()
	data, err := hex.DecodeString(text)
	if err != nil {
		t.Fatalf("decode golden hex: %v", err)
	}
	got, err := (&reader{data: data}).readValue()
	if err != nil {
		t.Fatalf("readValue: %v; data=%s", err, text)
	}
	return got
}

func goldenListItems(t *testing.T, got interface{}) []interface{} {
	t.Helper()
	if items, ok := got.([]interface{}); ok {
		return items
	}
	obj, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("got %T, want list", got)
	}
	items, ok := obj["items"].([]interface{})
	if !ok {
		t.Fatalf("got %#v, want list items", got)
	}
	return items
}

func goldenMapEntries(t *testing.T, got interface{}) map[string]interface{} {
	t.Helper()
	entries, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("got %T, want map", got)
	}
	if nested, ok := entries["entries"].(map[string]interface{}); ok {
		return nested
	}
	return entries
}

func goldenObjectFields(t *testing.T, got interface{}, wantType string) map[string]interface{} {
	t.Helper()
	obj, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("got %T, want object %s", got, wantType)
	}
	if obj["type"] != wantType {
		t.Fatalf("type = %#v, want %s; object=%#v", obj["type"], wantType, obj)
	}
	fields, ok := obj["fields"].(map[string]interface{})
	if !ok {
		t.Fatalf("fields missing in %#v", obj)
	}
	return fields
}
