package app

import (
	"fmt"
	"strings"

	"github.com/diandian921/sofarpc-cli/internal/javavalue"
	"github.com/diandian921/sofarpc-cli/internal/schema"
)

const maxTypePlanDepth = 128

func sameParamTypes(method schema.Method, types []string) bool {
	if len(method.Parameters) != len(types) {
		return false
	}
	for i := range method.Parameters {
		if rpcParamTypeForMethod(method.Parameters[i].Type, method) != rpcParamTypeForMethod(types[i], method) {
			return false
		}
	}
	return true
}

func methodSignatures(methods []schema.Method) string {
	out := make([]string, 0, len(methods))
	for _, method := range methods {
		params := make([]string, 0, len(method.Parameters))
		for _, param := range method.Parameters {
			typ := rpcParamTypeForMethod(param.Type, method)
			if param.Name != "" {
				params = append(params, fmt.Sprintf("%s %s", typ, param.Name))
			} else {
				params = append(params, typ)
			}
		}
		out = append(out, fmt.Sprintf("%s(%s)", method.Method, strings.Join(params, ", ")))
	}
	return strings.Join(out, "; ")
}

func rpcParamTypesForMethod(method schema.Method) []string {
	out := make([]string, 0, len(method.Parameters))
	for _, param := range method.Parameters {
		out = append(out, rpcParamTypeForMethod(param.Type, method))
	}
	return out
}

func typedArgumentsForMethod(values []interface{}, method schema.Method, desc schema.Description) []javavalue.TypedValue {
	out := make([]javavalue.TypedValue, len(values))
	for i, value := range values {
		javaType := ""
		if i < len(method.Parameters) {
			javaType = rpcValueTypeForMethod(method.Parameters[i].Type, method, desc.Types)
		}
		out[i] = typedValueForJavaType(value, javaType, desc.Types, 0)
	}
	return out
}

func typedValueForParam(value interface{}, paramType string, method schema.Method, desc schema.Description) javavalue.TypedValue {
	return typedValueForJavaType(value, rpcValueTypeForMethod(paramType, method, desc.Types), desc.Types, 0)
}

func typedValueForJavaType(value interface{}, javaType string, types map[string]schema.TypeSchema, depth int) javavalue.TypedValue {
	if depth > maxTypePlanDepth {
		return javavalue.Scalar(javaType, value)
	}
	// 净化非 FQN 标识(type variable / wildcard),否则下游 untyped Map
	// 兜底分支会把 "T" 当 class 写到 wire 上(codex review 2 抓到的)。
	if isUnresolvedTypeMarker(javaType) {
		javaType = ""
	}
	if isByteArrayType(javaType) {
		return javavalue.Scalar(javaType, value)
	}
	if typ, ok := types[eraseRPCGeneric(javaType)]; ok && typ.Kind == "enum" {
		return enumTypedValue(value, typ.Type)
	}
	switch raw := value.(type) {
	case map[string]interface{}:
		typ, ok := types[eraseRPCGeneric(javaType)]
		if ok && typ.Kind == "class" {
			fields := map[string]javavalue.TypedValue{}
			fieldTypes := map[string]string{}
			for _, field := range typ.Fields {
				fieldType := rpcValueTypeForType(field.Type, typ, types)
				if fieldType != "" {
					fieldTypes[field.Name] = fieldType
				}
			}
			for name, child := range raw {
				fieldType := fieldTypes[name]
				fields[name] = typedValueForJavaType(child, fieldType, types, depth+1)
			}
			return javavalue.Object(typ.Type, fields)
		}
		if shouldWrapJavaObject(javaType) {
			fields := map[string]javavalue.TypedValue{}
			for name, child := range raw {
				fields[name] = typedValueForJavaType(child, "", types, depth+1)
			}
			return javavalue.Object(eraseRPCGeneric(javaType), fields)
		}
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
	default:
		return javavalue.Scalar(javaType, value)
	}
}

func enumTypedValue(value interface{}, javaType string) javavalue.TypedValue {
	if value == nil {
		return javavalue.Scalar(javaType, nil)
	}
	if raw, ok := value.(map[string]interface{}); ok {
		if name, exists := raw["name"]; exists {
			value = name
		}
	}
	return javavalue.Object(javaType, map[string]javavalue.TypedValue{
		"name": javavalue.Scalar("java.lang.String", value),
	})
}

func shouldWrapJavaObject(javaType string) bool {
	base := eraseRPCGeneric(javaType)
	if base == "" || !strings.Contains(base, ".") {
		return false
	}
	if strings.HasPrefix(base, "java.lang.") || strings.HasPrefix(base, "java.math.") || strings.HasPrefix(base, "java.util.") {
		return false
	}
	return true
}

func untypedArguments(values []interface{}, types []string) []javavalue.TypedValue {
	out := make([]javavalue.TypedValue, len(values))
	for i, value := range values {
		javaType := ""
		if i < len(types) {
			javaType = types[i]
		}
		out[i] = typedValueForJavaType(value, javaType, nil, 0)
	}
	return out
}

func needsSchemaAnnotation(types []string) bool {
	for _, typ := range types {
		base := eraseRPCGeneric(typ)
		if base == "" {
			continue
		}
		if isPrimitiveRPCType(base) {
			continue
		}
		if strings.HasPrefix(base, "java.lang.") || strings.HasPrefix(base, "java.math.") || strings.HasPrefix(base, "java.util.") {
			continue
		}
		return true
	}
	return false
}

// resolveGenericType 把短名 + 泛型字符串解析成 resolved-with-generics 完整 FQN。
// 例:输入 "List<MaterialItem>" + imports{MaterialItem:"com.x.dto.MaterialItem"}
//     → "java.util.List<com.x.dto.MaterialItem>"
// 嵌套 generic 递归 resolve;无 generic 时退化为 resolveBaseType。
// 数组维度 "[]" 先剥离再 resolve,再原样追加回去。
// Wildcard generic("?", "? extends X", "? super X")显式特判,
// 不走 import resolve,**整段保留**(连 bound 内部的 X 也不递归)
// —— wildcard element 永远走 untyped Map 兜底,这是预期行为。
// types 用于 same-package lookup:有 schema 的 acronym DTO(URL/XML/ID 等)
// 优先按 schema FQN 解析,只在 schema miss 时才让 type variable 启发式生效。
func resolveGenericType(typ string, imports map[string]string, pkg string, types map[string]schema.TypeSchema, declaredTypeParams []string) string {
	typ = strings.TrimSpace(typ)
	typ = strings.TrimPrefix(typ, "final ")
	if typ == "" {
		return typ
	}
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
	resolvedBase := resolveBaseType(base, imports, pkg, types, declaredTypeParams)
	if genericRaw == "" {
		return resolvedBase + suffix
	}
	args := extractGenericArgs(typ)
	resolved := make([]string, len(args))
	for i, arg := range args {
		resolved[i] = resolveGenericType(arg, imports, pkg, types, declaredTypeParams)
	}
	return resolvedBase + "<" + strings.Join(resolved, ", ") + ">" + suffix
}

// resolveBaseType 把无泛型的短名解析成 FQN。
// 顺序:Java built-in → 已带 "." → 显式 import → declared type params 精确匹配 → same-pkg
// schema lookup → type variable 启发式 fallback → pkg fallback。
//
// declaredTypeParams 是当前 method 或 type 的 declared type parameter 简单名列表
// (`<T, K>` → ["T", "K"])。 在 pkg fallback 之前精确匹配:命中即 return as-is,
// 不进 same-pkg lookup。 根治 [[rpc-types-generic-preservation]] P3(`class Page<T>`
// 同 package 真有 `com.x.dto.T` 类时不被误解析为 DTO)。
//
// declaredTypeParams 为 nil 时(老 schema cache 还没填 TypeParams,或调用方没传)
// 退化为老的启发式行为(`isLikelyTypeVariable`)。
func resolveBaseType(base string, imports map[string]string, pkg string, types map[string]schema.TypeSchema, declaredTypeParams []string) string {
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
	// 精确匹配 declared type params —— same-pkg DTO 同名时按 type var 处理
	for _, tp := range declaredTypeParams {
		if tp == base {
			return base
		}
	}
	if pkg != "" {
		fqn := pkg + "." + base
		if _, ok := types[fqn]; ok {
			return fqn
		}
	}
	if isLikelyTypeVariable(base) {
		return base
	}
	if pkg != "" {
		return pkg + "." + base
	}
	return base
}

// isUnresolvedTypeMarker 识别非 FQN 类型标识(wildcard / type variable),
// typedValueForJavaType 用它判断 javaType 是否应该被清空以走 untyped 兜底。
func isUnresolvedTypeMarker(typ string) bool {
	if typ == "" {
		return false
	}
	if typ == "?" || strings.HasPrefix(typ, "? ") {
		return true
	}
	return isLikelyTypeVariable(typ)
}

// isLikelyTypeVariable 用 Java 命名 convention 启发式识别 type variable。
// Java 强约定 type variable 全大写字母 + 数字,长度 1-3(T / K / V / E / R / T1 / T2)。
// 真实 DTO class 极少这样命名 ——即便像 URL/XML/ID 这种 acronym 被误判,
// 退化效果只是 element fall back 到 untyped Map(不 corrupt wire),
// 不会比"错误 wrap 成 bogus class"更糟。
func isLikelyTypeVariable(s string) bool {
	if len(s) == 0 || len(s) > 3 {
		return false
	}
	hasLetter := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			hasLetter = true
			continue
		}
		if c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return hasLetter
}

// rpcParamTypeForMethod returns the *identity* form of a parameter type:
// fully-qualified Java class name with all generics erased.
// Used for method overload matching (sameParamTypes), user-facing method
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
	// declared type param 精确匹配 → 不 pkg fallback,return as-is
	for _, tp := range method.TypeParams {
		if tp == base {
			return base
		}
	}
	if method.Package != "" {
		return method.Package + "." + base
	}
	return base
}

// rpcValueTypeForMethod returns the *value* form of a parameter type:
// fully-qualified Java class name with generic arguments preserved and
// recursively resolved (e.g. "java.util.List<com.x.dto.MaterialItem>").
// Used ONLY when constructing javavalue.TypedValue trees that need to know
// nested element / value types for proper hessian serialization.
// MUST NOT leak to wire ArgTypes — hessian writer's eraseJavaType backstops,
// but call sites should never plumb this string into Request.ArgTypes.
func rpcValueTypeForMethod(typ string, method schema.Method, types map[string]schema.TypeSchema) string {
	return resolveGenericType(typ, method.Imports, method.Package, types, method.TypeParams)
}

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
	for _, tp := range owner.TypeParams {
		if tp == base {
			return base
		}
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
func rpcValueTypeForType(typ string, owner schema.TypeSchema, types map[string]schema.TypeSchema) string {
	pkg := ""
	if owner.Type != "" {
		if lastDot := strings.LastIndex(owner.Type, "."); lastDot > 0 {
			pkg = owner.Type[:lastDot]
		}
	}
	return resolveGenericType(typ, owner.Imports, pkg, types, owner.TypeParams)
}

func rpcParamType(typ string) string {
	switch typ {
	case "String":
		return "java.lang.String"
	case "Integer":
		return "java.lang.Integer"
	case "Long":
		return "java.lang.Long"
	case "Boolean":
		return "java.lang.Boolean"
	case "Double":
		return "java.lang.Double"
	case "Float":
		return "java.lang.Float"
	case "Short":
		return "java.lang.Short"
	case "Byte":
		return "java.lang.Byte"
	case "Character":
		return "java.lang.Character"
	case "BigDecimal":
		return "java.math.BigDecimal"
	case "Date":
		return "java.util.Date"
	case "List":
		return "java.util.List"
	case "Map":
		return "java.util.Map"
	case "Set":
		return "java.util.Set"
	default:
		return typ
	}
}

func eraseRPCGeneric(typ string) string {
	base := strings.TrimSpace(typ)
	base = strings.TrimPrefix(base, "final ")
	if idx := strings.Index(base, "<"); idx >= 0 {
		base = strings.TrimSpace(base[:idx])
	}
	return strings.TrimSuffix(base, "[]")
}

func isByteArrayType(typ string) bool {
	typ = strings.TrimSpace(typ)
	typ = strings.TrimPrefix(typ, "final ")
	return typ == "byte[]" || typ == "java.lang.Byte[]"
}

func isPrimitiveRPCType(typ string) bool {
	switch typ {
	case "boolean", "byte", "char", "short", "int", "long", "float", "double", "void":
		return true
	default:
		return false
	}
}

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
