package schema

import (
	"reflect"
	"testing"
)

func classType(fqn string, fields ...Field) TypeSchema {
	return TypeSchema{Type: fqn, Kind: "class", Fields: fields}
}

func TestExampleArgumentsFor(t *testing.T) {
	idx := &Index{Types: map[string]TypeSchema{
		// self-recursive
		"com.x.Node": classType("com.x.Node", Field{"id", "long"}, Field{"next", "com.x.Node"}),
		// sibling same-type
		"com.x.Order": classType("com.x.Order", Field{"ship", "com.x.Addr"}, Field{"bill", "com.x.Addr"}),
		"com.x.Addr":  classType("com.x.Addr", Field{"zip", "java.lang.String"}),
		// enum + list + java.time + map
		"com.x.Req": classType("com.x.Req",
			Field{"status", "com.x.Status"},
			Field{"items", "java.util.List<com.x.Item>"},
			Field{"when", "java.time.LocalDate"},
			Field{"attrs", "java.util.Map<java.lang.String,java.lang.String>"},
		),
		"com.x.Status": {Type: "com.x.Status", Kind: "enum", EnumValues: []string{"ACTIVE", "CLOSED"}},
		"com.x.Item":   classType("com.x.Item", Field{"sku", "java.lang.String"}, Field{"qty", "int"}),
		// inheritance
		"com.x.Sub":  {Type: "com.x.Sub", Kind: "class", Extends: []string{"com.x.Base"}, Fields: []Field{{"b", "int"}}},
		"com.x.Base": classType("com.x.Base", Field{"a", "java.lang.String"}),
	}}

	cases := []struct {
		name  string
		param Field
		want  map[string]interface{}
	}{
		{
			name:  "self-recursive cut with null",
			param: Field{"root", "com.x.Node"},
			want:  map[string]interface{}{"root": map[string]interface{}{"id": 0, "next": nil}},
		},
		{
			name:  "sibling same type both expand",
			param: Field{"o", "com.x.Order"},
			want: map[string]interface{}{"o": map[string]interface{}{
				"ship": map[string]interface{}{"zip": ""},
				"bill": map[string]interface{}{"zip": ""},
			}},
		},
		{
			name:  "enum list java.time map",
			param: Field{"r", "com.x.Req"},
			want: map[string]interface{}{"r": map[string]interface{}{
				"status": "ACTIVE",
				"items":  []interface{}{map[string]interface{}{"sku": "", "qty": 0}},
				"when":   "2024-01-01",
				"attrs":  map[string]interface{}{},
			}},
		},
		{
			name:  "inherited fields included",
			param: Field{"s", "com.x.Sub"},
			want:  map[string]interface{}{"s": map[string]interface{}{"b": 0, "a": ""}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			method := Method{Package: "com.x", Parameters: []Parameter{{Name: tc.param.Name, Type: tc.param.Type}}}
			got := ExampleArgumentsFor(idx, method)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("example = %#v\nwant     %#v", got, tc.want)
			}
		})
	}
}

// TestExampleArgumentsMutualRecursionTerminates guards that A->B->A does not
// loop or stack-overflow, and cuts the back-edge with null.
func TestExampleArgumentsMutualRecursionTerminates(t *testing.T) {
	idx := &Index{Types: map[string]TypeSchema{
		"com.x.A": classType("com.x.A", Field{"b", "com.x.B"}),
		"com.x.B": classType("com.x.B", Field{"a", "com.x.A"}),
	}}
	got := ExampleArgumentsFor(idx, Method{Package: "com.x", Parameters: []Parameter{{Name: "a", Type: "com.x.A"}}})
	want := map[string]interface{}{"a": map[string]interface{}{"b": map[string]interface{}{"a": nil}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mutual recursion example = %#v, want %#v", got, want)
	}
}

func TestExampleArgumentsNoParams(t *testing.T) {
	if got := ExampleArgumentsFor(&Index{Types: map[string]TypeSchema{}}, Method{}); got != nil {
		t.Fatalf("no-param method should have nil example, got %#v", got)
	}
}
