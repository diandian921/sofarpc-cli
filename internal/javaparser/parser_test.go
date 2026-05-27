package javaparser

import "testing"

func TestParseEmptyReturnsEmptyCompilationUnit(t *testing.T) {
	cu, err := Parse([]byte(""), "Empty.java")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if cu == nil {
		t.Fatal("cu = nil")
	}
	if cu.SourceFile != "Empty.java" {
		t.Errorf("SourceFile = %q, want Empty.java", cu.SourceFile)
	}
	if cu.Package != nil {
		t.Errorf("Package = %#v, want nil", cu.Package)
	}
	if len(cu.Imports) != 0 || len(cu.Types) != 0 {
		t.Errorf("Imports/Types non-empty: %#v / %#v", cu.Imports, cu.Types)
	}
}

func TestTypeRefString(t *testing.T) {
	cases := []struct {
		in   TypeRef
		want string
	}{
		{TypeRef{Name: "String"}, "String"},
		{TypeRef{Name: "int"}, "int"},
		{TypeRef{Name: "String", ArrayDims: 2}, "String[][]"},
		{TypeRef{Name: "List", Args: []TypeRef{{Name: "X"}}}, "List<X>"},
		{
			TypeRef{Name: "Map", Args: []TypeRef{{Name: "String"}, {Name: "List", Args: []TypeRef{{Name: "Y"}}}}},
			"Map<String, List<Y>>",
		},
		{TypeRef{IsWildcard: true, WildcardKind: WildcardUnbounded}, "?"},
		{TypeRef{IsWildcard: true, WildcardKind: WildcardExtends, WildcardBound: &TypeRef{Name: "Number"}}, "? extends Number"},
		{TypeRef{IsWildcard: true, WildcardKind: WildcardSuper, WildcardBound: &TypeRef{Name: "Integer"}}, "? super Integer"},
		{
			TypeRef{Name: "List", Args: []TypeRef{{IsWildcard: true, WildcardKind: WildcardExtends, WildcardBound: &TypeRef{Name: "Number"}}}},
			"List<? extends Number>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			got := tc.in.String()
			if got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}
