package app

import (
	"fmt"
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

func TestTypedArgumentsMergesInheritedFieldTypes(t *testing.T) {
	method := schema.Method{
		Service:    "com.x.facade.OrderFacade",
		Method:     "createOrder",
		Package:    "com.x.facade",
		Parameters: []schema.Parameter{{Name: "order", Type: "OrderDTO"}},
		Imports:    map[string]string{"OrderDTO": "com.x.dto.OrderDTO"},
	}
	desc := schema.Description{
		Methods: []schema.Method{method},
		Types: map[string]schema.TypeSchema{
			"com.x.dto.OrderDTO": {
				Type:    "com.x.dto.OrderDTO",
				Kind:    "class",
				Fields:  []schema.Field{{Name: "orderId", Type: "String"}},
				Extends: []string{"BaseDTO"},
				Imports: map[string]string{"BaseDTO": "com.x.base.BaseDTO"},
			},
			"com.x.base.BaseDTO": {
				Type:    "com.x.base.BaseDTO",
				Kind:    "class",
				Fields:  []schema.Field{{Name: "gmtCreate", Type: "Long"}, {Name: "status", Type: "OrderStatus"}},
				Imports: map[string]string{"OrderStatus": "com.x.base.OrderStatus"},
			},
			"com.x.base.OrderStatus": {
				Type:       "com.x.base.OrderStatus",
				Kind:       "enum",
				EnumValues: []string{"ACTIVE", "INACTIVE"},
			},
		},
	}
	args := []interface{}{
		map[string]interface{}{"orderId": "o1", "gmtCreate": 123, "status": "ACTIVE"},
	}

	got := typedArgumentsForMethod(args, method, desc)
	if len(got) != 1 || got[0].Kind != javavalue.KindObject {
		t.Fatalf("top-level not object: %#v", got)
	}
	gmt := got[0].Fields["gmtCreate"]
	if !strings.Contains(gmt.JavaType, "Long") {
		t.Errorf("inherited gmtCreate JavaType = %q, want a Long type (empty means inheritance not merged)", gmt.JavaType)
	}
	status := got[0].Fields["status"]
	if status.Kind != javavalue.KindObject || status.JavaType != "com.x.base.OrderStatus" {
		t.Errorf("inherited enum status = {Kind:%q JavaType:%q}, want enum object com.x.base.OrderStatus", status.Kind, status.JavaType)
	}
}

func TestTypedArgumentsSubstitutesGenericInheritedField(t *testing.T) {
	method := schema.Method{
		Service:    "com.x.facade.OrderFacade",
		Method:     "createOrder",
		Package:    "com.x.facade",
		Parameters: []schema.Parameter{{Name: "order", Type: "OrderDTO"}},
		Imports:    map[string]string{"OrderDTO": "com.x.dto.OrderDTO"},
	}
	desc := schema.Description{
		Methods: []schema.Method{method},
		Types: map[string]schema.TypeSchema{
			"com.x.dto.OrderDTO": {
				Type:    "com.x.dto.OrderDTO",
				Kind:    "class",
				Fields:  []schema.Field{{Name: "orderId", Type: "String"}},
				Extends: []string{"Base<OrderStatus>"},
				Imports: map[string]string{"Base": "com.x.base.Base", "OrderStatus": "com.x.base.OrderStatus"},
			},
			"com.x.base.Base": {
				Type:       "com.x.base.Base",
				Kind:       "class",
				TypeParams: []string{"T"},
				Fields:     []schema.Field{{Name: "status", Type: "T"}, {Name: "history", Type: "List<T>"}},
			},
			"com.x.base.OrderStatus": {
				Type:       "com.x.base.OrderStatus",
				Kind:       "enum",
				EnumValues: []string{"ACTIVE", "INACTIVE"},
			},
		},
	}
	args := []interface{}{
		map[string]interface{}{"orderId": "o1", "status": "ACTIVE", "history": []interface{}{"INACTIVE"}},
	}

	got := typedArgumentsForMethod(args, method, desc)
	if len(got) != 1 || got[0].Kind != javavalue.KindObject {
		t.Fatalf("top-level not object: %#v", got)
	}
	// inherited `T status` -> OrderStatus enum (was unresolved type variable before)
	status := got[0].Fields["status"]
	if status.Kind != javavalue.KindObject || status.JavaType != "com.x.base.OrderStatus" {
		t.Errorf("status = {Kind:%q JavaType:%q}, want enum com.x.base.OrderStatus", status.Kind, status.JavaType)
	}
	// inherited `List<T> history` -> List<OrderStatus>, element typed as the enum
	history := got[0].Fields["history"]
	if history.Kind != javavalue.KindList || len(history.Items) != 1 {
		t.Fatalf("history not a 1-element list: %#v", history)
	}
	if history.Items[0].JavaType != "com.x.base.OrderStatus" {
		t.Errorf("history element JavaType = %q, want com.x.base.OrderStatus", history.Items[0].JavaType)
	}
}

func TestTypedArgumentsEncodesJavaTimeFromISO(t *testing.T) {
	method := schema.Method{
		Service: "com.x.facade.TimeFacade",
		Method:  "byDate",
		Package: "com.x.facade",
		Parameters: []schema.Parameter{
			{Name: "day", Type: "LocalDate"},
			{Name: "ts", Type: "LocalDateTime"},
			{Name: "at", Type: "Instant"},
		},
		Imports: map[string]string{
			"LocalDate":     "java.time.LocalDate",
			"LocalDateTime": "java.time.LocalDateTime",
			"Instant":       "java.time.Instant",
		},
	}
	desc := schema.Description{Methods: []schema.Method{method}}
	args := []interface{}{"2024-01-15", "2024-01-15T10:30:00", "2024-01-15T10:30:00Z"}

	got := typedArgumentsForMethod(args, method, desc)
	if len(got) != 3 {
		t.Fatalf("want 3 args, got %#v", got)
	}
	if got[0].Kind != javavalue.KindObject || got[0].JavaType != "com.caucho.hessian.io.jdk8.LocalDateHandle" {
		t.Fatalf("LocalDate -> %#v", got[0])
	}
	if fmt.Sprint(got[0].Fields["year"].Scalar) != "2024" || fmt.Sprint(got[0].Fields["day"].Scalar) != "15" {
		t.Fatalf("LocalDate fields = %#v", got[0].Fields)
	}
	if got[1].JavaType != "com.caucho.hessian.io.jdk8.LocalDateTimeHandle" {
		t.Fatalf("LocalDateTime -> %#v", got[1])
	}
	if got[2].JavaType != "com.caucho.hessian.io.jdk8.InstantHandle" {
		t.Fatalf("Instant -> %#v", got[2])
	}
	if fmt.Sprint(got[2].Fields["seconds"].Scalar) != "1705314600" {
		t.Fatalf("Instant seconds = %#v", got[2].Fields["seconds"])
	}
}

func TestTypedArgumentsEncodesBigIntegerFromString(t *testing.T) {
	method := schema.Method{
		Service:    "com.x.facade.NumFacade",
		Method:     "big",
		Package:    "com.x.facade",
		Parameters: []schema.Parameter{{Name: "n", Type: "BigInteger"}},
		Imports:    map[string]string{"BigInteger": "java.math.BigInteger"},
	}
	desc := schema.Description{Methods: []schema.Method{method}}

	got := typedArgumentsForMethod([]interface{}{"9223372036854775807"}, method, desc)
	if len(got) != 1 {
		t.Fatalf("want 1 arg, got %#v", got)
	}
	bi := got[0]
	if bi.Kind != javavalue.KindObject || bi.JavaType != "java.math.BigInteger" {
		t.Fatalf("BigInteger -> %#v", bi)
	}
	if fmt.Sprint(bi.Fields["signum"].Scalar) != "1" {
		t.Fatalf("signum = %#v", bi.Fields["signum"])
	}
	mag := bi.Fields["mag"]
	if mag.Kind != javavalue.KindList || len(mag.Items) != 2 {
		t.Fatalf("mag = %#v", mag)
	}
	if fmt.Sprint(mag.Items[0].Scalar) != "2147483647" || fmt.Sprint(mag.Items[1].Scalar) != "-1" {
		t.Fatalf("mag items = %#v", mag.Items)
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
		// type variable 启发式 (codex review 抓的 regression)
		{"T", "T"},
		{"K", "K"},
		{"V", "V"},
		{"E", "E"},
		{"R", "R"},
		{"T1", "T1"},
		{"T2", "T2"},
		{"List<T>", "java.util.List<T>"},
		{"Map<K, V>", "java.util.Map<K, V>"},
		{"List<List<T>>", "java.util.List<java.util.List<T>>"},
		// explicit import 优先于 type variable 启发式
		// (没在 imports 表里加 T,所以这里测的是 "没 import 时" 走 type var 分支;
		//  imports 表如果显式有 T -> com.x.T,会走 imports lookup,见下个单测)
	}
	for _, tc := range cases {
		got := resolveGenericType(tc.in, imports, pkg, nil, nil)
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

func TestResolveTypeVariableOverriddenByExplicitImport(t *testing.T) {
	// 边界 case:如果 user 真的 import 了一个叫 T 的 class(违反 convention 但可能),
	// explicit import 必须优先于 type variable 启发式。
	imports := map[string]string{"T": "com.example.weird.T"}
	got := resolveGenericType("T", imports, "com.example.pkg", nil, nil)
	if got != "com.example.weird.T" {
		t.Errorf("explicit import should win over type-var heuristic, got %q", got)
	}
}

func TestResolveAcronymDTOWithSchemaUsesPkgLookup(t *testing.T) {
	types := map[string]schema.TypeSchema{
		"com.example.dto.URL": {Type: "com.example.dto.URL", Kind: "class"},
	}
	got := resolveGenericType("URL", nil, "com.example.dto", types, nil)
	if got != "com.example.dto.URL" {
		t.Errorf("same-pkg schema lookup should win over type-var heuristic, got %q", got)
	}
}

func TestResolveAcronymDTOWithoutSchemaFallsBackUntyped(t *testing.T) {
	got := resolveGenericType("URL", nil, "com.example.dto", nil, nil)
	if got != "URL" {
		t.Errorf("no-schema acronym should hit type-var heuristic, got %q", got)
	}
}

func TestIsLikelyTypeVariable(t *testing.T) {
	yes := []string{"T", "K", "V", "E", "R", "T1", "T2", "K2", "ID", "URL"}
	no := []string{"", "Foo", "Bar", "Id", "Url", "MaterialItem", "ABCD", "list", "t"}
	for _, s := range yes {
		if !isLikelyTypeVariable(s) {
			t.Errorf("isLikelyTypeVariable(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if isLikelyTypeVariable(s) {
			t.Errorf("isLikelyTypeVariable(%q) = true, want false", s)
		}
	}
}

// codex review (Plan B 实施 commit) 抓的 P2 regression:
// 之前的实现把 type variable T 走 pkg fallback 拼成 "com.x.dto.T",
// 后续 shouldWrapJavaObject 把 element wrap 成不存在的 class。
// e.g. Page<T> 里的 records: List<T> 会让 element 序列化成 com.x.dto.T。
func TestTypedArgumentsListOfTypeVariableFallsBackUntyped(t *testing.T) {
	// 模拟 Page<T> 这种泛型容器 DTO,records 字段类型 List<T>
	method := schema.Method{
		Service: "com.x.facade.QueryFacade",
		Method:  "queryPage",
		Package: "com.x.facade",
		Parameters: []schema.Parameter{
			{Name: "page", Type: "Page"},
		},
		Imports: map[string]string{
			"Page": "com.x.dto.Page",
		},
	}
	desc := schema.Description{
		Methods: []schema.Method{method},
		Types: map[string]schema.TypeSchema{
			"com.x.dto.Page": {
				Type: "com.x.dto.Page",
				Kind: "class",
				Fields: []schema.Field{
					{Name: "records", Type: "List<T>"},
					{Name: "total", Type: "int"},
				},
			},
		},
	}
	args := []interface{}{
		map[string]interface{}{
			"records": []interface{}{
				map[string]interface{}{"name": "row1"},
			},
			"total": 1,
		},
	}
	got := typedArgumentsForMethod(args, method, desc)
	records := got[0].Fields["records"]
	if records.Kind != javavalue.KindList {
		t.Fatalf("records not list: %#v", records)
	}
	if len(records.Items) != 1 {
		t.Fatalf("records items: %#v", records.Items)
	}
	element := records.Items[0]
	// element 应该 fall back 到 untyped Map,而不是被 wrap 成 bogus class
	if element.Kind != javavalue.KindMap {
		t.Errorf("type-var element kind = %q, want map (untyped fallback)", element.Kind)
	}
	if strings.Contains(element.JavaType, "com.x.dto.T") {
		t.Errorf("type variable T leaked into FQN: %q", element.JavaType)
	}
}

func TestResolveBaseTypeP3DeclaredTypeParamShadowsSamePkgClass(t *testing.T) {
	imports := map[string]string{}
	pkg := "com.x.dto"
	types := map[string]schema.TypeSchema{
		"com.x.dto.T": {Type: "com.x.dto.T", Kind: "class"},
	}
	got := resolveBaseType("T", imports, pkg, types, []string{"T", "K"})
	if got != "T" {
		t.Errorf("resolveBaseType(T) = %q, want T (declared type param wins over same-pkg lookup)", got)
	}
	got = resolveBaseType("K", imports, pkg, types, []string{"T", "K"})
	if got != "K" {
		t.Errorf("resolveBaseType(K) = %q, want K", got)
	}
	types["com.x.dto.MaterialItem"] = schema.TypeSchema{Type: "com.x.dto.MaterialItem", Kind: "class"}
	got = resolveBaseType("MaterialItem", imports, pkg, types, []string{"T", "K"})
	if got != "com.x.dto.MaterialItem" {
		t.Errorf("resolveBaseType(MaterialItem) = %q, want com.x.dto.MaterialItem", got)
	}
}

func TestResolveBaseTypeP3NilDeclaredTypeParamsFallsBackToHeuristic(t *testing.T) {
	imports := map[string]string{}
	pkg := "com.x.dto"
	types := map[string]schema.TypeSchema{}
	got := resolveBaseType("T", imports, pkg, types, nil)
	if got != "T" {
		t.Errorf("nil TypeParams + likely type var → %q, want T (heuristic still fires)", got)
	}
	types["com.x.dto.ID"] = schema.TypeSchema{Type: "com.x.dto.ID", Kind: "class"}
	got = resolveBaseType("ID", imports, pkg, types, nil)
	if got != "com.x.dto.ID" {
		t.Errorf("nil TypeParams + ID with schema → %q, want com.x.dto.ID", got)
	}
}

func TestRpcParamTypeForMethodP3SkipsTypeParamPkgFallback(t *testing.T) {
	method := schema.Method{
		Package:    "com.x.facade",
		TypeParams: []string{"T"},
		Imports:    map[string]string{},
	}
	got := rpcParamTypeForMethod("T", method)
	if got != "T" {
		t.Errorf("rpcParamTypeForMethod(T) = %q, want T (TypeParam should bypass pkg fallback)", got)
	}
	got = rpcParamTypeForMethod("MaterialItem", method)
	if got != "com.x.facade.MaterialItem" {
		t.Errorf("rpcParamTypeForMethod(MaterialItem) = %q, want com.x.facade.MaterialItem", got)
	}
}

func TestRpcFieldTypeForTypeP3SkipsTypeParamPkgFallback(t *testing.T) {
	owner := schema.TypeSchema{
		Type:       "com.x.dto.Page",
		Kind:       "class",
		TypeParams: []string{"T"},
		Imports:    map[string]string{},
	}
	got := rpcFieldTypeForType("T", owner)
	if got != "T" {
		t.Errorf("rpcFieldTypeForType(T) = %q, want T (class TypeParam should bypass pkg fallback)", got)
	}
}
