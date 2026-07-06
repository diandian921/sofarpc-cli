package javavalue

import (
	"reflect"
	"testing"
)

// TestConstructorsSetKindAndType pins the constructor contract: each builder
// stamps its Kind and carries the Java type through unchanged.
func TestConstructorsSetKindAndType(t *testing.T) {
	cases := []struct {
		name     string
		value    TypedValue
		wantKind Kind
		wantType string
	}{
		{"scalar", Scalar("java.lang.String", "x"), KindScalar, "java.lang.String"},
		{"object", Object("com.x.Dto", nil), KindObject, "com.x.Dto"},
		{"list", List("java.util.List", nil), KindList, "java.util.List"},
		{"map", Map("java.util.Map", nil), KindMap, "java.util.Map"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.value.Kind != tc.wantKind || tc.value.JavaType != tc.wantType {
				t.Errorf("got kind=%q type=%q, want kind=%q type=%q",
					tc.value.Kind, tc.value.JavaType, tc.wantKind, tc.wantType)
			}
		})
	}
}

// TestConstructorsNormalizeNilContainers pins nil-safety: nil fields/items/entries
// become empty (non-nil) containers so callers can range without nil checks.
func TestConstructorsNormalizeNilContainers(t *testing.T) {
	if obj := Object("T", nil); obj.Fields == nil || len(obj.Fields) != 0 {
		t.Errorf("Object(nil) fields = %#v, want empty non-nil map", obj.Fields)
	}
	if list := List("T", nil); list.Items == nil || len(list.Items) != 0 {
		t.Errorf("List(nil) items = %#v, want empty non-nil slice", list.Items)
	}
	if m := Map("T", nil); m.Entries == nil || len(m.Entries) != 0 {
		t.Errorf("Map(nil) entries = %#v, want empty non-nil slice", m.Entries)
	}
}

// TestDisplayShapes is the table-driven pin of Display's per-kind JSON-ready
// shape: scalars carry value, objects carry fields, lists carry items, maps carry
// key/value entry pairs, and nesting recurses.
func TestDisplayShapes(t *testing.T) {
	cases := []struct {
		name  string
		value TypedValue
		want  interface{}
	}{
		{
			name:  "scalar string",
			value: Scalar("java.lang.String", "hello"),
			want: map[string]interface{}{
				"javaType": "java.lang.String", "kind": "scalar", "value": "hello",
			},
		},
		{
			name:  "scalar nil value",
			value: Scalar("java.lang.Object", nil),
			want: map[string]interface{}{
				"javaType": "java.lang.Object", "kind": "scalar", "value": nil,
			},
		},
		{
			name:  "empty object",
			value: Object("com.x.Empty", nil),
			want: map[string]interface{}{
				"javaType": "com.x.Empty", "kind": "object", "fields": map[string]interface{}{},
			},
		},
		{
			name: "object with fields",
			value: Object("com.x.User", map[string]TypedValue{
				"id":   Scalar("java.lang.Long", int64(1)),
				"name": Scalar("java.lang.String", "u"),
			}),
			want: map[string]interface{}{
				"javaType": "com.x.User", "kind": "object",
				"fields": map[string]interface{}{
					"id":   map[string]interface{}{"javaType": "java.lang.Long", "kind": "scalar", "value": int64(1)},
					"name": map[string]interface{}{"javaType": "java.lang.String", "kind": "scalar", "value": "u"},
				},
			},
		},
		{
			name:  "empty list",
			value: List("java.util.List", nil),
			want: map[string]interface{}{
				"javaType": "java.util.List", "kind": "list", "items": []interface{}{},
			},
		},
		{
			name: "list of scalars",
			value: List("java.util.List", []TypedValue{
				Scalar("java.lang.Integer", 1),
				Scalar("java.lang.Integer", 2),
			}),
			want: map[string]interface{}{
				"javaType": "java.util.List", "kind": "list",
				"items": []interface{}{
					map[string]interface{}{"javaType": "java.lang.Integer", "kind": "scalar", "value": 1},
					map[string]interface{}{"javaType": "java.lang.Integer", "kind": "scalar", "value": 2},
				},
			},
		},
		{
			name: "map entries keep key and value displays",
			value: Map("java.util.Map", []MapEntry{
				{Key: Scalar("java.lang.String", "k"), Value: Scalar("java.lang.Integer", 9)},
			}),
			want: map[string]interface{}{
				"javaType": "java.util.Map", "kind": "map",
				"entries": []map[string]interface{}{
					{
						"key":   map[string]interface{}{"javaType": "java.lang.String", "kind": "scalar", "value": "k"},
						"value": map[string]interface{}{"javaType": "java.lang.Integer", "kind": "scalar", "value": 9},
					},
				},
			},
		},
		{
			name: "nested object list map",
			value: Object("com.x.Order", map[string]TypedValue{
				"tags": List("java.util.List", []TypedValue{
					Map("java.util.Map", []MapEntry{
						{Key: Scalar("java.lang.String", "color"), Value: Scalar("java.lang.String", "red")},
					}),
				}),
			}),
			want: map[string]interface{}{
				"javaType": "com.x.Order", "kind": "object",
				"fields": map[string]interface{}{
					"tags": map[string]interface{}{
						"javaType": "java.util.List", "kind": "list",
						"items": []interface{}{
							map[string]interface{}{
								"javaType": "java.util.Map", "kind": "map",
								"entries": []map[string]interface{}{
									{
										"key":   map[string]interface{}{"javaType": "java.lang.String", "kind": "scalar", "value": "color"},
										"value": map[string]interface{}{"javaType": "java.lang.String", "kind": "scalar", "value": "red"},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.value.Display()
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Display() mismatch\n got: %#v\nwant: %#v", got, tc.want)
			}
		})
	}
}

// TestDisplayCoversAllFieldKeys pins deterministic field handling: every field of
// an object shows up exactly once regardless of insertion order (Display iterates
// keys through a sorted list).
func TestDisplayCoversAllFieldKeys(t *testing.T) {
	fields := map[string]TypedValue{
		"zeta":  Scalar("java.lang.String", "z"),
		"alpha": Scalar("java.lang.String", "a"),
		"mid":   Scalar("java.lang.String", "m"),
	}
	display, _ := Object("com.x.T", fields).Display().(map[string]interface{})
	got, _ := display["fields"].(map[string]interface{})
	if len(got) != len(fields) {
		t.Fatalf("expected %d fields, got %d", len(fields), len(got))
	}
	for key := range fields {
		if _, ok := got[key]; !ok {
			t.Errorf("field %q missing from display", key)
		}
	}
}
