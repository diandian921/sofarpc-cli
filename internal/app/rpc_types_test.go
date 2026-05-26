package app

import (
	"reflect"
	"strings"
	"testing"

	"github.com/diandian921/sofarpc-cli/internal/javavalue"
	"github.com/diandian921/sofarpc-cli/internal/schema"
)

func TestTypedArgumentsListOfDTOPreservesElementType(t *testing.T) {
	method := schema.Method{
		Service: "com.x.facade.MaterialFacade",
		Method:  "addMaterials",
		Package: "com.x.facade",
		Parameters: []schema.Parameter{
			{Name: "req", Type: "MaterialAddRequest"},
		},
		Imports: map[string]string{
			"MaterialAddRequest": "com.x.dto.MaterialAddRequest",
		},
	}
	desc := schema.Description{
		Methods: []schema.Method{method},
		Types: map[string]schema.TypeSchema{
			"com.x.dto.MaterialAddRequest": {
				Type: "com.x.dto.MaterialAddRequest",
				Kind: "class",
				Fields: []schema.Field{
					{Name: "items", Type: "List<MaterialItem>"},
				},
				Imports: map[string]string{
					"MaterialItem": "com.x.dto.MaterialItem",
				},
			},
			"com.x.dto.MaterialItem": {
				Type: "com.x.dto.MaterialItem",
				Kind: "class",
				Fields: []schema.Field{
					{Name: "name", Type: "String"},
					{Name: "weight", Type: "int"},
				},
			},
		},
	}
	args := []interface{}{
		map[string]interface{}{
			"items": []interface{}{
				map[string]interface{}{"name": "a", "weight": 1},
			},
		},
	}

	got := typedArgumentsForMethod(args, method, desc)
	if len(got) != 1 || got[0].Kind != javavalue.KindObject {
		t.Fatalf("top-level not object: %#v", got)
	}
	items, ok := got[0].Fields["items"]
	if !ok || items.Kind != javavalue.KindList {
		t.Fatalf("items field not list: %#v", got[0].Fields)
	}
	if len(items.Items) != 1 {
		t.Fatalf("items length: %#v", items.Items)
	}
	element := items.Items[0]
	if element.Kind != javavalue.KindObject {
		t.Errorf("element kind = %q, want object", element.Kind)
	}
	if element.JavaType != "com.x.dto.MaterialItem" {
		t.Errorf("element JavaType = %q, want com.x.dto.MaterialItem", element.JavaType)
	}
}

func TestExtractGenericArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"List<Item>", []string{"Item"}},
		{"Map<String, List<Long>>", []string{"String", "List<Long>"}},
		{"List<Map<String, Item>>", []string{"Map<String, Item>"}},
		{"java.util.List<com.x.Item>", []string{"com.x.Item"}},
		{"String", nil},
		{"", nil},
		{"List<>", []string{""}},
	}
	for _, tc := range cases {
		got := extractGenericArgs(tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("extractGenericArgs(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestResolveGenericType(t *testing.T) {
	imports := map[string]string{
		"MaterialItem":       "com.x.dto.MaterialItem",
		"MaterialAddRequest": "com.x.dto.MaterialAddRequest",
	}
	pkg := "com.x.facade"
	cases := []struct {
		in   string
		want string
	}{
		{"MaterialItem", "com.x.dto.MaterialItem"},
		{"List<MaterialItem>", "java.util.List<com.x.dto.MaterialItem>"},
		{"Map<String, MaterialItem>", "java.util.Map<java.lang.String, com.x.dto.MaterialItem>"},
		{"Set<MaterialItem>", "java.util.Set<com.x.dto.MaterialItem>"},
		{"List<List<MaterialItem>>", "java.util.List<java.util.List<com.x.dto.MaterialItem>>"},
		{"Map<String, List<MaterialItem>>", "java.util.Map<java.lang.String, java.util.List<com.x.dto.MaterialItem>>"},
		{"int", "int"},
		{"long", "long"},
		{"String", "java.lang.String"},
		{"java.util.List<Long>", "java.util.List<java.lang.Long>"},
		{"MaterialItem[]", "com.x.dto.MaterialItem[]"},
		{"List<MaterialItem>[]", "java.util.List<com.x.dto.MaterialItem>[]"},
		{"UnknownClass", "com.x.facade.UnknownClass"},
		{"", ""},
		{"?", "?"},
		{"? extends MaterialItem", "? extends MaterialItem"},
		{"? super MaterialItem", "? super MaterialItem"},
		{"List<?>", "java.util.List<?>"},
		{"List<? extends MaterialItem>", "java.util.List<? extends MaterialItem>"},
	}
	for _, tc := range cases {
		got := resolveGenericType(tc.in, imports, pkg)
		if got != tc.want {
			t.Errorf("resolveGenericType(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTypedArgumentsMapValueDTOPreservesType(t *testing.T) {
	method := schema.Method{
		Service: "com.x.facade.TagFacade",
		Method:  "saveTags",
		Package: "com.x.facade",
		Parameters: []schema.Parameter{
			{Name: "tagsByCode", Type: "Map<String, TagItem>"},
		},
		Imports: map[string]string{
			"TagItem": "com.x.dto.TagItem",
		},
	}
	desc := schema.Description{
		Methods: []schema.Method{method},
		Types: map[string]schema.TypeSchema{
			"com.x.dto.TagItem": {
				Type: "com.x.dto.TagItem",
				Kind: "class",
				Fields: []schema.Field{
					{Name: "label", Type: "String"},
				},
			},
		},
	}
	args := []interface{}{
		map[string]interface{}{
			"A": map[string]interface{}{"label": "alpha"},
		},
	}

	got := typedArgumentsForMethod(args, method, desc)
	if len(got) != 1 || got[0].Kind != javavalue.KindMap {
		t.Fatalf("top-level not map: %#v", got)
	}
	if len(got[0].Entries) != 1 {
		t.Fatalf("entries: %#v", got[0].Entries)
	}
	value := got[0].Entries[0].Value
	if value.Kind != javavalue.KindObject {
		t.Errorf("value kind = %q, want object", value.Kind)
	}
	if value.JavaType != "com.x.dto.TagItem" {
		t.Errorf("value JavaType = %q, want com.x.dto.TagItem", value.JavaType)
	}
}

func TestTypedArgumentsSetOfDTOPreservesElementType(t *testing.T) {
	method := schema.Method{
		Service: "com.x.facade.TagFacade",
		Method:  "addTags",
		Package: "com.x.facade",
		Parameters: []schema.Parameter{
			{Name: "tags", Type: "Set<TagItem>"},
		},
		Imports: map[string]string{
			"TagItem": "com.x.dto.TagItem",
		},
	}
	desc := schema.Description{
		Methods: []schema.Method{method},
		Types: map[string]schema.TypeSchema{
			"com.x.dto.TagItem": {
				Type: "com.x.dto.TagItem",
				Kind: "class",
				Fields: []schema.Field{
					{Name: "label", Type: "String"},
				},
			},
		},
	}
	args := []interface{}{
		[]interface{}{
			map[string]interface{}{"label": "alpha"},
		},
	}
	got := typedArgumentsForMethod(args, method, desc)
	if len(got) != 1 || got[0].Kind != javavalue.KindList {
		t.Fatalf("top-level not list (Set 走 list kind): %#v", got)
	}
	if !strings.HasPrefix(got[0].JavaType, "java.util.Set") {
		t.Errorf("top-level JavaType = %q, want prefix java.util.Set", got[0].JavaType)
	}
	if len(got[0].Items) != 1 || got[0].Items[0].Kind != javavalue.KindObject {
		t.Fatalf("element not object: %#v", got[0].Items)
	}
	if got[0].Items[0].JavaType != "com.x.dto.TagItem" {
		t.Errorf("element JavaType = %q, want com.x.dto.TagItem", got[0].Items[0].JavaType)
	}
}

func TestTypedArgumentsWildcardGenericDoesNotCorruptType(t *testing.T) {
	method := schema.Method{
		Service: "com.x.facade.QueryFacade",
		Method:  "listAny",
		Package: "com.x.facade",
		Parameters: []schema.Parameter{
			{Name: "items", Type: "List<? extends BaseDTO>"},
		},
		Imports: map[string]string{
			"BaseDTO": "com.x.dto.BaseDTO",
		},
	}
	desc := schema.Description{Methods: []schema.Method{method}, Types: map[string]schema.TypeSchema{}}
	args := []interface{}{
		[]interface{}{
			map[string]interface{}{"name": "alpha"},
		},
	}
	got := typedArgumentsForMethod(args, method, desc)
	if len(got) != 1 || got[0].Kind != javavalue.KindList {
		t.Fatalf("top-level not list: %#v", got)
	}
	if len(got[0].Items) != 1 {
		t.Fatalf("items: %#v", got[0].Items)
	}
	if got[0].Items[0].Kind != javavalue.KindMap {
		t.Errorf("wildcard element kind = %q, want map (untyped fallback)", got[0].Items[0].Kind)
	}
	if strings.Contains(got[0].Items[0].JavaType, "com.x.facade.?") {
		t.Errorf("wildcard leaked into FQN: %q", got[0].Items[0].JavaType)
	}
}
