# rpc-types 泛型保留重构 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `internal/app/rpc_types.go` 引入"两套 type contract" —— **identity**(erased FQN,给 overload 匹配 / wire ArgTypes / plan output / user-facing 用)和 **value**(generic-aware FQN,只在 javavalue tree 内部用)。 修复 Issue A —— 嵌套 DTO `List<MaterialItem>` / `Map<String, DTO>` / `Set<DTO>` 调用时,element 被序列化成 untyped Map,服务端反序列化 ClassCastException → SYS_ERROR。

**Architecture:**

**两条 type contract,各自的 caller 永远不需要切换:**

```
Identity (erased)                    Value (generic-aware)
  ↓                                    ↓
sameParamTypes (overload 匹配)      typedValueForJavaType
methodSignatures (用户看的 sig)         (list/map element 递归)
rpcParamTypesForMethod              typedArgumentsForMethod
  → InvocationPlan.Method.ParamTypes  typedValueForParam
  → direct.Request.ArgTypes (wire)  (内部 field 类型解析)
```

- **identity 路径** 沿用现有 `rpcParamTypeForMethod` / `rpcFieldTypeForType`(它们当前就是返回 erased FQN,**行为不变**);所有 **javavalue construction 之外** 的 identity consumer(`sameParamTypes` / `methodSignatures` / `rpcParamTypesForMethod` / invoke.go 三处 paramTypes output)全部不动
- **value 路径** 新增 `rpcValueTypeForMethod` / `rpcValueTypeForType`,返回 resolved-with-generics 完整字符串(如 `"java.util.List<com.x.MaterialItem>"`),递归 resolve 每个 generic arg
- `typedValueForJavaType` 的 list case(同时承载 `Set<T>` 因为 Go 端都是 `[]interface{}`)和 untyped map case 用 `extractGenericArgs` 从带泛型 javaType 提取 element / value type 递归
- Wire 层(`internal/direct/hessian_writer.go:eraseJavaType`)**不动**,作为最后兜底:即便 generic 不小心泄漏到 javavalue.JavaType,wire 输出也保证 erased
- Wildcard generic `?` / `? extends X` / `? super X` 显式特判,**整段保留不递归 resolve**(不仅外层 `?` 不进 import resolve,bound 内部的 `X` 也不递归);效果是 wildcard element 永远走 untyped Map 兜底(预期行为,因为我们没有具体 element 类型可用)

**这个架构跟前一版差别(请勿采用前一版):** 前一版让 `rpcParamTypeForMethod` 改成 generic-aware,然后在所有 caller 上"自己 erase 回来",blast radius 大且容易漏一处(原 codex review #1 / #2 / #8)。 当前架构由 codex review 提议:**从源头隔离两条 contract,各自的 caller 永远不需要 case-by-case 切换**,且 `rpcParamTypeForMethod` 现有行为 / 测试 / invoke.go callsite 完全不动。 Plan 改动量比前一版小一半。

**Tech Stack:** Go 1.21+,标准库 `strings`,既有 `testing` + `reflect`。

---

## File Structure

| 文件 | 操作 | 责任 |
|---|---|---|
| `internal/app/rpc_types.go` | Modify | 新增 helper:`extractGenericArgs` / `resolveGenericType` / `resolveBaseType`、value resolver `rpcValueTypeForMethod` / `rpcValueTypeForType`;改 `typedArgumentsForMethod`(line 50-60)/ `typedValueForParam`(line 62-64)/ 内部 field 递归(line 78-92)从调 identity resolver 切到调 value resolver;改 `typedValueForJavaType` 的 list / map case(line 109-114 / 101-108)用 `extractGenericArgs` 取 element/value type 递归;在 `resolveBaseType` 显式处理 wildcard `?` |
| `internal/app/rpc_types_test.go` | Create | 新文件,table-driven test + 嵌套 DTO list / map / set 端到端用例 + wildcard generic 用例 |
| `internal/app/invoke.go` | **不动** | identity 路径不变,line 209 / 220 / 242 仍调 `rpcParamTypeForMethod` / `rpcParamTypesForMethod`,行为不变 |
| `internal/app/invoke_test.go` | **不动** | identity 路径不变,line 39-46 现有 assertion 仍 PASS |
| `internal/direct/hessian_writer.go` | **不动** | 已有 `eraseJavaType` 兜底(line 132 / 148 / 158 / 337 / 422) |
| `internal/direct/hessian_writer_test.go` | Modify(若不存在则创建) | 加 wire 端到端 sanity:带泛型 javaType 经过 writer,**decode** wire bytes 断言 type 字符串是 erased(不只是搜 `<>`) |

---

## Task 1:Failing test — nested DTO list 丢类型(RED phase)

**Files:**
- Create: `internal/app/rpc_types_test.go`

- [ ] **Step 1: 创建测试文件骨架**

```go
package app

import (
	"reflect"
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
```

- [ ] **Step 2: 跑这个测试,确认它 FAIL**

Run: `go test ./internal/app/ -run TestTypedArgumentsListOfDTOPreservesElementType -v`

Expected: FAIL,报错 `element kind = "map", want object` 或 `element JavaType = "", want com.x.dto.MaterialItem`。 这是 Issue A 在单元测试里的最小复现。

- [ ] **Step 3: 暂不 commit,后续 Task 把它转 GREEN**

---

## Task 2:`extractGenericArgs` helper + 单元 test

**Files:**
- Modify: `internal/app/rpc_types.go`(末尾追加)
- Modify: `internal/app/rpc_types_test.go`(末尾追加)

- [ ] **Step 1: 在 `rpc_types.go` 末尾追加**

```go
// extractGenericArgs 从形如 "List<Item>" 或 "Map<String, List<Long>>" 的字符串中
// 提取顶层泛型参数。无 `<>` 时返回 nil;每段返回值已 strings.TrimSpace。
// 嵌套泛型由 depth-aware 逗号切分保留为整段(不展开)。
// 假设输入是 well-formed Java-ish type string(来自 schema 解析,
// 不是 user-supplied free text);malformed 输入如 "Map<A, B>>" 或
// "Map<A<B>" 不做完整性校验,可能返回 plausible junk。
func extractGenericArgs(javaType string) []string {
	open := strings.Index(javaType, "<")
	if open < 0 {
		return nil
	}
	close := strings.LastIndex(javaType, ">")
	if close <= open {
		return nil
	}
	inner := javaType[open+1 : close]
	var args []string
	depth := 0
	start := 0
	for i, r := range inner {
		switch r {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(inner[start:i]))
				start = i + 1
			}
		}
	}
	args = append(args, strings.TrimSpace(inner[start:]))
	return args
}
```

- [ ] **Step 2: 在 `rpc_types_test.go` 末尾追加 table-driven test**

```go
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
```

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/app/ -run TestExtractGenericArgs -v`

Expected: PASS。

---

## Task 3:`resolveGenericType` / `resolveBaseType` helpers + wildcard `?` 处理 + 单元 test

**Files:**
- Modify: `internal/app/rpc_types.go`(放在 `rpcParamTypeForMethod` 上方)
- Modify: `internal/app/rpc_types_test.go`

- [ ] **Step 1: 加 `resolveGenericType` 与 `resolveBaseType`**

放在 `rpc_types.go:174 rpcParamTypeForMethod` 上方:

```go
// resolveGenericType 把短名 + 泛型字符串解析成 resolved-with-generics 完整 FQN。
// 例:输入 "List<MaterialItem>" + imports{MaterialItem:"com.x.dto.MaterialItem"}
//     → "java.util.List<com.x.dto.MaterialItem>"
// 嵌套 generic 递归 resolve;无 generic 时退化为 resolveBaseType。
// 数组维度 "[]" 先剥离再 resolve,再原样追加回去。
// Wildcard generic("?", "? extends X", "? super X")显式特判,
// 不走 import resolve,原样保留(避免 fallback 产生 pkg+".?" 这种 garbage FQN)。
func resolveGenericType(typ string, imports map[string]string, pkg string) string {
	typ = strings.TrimSpace(typ)
	typ = strings.TrimPrefix(typ, "final ")
	if typ == "" {
		return typ
	}
	// Wildcard 特判:不 resolve,原样保留。
	// 形态:"?", "? extends X", "? super X"。
	if typ == "?" || strings.HasPrefix(typ, "? ") {
		return typ
	}
	suffix := ""
	for strings.HasSuffix(typ, "[]") {
		suffix += "[]"
		typ = strings.TrimSuffix(typ, "[]")
		typ = strings.TrimSpace(typ)
	}
	open := strings.Index(typ, "<")
	var base, genericRaw string
	if open < 0 {
		base = typ
	} else {
		base = strings.TrimSpace(typ[:open])
		genericRaw = typ[open:]
	}
	resolvedBase := resolveBaseType(base, imports, pkg)
	if genericRaw == "" {
		return resolvedBase + suffix
	}
	args := extractGenericArgs(typ)
	resolved := make([]string, len(args))
	for i, arg := range args {
		resolved[i] = resolveGenericType(arg, imports, pkg)
	}
	return resolvedBase + "<" + strings.Join(resolved, ", ") + ">" + suffix
}

// resolveBaseType 把无泛型的短名解析成 FQN。
// 顺序:Java built-in 映射 → 已带 "." → 显式 import → 同 package fallback。
func resolveBaseType(base string, imports map[string]string, pkg string) string {
	if base == "" {
		return base
	}
	mapped := rpcParamType(base)
	if mapped != base || strings.Contains(mapped, ".") || isPrimitiveRPCType(mapped) {
		return mapped
	}
	if imported, ok := imports[base]; ok {
		return imported
	}
	if pkg != "" {
		return pkg + "." + base
	}
	return base
}
```

- [ ] **Step 2: 加 table-driven test**

```go
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
		// wildcard generic 显式特判
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
```

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/app/ -run TestResolveGenericType -v`

Expected: PASS。 注意 wildcard 6 个 case 全过 —— `? extends X` 内部的 `X` 不被 resolve(整段保留),这是 hessian generic invoke 的 wildcard 通用 fallback 策略。

---

## Task 4:加 value resolver `rpcValueTypeForMethod` / `rpcValueTypeForType`

**Files:**
- Modify: `internal/app/rpc_types.go`(在现有 `rpcParamTypeForMethod` / `rpcFieldTypeForType` 旁边追加)

- [ ] **Step 1: 在 `rpc_types.go` 现有 `rpcParamTypeForMethod`(line 174)上方追加 doc + value 版本**

把 `rpcParamTypeForMethod` 函数前的注释升级,并紧接着追加 `rpcValueTypeForMethod`:

```go
// rpcParamTypeForMethod returns the *identity* form of a parameter type:
// fully-qualified Java class name with all generics erased.
// Used for: method overload matching (sameParamTypes), user-facing method
// signatures (methodSignatures), and wire-level paramTypes (Request.ArgTypes).
// Equivalent in spirit to Java reflection's method.getParameterTypes().
// DO NOT use this when building javavalue trees — element types will be lost.
// For javavalue construction, use rpcValueTypeForMethod.
func rpcParamTypeForMethod(typ string, method schema.Method) string {
	base := eraseRPCGeneric(typ)
	if base == "" {
		return typ
	}
	mapped := rpcParamType(base)
	if mapped != base || strings.Contains(mapped, ".") || isPrimitiveRPCType(mapped) {
		return mapped
	}
	if imported, ok := method.Imports[base]; ok {
		return imported
	}
	if method.Package != "" {
		return method.Package + "." + base
	}
	return base
}

// rpcValueTypeForMethod returns the *value* form of a parameter type:
// fully-qualified Java class name **with generic arguments preserved and
// recursively resolved** (e.g. "java.util.List<com.x.dto.MaterialItem>").
// Used ONLY when constructing javavalue.TypedValue trees that need to know
// nested element/value types for proper hessian serialization.
// MUST NOT leak to wire ArgTypes — hessian writer's eraseJavaType backstops,
// but call sites should never plumb this string into Request.ArgTypes.
func rpcValueTypeForMethod(typ string, method schema.Method) string {
	return resolveGenericType(typ, method.Imports, method.Package)
}
```

`rpcParamTypeForMethod` 函数体本身**不改**(代码完全保留),只是上方加了一段 contract doc 把它的角色钉死成 "identity"。

- [ ] **Step 2: 在 `rpcFieldTypeForType`(line 192)上方做同样的事**

```go
// rpcFieldTypeForType returns the *identity* form of a field type. See
// rpcParamTypeForMethod doc. For javavalue construction inside DTO fields,
// use rpcValueTypeForType.
func rpcFieldTypeForType(typ string, owner schema.TypeSchema) string {
	base := eraseRPCGeneric(typ)
	if base == "" {
		return typ
	}
	mapped := rpcParamType(base)
	if mapped != base || strings.Contains(mapped, ".") || isPrimitiveRPCType(mapped) {
		return mapped
	}
	if imported, ok := owner.Imports[base]; ok {
		return imported
	}
	if owner.Type != "" {
		if lastDot := strings.LastIndex(owner.Type, "."); lastDot > 0 {
			return owner.Type[:lastDot+1] + base
		}
	}
	return base
}

// rpcValueTypeForType returns the *value* form of a field type:
// generic-aware FQN for javavalue tree construction. See rpcValueTypeForMethod.
func rpcValueTypeForType(typ string, owner schema.TypeSchema) string {
	pkg := ""
	if owner.Type != "" {
		if lastDot := strings.LastIndex(owner.Type, "."); lastDot > 0 {
			pkg = owner.Type[:lastDot]
		}
	}
	return resolveGenericType(typ, owner.Imports, pkg)
}
```

`rpcFieldTypeForType` 函数体**不改**。

- [ ] **Step 3: build + 跑除 Task 1 RED test 之外的 internal/app test**

Run: `go build ./... && go test ./internal/app/ -v -skip '^TestTypedArgumentsListOfDTOPreservesElementType$'`

**注意:必须用 `-skip` 不能用 `-run "^(?!...)"`** —— Go test 用的是 RE2,不支持负向 lookahead;`-skip` 是 Go 1.20+ 的官方 way to exclude tests by name pattern。

Expected: **全 PASS**。 这一步只是新增代码,没有 callsite 切换;identity 路径行为不变,现有 test 照常通过。 Task 1 的 RED test 仍 FAIL,本步**不要**跑它 —— GREEN 在 Task 6。

---

## Task 5:把 3 处 value-path callsite 从 identity resolver 切到 value resolver

**Files:**
- Modify: `internal/app/rpc_types.go`(3 处具体行号见下)

只改 3 处,其余 6 处 identity callsite 保持不变。

- [ ] **Step 1: `typedArgumentsForMethod`(line 50-60)**

把 line 55:

```go
javaType = rpcParamTypeForMethod(method.Parameters[i].Type, method)
```

改为:

```go
javaType = rpcValueTypeForMethod(method.Parameters[i].Type, method)
```

- [ ] **Step 2: `typedValueForParam`(line 62-64)**

把 line 63:

```go
return typedValueForJavaType(value, rpcParamTypeForMethod(paramType, method), desc.Types, 0)
```

改为:

```go
return typedValueForJavaType(value, rpcValueTypeForMethod(paramType, method), desc.Types, 0)
```

- [ ] **Step 3: 内部 field type 递归(line 78-92,class 分支)**

在 `typedValueForJavaType` 的 class case 内,找到 line 83:

```go
fieldType := rpcFieldTypeForType(field.Type, typ)
```

改为:

```go
fieldType := rpcValueTypeForType(field.Type, typ)
```

- [ ] **Step 4: build + 跑除 Task 1 RED test 之外的 internal/app test**

Run: `go build ./... && go test ./internal/app/ -v -skip '^TestTypedArgumentsListOfDTOPreservesElementType$'`

Expected: **全 PASS**(identity 路径完全不变 → invoke_test.go 现有 assertion 仍然过)。 Task 1 的 RED test 仍 FAIL,本步**不要**跑它 —— GREEN 在 Task 6 落地。

---

## Task 6:`typedValueForJavaType` list case 用 element type 递归(GREEN)

**Files:**
- Modify: `internal/app/rpc_types.go:109-114`

- [ ] **Step 1: 替换 list case**

把 line 109-114 当前:

```go
case []interface{}:
    items := make([]javavalue.TypedValue, len(raw))
    for i, child := range raw {
        items[i] = typedValueForJavaType(child, "", types, depth+1)
    }
    return javavalue.List(javaType, items)
```

替换为:

```go
case []interface{}:
    elementType := ""
    if args := extractGenericArgs(javaType); len(args) >= 1 {
        elementType = args[0]
    }
    items := make([]javavalue.TypedValue, len(raw))
    for i, child := range raw {
        items[i] = typedValueForJavaType(child, elementType, types, depth+1)
    }
    return javavalue.List(javaType, items)
```

- [ ] **Step 2: 跑 Task 1 的 failing test 确认 GREEN**

Run: `go test ./internal/app/ -run TestTypedArgumentsListOfDTOPreservesElementType -v`

Expected: PASS。

如果还 FAIL:回 Task 4/5 确认 value resolver 真的被调用了,可以加临时 `t.Logf("items.JavaType = %q", items.JavaType)` 排查。

---

## Task 7:`typedValueForJavaType` untyped map case 用 value type 递归 + Map 测试

**Files:**
- Modify: `internal/app/rpc_types.go:101-108`
- Modify: `internal/app/rpc_types_test.go`(末尾追加)

- [ ] **Step 1: 替换 untyped map case**

把 line 101-108 当前:

```go
entries := make([]javavalue.MapEntry, 0, len(raw))
for name, child := range raw {
    entries = append(entries, javavalue.MapEntry{
        Key:   javavalue.Scalar("java.lang.String", name),
        Value: typedValueForJavaType(child, "", types, depth+1),
    })
}
return javavalue.Map(javaType, entries)
```

替换为:

```go
valueType := ""
if args := extractGenericArgs(javaType); len(args) >= 2 {
    valueType = args[1]
}
entries := make([]javavalue.MapEntry, 0, len(raw))
for name, child := range raw {
    entries = append(entries, javavalue.MapEntry{
        Key:   javavalue.Scalar("java.lang.String", name),
        Value: typedValueForJavaType(child, valueType, types, depth+1),
    })
}
return javavalue.Map(javaType, entries)
```

Note:Map key 仍写死 `Scalar("java.lang.String", name)`,本 plan 仅修复 `Map<String, V>`。 非 String key Map(`Map<Long, V>`)留作 follow-up,因为 Go `map[string]interface{}` 没有 key 类型信息。

- [ ] **Step 2: 加 Map<String, DTO> test**

```go
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
```

- [ ] **Step 3: 跑 typedArguments 系列**

Run: `go test ./internal/app/ -run TestTypedArguments -v`

Expected: 全 PASS(list test 在 Task 6 已过,map test 这一步刚加)。

---

## Task 8:`Set<DTO>` 端到端 test(codex finding #4)

**Files:**
- Modify: `internal/app/rpc_types_test.go`

`Set<T>` 当前序列化路径:javavalue 把 `[]interface{}` 包装成 `KindList`,javaType 是 `"java.util.Set<...>"`,hessian writer 看到这个 javaType 写 wire 时 erase 出 `"java.util.Set"`。 这一步**显式 verify** 这个链路真的 work,不靠 hand-wave"搭车修复"假设。

- [ ] **Step 1: 加 Set 端到端 test**

```go
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
```

需要 import `"strings"`(若文件还没 import)。

- [ ] **Step 2: 跑测试**

Run: `go test ./internal/app/ -run TestTypedArgumentsSetOfDTOPreservesElementType -v`

Expected: PASS。 如果 FAIL —— Set 在当前 hessian writer 路径下可能需要特殊处理(比如改成 `java.util.HashSet`),那一步在 Out of Scope 处升级到 follow-up。

---

## Task 9:Wildcard generic 端到端 sanity test(codex finding #5)

**Files:**
- Modify: `internal/app/rpc_types_test.go`

- [ ] **Step 1: 加 wildcard test**

```go
func TestTypedArgumentsWildcardGenericDoesNotCorruptType(t *testing.T) {
	// Plan input has only ResolveType already covered, here verify the
	// typedArgumentsForMethod end-to-end path does not produce a garbage
	// "com.x.facade.?" or similar when method signature uses wildcards.
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
	// 元素 javaType 应保留为 wildcard 原样,不被 corrupt 成 "com.x.facade.?"。
	// 元素是 map → 走 untyped map 兜底,KindMap 是预期(不是 object)。
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
```

- [ ] **Step 2: 跑测试**

Run: `go test ./internal/app/ -run TestTypedArgumentsWildcardGenericDoesNotCorruptType -v`

Expected: PASS。

---

## Task 10:加强 hessian wire boundary test(codex finding #7)

**Files:**
- Modify: `internal/direct/hessian_writer_test.go`(若不存在则创建)

弱测试(只搜 `<>`)被 codex 指出可能漏出错 case。 这一版加强为 **同时正向 assert 期望的 erased class names 出现 + 反向 assert 任何 `<>` 不出现** 的 byte 级 substring 检查。 严格 hessian 协议层 decode 留作 follow-up(收益边际,因为 `eraseJavaType` 实现层已经在 line 132/148/158/337 处统一兜底,positive + negative substring 已能覆盖 95% 回归 case)。

- [ ] **Step 1: 加 test 用例**

```go
func TestWriteTypedValueErasesGenericInWireClassName(t *testing.T) {
	value := javavalue.Object("com.x.dto.MaterialAddRequest", map[string]javavalue.TypedValue{
		"items": javavalue.List("java.util.List<com.x.dto.MaterialItem>", []javavalue.TypedValue{
			javavalue.Object("com.x.dto.MaterialItem", map[string]javavalue.TypedValue{
				"name": javavalue.Scalar("java.lang.String", "alpha"),
			}),
		}),
	})

	w := newWriter()
	if err := w.writeTypedValue(value); err != nil {
		t.Fatalf("write: %v", err)
	}
	wire := string(w.bytes())

	// Wire 应包含 erased 类型名;不应包含带泛型的形式。
	wantClasses := []string{
		"com.x.dto.MaterialAddRequest",
		"java.util.List",
		"com.x.dto.MaterialItem",
	}
	for _, want := range wantClasses {
		if !strings.Contains(wire, want) {
			t.Errorf("wire missing erased class %q", want)
		}
	}
	unwantedClasses := []string{
		"java.util.List<",
		"List<com.x.dto.MaterialItem>",
		"<",
		">",
	}
	for _, bad := range unwantedClasses {
		if strings.Contains(wire, bad) {
			t.Errorf("wire contains unmangled generic chars %q", bad)
		}
	}
}
```

`newWriter()` 和 `(*writer).bytes()` 是 `internal/direct/hessian_writer.go` 已有的 package-private helper(line 30 / 34)。 需要 import `"strings"`、`"testing"`、`"github.com/diandian921/sofarpc-cli/internal/javavalue"`(若还没 import)。

- [ ] **Step 2: 跑测试**

Run: `go test ./internal/direct/ -run TestWriteTypedValueErasesGenericInWireClassName -v`

Expected: PASS。 如果有 `<` 或 `>` 进入 wire —— 说明 hessian writer 有路径漏 `eraseJavaType`,需要回头补该路径。

---

## Task 11:全套 regression + commit

**Files:** all modified

- [ ] **Step 1: 全项目 test 确认全绿**

Run: `go test ./...`

Expected: 全 PASS。 因为 identity 路径完全没动,既有 invoke_test.go / render_test.go / parser_golden_test.go 都不需要任何修改。

- [ ] **Step 2: vet + build**

Run: `go vet ./... && go build ./...`

Expected: 无 warning / error。

- [ ] **Step 3: 看 git status**

Run: `git status && git diff --stat`

预期改动文件(7 个):
```
M internal/app/rpc_types.go
A internal/app/rpc_types_test.go
M internal/direct/hessian_writer_test.go  (或 A 若新建)
A docs/plans/2026-05-26-rpc-types-generic-preservation.md
```

**注意:**`internal/app/invoke.go` 和 `internal/app/invoke_test.go` **没改动**。 如果 diff 显示它们被改了,回 Task 5 检查是不是误改了 identity callsite。

- [ ] **Step 4: 单次 commit**

```bash
git add internal/app/rpc_types.go internal/app/rpc_types_test.go internal/direct/hessian_writer_test.go docs/plans/2026-05-26-rpc-types-generic-preservation.md
git commit -m "$(cat <<'EOF'
fix: 嵌套 DTO List / Map / Set 序列化保留泛型 element 类型

引入 identity vs value 两套 type contract:
- identity (现有 rpcParamTypeForMethod / rpcFieldTypeForType,行为不变):
  erased FQN,用于 overload 匹配、wire ArgTypes、plan output
- value (新增 rpcValueTypeForMethod / rpcValueTypeForType):
  generic-aware FQN,只在 javavalue tree 内部使用

typedValueForJavaType 的 list / map case 用新加的 extractGenericArgs
从带泛型 javaType 提取 element / value type 递归。Wildcard generic
显式特判保留原样,不进 import resolve。Hessian writer 已有
eraseJavaType 作为最后兜底。

修复 List<DTO> / Map<String, DTO> / Set<DTO> 调用时 element 被
序列化成 untyped Map,服务端 ClassCastException → SYS_ERROR 的问题。
EOF
)"
```

Note:per `~/.claude/CLAUDE.md` 全局规则,**不附加 `Co-Authored-By` trailer**。

---

## Verification

完成全部 Task 后:

```bash
go test ./...    # 全 PASS
go vet ./...     # 无 warning
go build ./...   # 无 error
```

新增的 6 个 test 全 PASS:
- `TestExtractGenericArgs`
- `TestResolveGenericType`(含 wildcard 5 个 case)
- `TestTypedArgumentsListOfDTOPreservesElementType`
- `TestTypedArgumentsMapValueDTOPreservesType`
- `TestTypedArgumentsSetOfDTOPreservesElementType`
- `TestTypedArgumentsWildcardGenericDoesNotCorruptType`
- `TestWriteTypedValueErasesGenericInWireClassName`

端到端验证(可选,本地 sofarpc-cli 跑真实 MaterialBgFacade.addMaterials):
- plan dryRun 输出 `arguments[0].fields.items.items[0]` 是 `kind:"object", javaType:"com.x.dto.MaterialItem"`(原 bug 是 `kind:"map", javaType:""`)
- 真打远端,服务端不再抛 ClassCastException

## Out of Scope(留作 follow-up)

- **Map non-String key**(`Map<Long, V>` 等):untyped map 路径下 key 写死 `java.lang.String`,因为 Go `map[string]interface{}` 没有 key 类型信息。 需要 ordered entries 或显式 key type 时再扩展。
- **DTO array element 类型推断不支持**(`Item[]` / `Item[][]`):value path 把 array 当 `[]interface{}` 走 list case,但 array 不走 generic args,所以 element 还是 untyped Map → 服务端反序列化同样 ClassCastException。 codex 二审明确这是 Issue A 的同类 bug 而不只是 paramTypes 问题。 真撞到时 fix 路径:在 list case 加一个 "if javaType 以 `[]` 结尾,取 strip 后的 base 作为 element type"。 当前用户工程没 DTO array 业务,defer。
- **多维 array paramType wire 形态**:Java 反射 `Class.getName()` 对数组返回 JVM 描述符 `[Ljava/util/List;`,sofarpc-cli 当前 paramTypes 直接用 `List[]` 字符串形态,跟 Java 反射不完全一致 —— 但这条**先于本 plan 就存在**,不在本 plan 引入也不在本 plan 修。
- **`>>` / `>>>` token**:当前 plan 的 `extractGenericArgs` 用 `strings.LastIndex(typ, ">")` 找闭合,泛型嵌套 `List<List<X>>` 正常 work(`>>` 解析为两个 `>`)。 如果未来 Schema 解析器输出形如 `List<List<X>>` 紧贴的字符串,extractGenericArgs 仍然 work,本身没有 `>>` token 概念。
