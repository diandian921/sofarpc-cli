package direct

import (
	"reflect"
	"testing"
)

func TestHessianKnownLengthListSelfReferenceResolves(t *testing.T) {
	got, err := (&reader{data: []byte{'V', 0x6e, 0x01, refByte, 0x00, 'z'}}).readValue()
	if err != nil {
		t.Fatalf("readValue: %v", err)
	}
	items, ok := got.([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("got %#v, want one-item list", got)
	}
	assertSameListIdentity(t, items, items[0])
}

func TestHessianVariableLengthListSelfReferenceResolves(t *testing.T) {
	got, err := (&reader{data: []byte{'V', refByte, 0x00, 'z'}}).readValue()
	if err != nil {
		t.Fatalf("readValue: %v", err)
	}
	items, ok := got.([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("got %#v, want one-item list", got)
	}
	assertSameListIdentity(t, items, items[0])
}

func TestHessianVariableLengthTypedListSelfReferenceResolves(t *testing.T) {
	typeName := "java.util.ArrayList"
	data := []byte{'V', 't', 0x00, byte(len(typeName))}
	data = append(data, typeName...)
	data = append(data, refByte, 0x00, 'z')

	got, err := (&reader{data: data}).readValue()
	if err != nil {
		t.Fatalf("readValue: %v", err)
	}
	list, ok := got.(map[string]interface{})
	if !ok || list["type"] != typeName {
		t.Fatalf("got %#v, want typed list", got)
	}
	items, ok := list["items"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v, want one item", list["items"])
	}
	backRef, ok := items[0].(map[string]interface{})
	if !ok || reflect.ValueOf(backRef).Pointer() != reflect.ValueOf(list).Pointer() {
		t.Fatalf("typed-list back-reference did not preserve identity: %#v", items[0])
	}
}

func TestHessianVariableLengthListResolvesNestedBackReference(t *testing.T) {
	data := []byte{'V', 'M', 0x04, 's', 'e', 'l', 'f', refByte, 0x00, 'z', 'z'}
	got, err := (&reader{data: data}).readValue()
	if err != nil {
		t.Fatalf("readValue: %v", err)
	}
	items, ok := got.([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("got %#v, want one-item list", got)
	}
	nested, ok := items[0].(map[string]interface{})
	if !ok {
		t.Fatalf("item = %#v, want map", items[0])
	}
	assertSameListIdentity(t, items, nested["self"])
}

func TestHessianFixedListSelfReferenceResolves(t *testing.T) {
	r := &reader{
		data:  []byte{'v', intZero, intZero + 1, refByte, 0x00},
		types: []string{"java.util.ArrayList"},
	}
	got, err := r.readValue()
	if err != nil {
		t.Fatalf("readValue: %v", err)
	}
	list, ok := got.(map[string]interface{})
	if !ok || list["type"] != "java.util.ArrayList" {
		t.Fatalf("got %#v, want typed list", got)
	}
	items, ok := list["items"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v, want one item", list["items"])
	}
	backRef, ok := items[0].(map[string]interface{})
	if !ok || reflect.ValueOf(backRef).Pointer() != reflect.ValueOf(list).Pointer() {
		t.Fatalf("fixed-list back-reference did not preserve identity: %#v", items[0])
	}
}

func assertSameListIdentity(t *testing.T, want []interface{}, got interface{}) {
	t.Helper()
	backRef, ok := got.([]interface{})
	if !ok {
		t.Fatalf("back-reference = %T, want []interface{}", got)
	}
	if reflect.ValueOf(backRef).Pointer() != reflect.ValueOf(want).Pointer() {
		t.Fatalf("back-reference did not preserve list identity")
	}
}
