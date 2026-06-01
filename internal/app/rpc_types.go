package app

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

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
	if tv, ok := javaTimeTypedValue(eraseRPCGeneric(javaType), value); ok {
		return tv
	}
	if tv, ok := bigIntegerTypedValue(eraseRPCGeneric(javaType), value); ok {
		return tv
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
			for _, of := range collectFieldsWithInherited(typ, types, map[string]bool{}) {
				if _, exists := fieldTypes[of.field.Name]; exists {
					// subclass field shadows an inherited one of the same name
					continue
				}
				fieldType := rpcValueTypeForType(of.field.Type, of.owner, types)
				if fieldType != "" {
					fieldTypes[of.field.Name] = fieldType
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

// javaTimeTypedValue encodes a java.time argument supplied as an ISO-8601 string
// into the alipay Hessian jdk8 *Handle proxy the provider expects (the same wire
// form Java writes via writeReplace; the provider's readResolve reconstructs the
// value). Returns false for a non-string or unparseable value so the caller falls
// back to default handling.
func javaTimeTypedValue(javaType string, value interface{}) (javavalue.TypedValue, bool) {
	s, ok := value.(string)
	if !ok {
		return javavalue.TypedValue{}, false
	}
	switch javaType {
	case "java.time.LocalDate":
		if t, err := time.Parse("2006-01-02", s); err == nil {
			return localDateHandle(t), true
		}
	case "java.time.LocalDateTime":
		for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
			if t, err := time.Parse(layout, s); err == nil {
				return localDateTimeHandle(t), true
			}
		}
	case "java.time.Instant":
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return instantHandle(t.UTC()), true
		}
	}
	return javavalue.TypedValue{}, false
}

func javaIntScalar(n int) javavalue.TypedValue {
	return javavalue.Scalar("java.lang.Integer", json.Number(strconv.Itoa(n)))
}

func javaLongScalar(n int64) javavalue.TypedValue {
	return javavalue.Scalar("java.lang.Long", json.Number(strconv.FormatInt(n, 10)))
}

func localDateHandle(t time.Time) javavalue.TypedValue {
	return javavalue.Object("com.caucho.hessian.io.jdk8.LocalDateHandle", map[string]javavalue.TypedValue{
		"year":  javaIntScalar(t.Year()),
		"month": javaIntScalar(int(t.Month())),
		"day":   javaIntScalar(t.Day()),
	})
}

func localTimeHandle(t time.Time) javavalue.TypedValue {
	return javavalue.Object("com.caucho.hessian.io.jdk8.LocalTimeHandle", map[string]javavalue.TypedValue{
		"hour":   javaIntScalar(t.Hour()),
		"minute": javaIntScalar(t.Minute()),
		"second": javaIntScalar(t.Second()),
		"nano":   javaIntScalar(t.Nanosecond()),
	})
}

func localDateTimeHandle(t time.Time) javavalue.TypedValue {
	return javavalue.Object("com.caucho.hessian.io.jdk8.LocalDateTimeHandle", map[string]javavalue.TypedValue{
		"date": localDateHandle(t),
		"time": localTimeHandle(t),
	})
}

func instantHandle(t time.Time) javavalue.TypedValue {
	return javavalue.Object("com.caucho.hessian.io.jdk8.InstantHandle", map[string]javavalue.TypedValue{
		"seconds": javaLongScalar(t.Unix()),
		"nanos":   javaIntScalar(t.Nanosecond()),
	})
}

// bigIntegerTypedValue encodes a java.math.BigInteger argument (given as a string
// or integer JSON number) into BigInteger's serialized signum + mag object form,
// which the provider's Java Hessian reads back as a BigInteger. Returns false for
// non-integer / unparseable values so the caller falls back to default handling.
func bigIntegerTypedValue(javaType string, value interface{}) (javavalue.TypedValue, bool) {
	if javaType != "java.math.BigInteger" {
		return javavalue.TypedValue{}, false
	}
	n, ok := parseBigInt(value)
	if !ok {
		return javavalue.TypedValue{}, false
	}
	return bigIntegerHandle(n), true
}

func parseBigInt(value interface{}) (*big.Int, bool) {
	switch x := value.(type) {
	case string:
		return new(big.Int).SetString(strings.TrimSpace(x), 10)
	case json.Number:
		return new(big.Int).SetString(x.String(), 10)
	case int:
		return big.NewInt(int64(x)), true
	case int64:
		return big.NewInt(x), true
	case float64:
		if x == float64(int64(x)) {
			return big.NewInt(int64(x)), true
		}
	}
	return nil, false
}

func bigIntegerHandle(n *big.Int) javavalue.TypedValue {
	mag := magFromBigInt(n)
	items := make([]javavalue.TypedValue, len(mag))
	for i, w := range mag {
		items[i] = javaIntScalar(int(int32(w)))
	}
	return javavalue.Object("java.math.BigInteger", map[string]javavalue.TypedValue{
		"signum":             javaIntScalar(n.Sign()),
		"bitCount":           javaIntScalar(0),
		"bitLength":          javaIntScalar(0),
		"lowestSetBit":       javaIntScalar(0),
		"firstNonzeroIntNum": javaIntScalar(0),
		"mag":                javavalue.List("[int", items),
	})
}

// validateSpecialArgs rejects a schema-coerced argument whose declared java.time
// or BigInteger type failed to encode: a valid one becomes the expected object
// form, so a leftover scalar of that type means the input (e.g. a malformed ISO
// date or non-integer BigInteger) could not be parsed. Catching it at plan time
// yields a clear ARGUMENT_TYPE_MISMATCH instead of a server-side deserialization
// error. Only call this on coerced args — untyped args keep special types as
// scalars by design and would false-positive.
func validateSpecialArgs(args []javavalue.TypedValue) error {
	for i, a := range args {
		if t := firstMalformedSpecial(a); t != "" {
			return &DomainError{
				Kind:    ErrArgumentTypeMismatch,
				Message: fmt.Sprintf("argument %d is not a valid %s value", i, t),
				Details: map[string]interface{}{"index": i, "type": t},
			}
		}
	}
	return nil
}

// firstMalformedSpecial returns the java type of the first un-coerced special
// value in v (recursing into DTO fields, list items, and map values), or "".
func firstMalformedSpecial(v javavalue.TypedValue) string {
	switch v.Kind {
	case javavalue.KindScalar:
		if v.Scalar != nil && isSpecialEncodedType(v.JavaType) {
			return v.JavaType
		}
	case javavalue.KindObject:
		for _, f := range v.Fields {
			if t := firstMalformedSpecial(f); t != "" {
				return t
			}
		}
	case javavalue.KindList:
		for _, it := range v.Items {
			if t := firstMalformedSpecial(it); t != "" {
				return t
			}
		}
	case javavalue.KindMap:
		for _, e := range v.Entries {
			if t := firstMalformedSpecial(e.Value); t != "" {
				return t
			}
		}
	}
	return ""
}

func isSpecialEncodedType(javaType string) bool {
	switch javaType {
	case "java.time.LocalDate", "java.time.LocalDateTime", "java.time.Instant", "java.math.BigInteger":
		return true
	}
	return false
}

// magFromBigInt returns the big-endian magnitude of n as unsigned 32-bit words
// with no leading-zero word — the shape Java BigInteger.mag uses. Empty for zero.
func magFromBigInt(n *big.Int) []uint32 {
	b := new(big.Int).Abs(n).Bytes()
	if len(b) == 0 {
		return nil
	}
	if pad := (4 - len(b)%4) % 4; pad > 0 {
		b = append(make([]byte, pad), b...)
	}
	words := make([]uint32, len(b)/4)
	for i := range words {
		words[i] = uint32(b[i*4])<<24 | uint32(b[i*4+1])<<16 | uint32(b[i*4+2])<<8 | uint32(b[i*4+3])
	}
	return words
}

// ownedField pairs a field with its declaring type, so an inherited field's type
// resolves against the class that declared it (its own imports/package), not the
// leaf subclass.
type ownedField struct {
	field schema.Field
	owner schema.TypeSchema
}

// collectFieldsWithInherited returns a class's own fields followed by those of its
// superclasses, walked via TypeSchema.Extends and resolved within types, so invoke
// type-coerces inherited fields instead of falling back to an empty type (which
// mis-encodes numbers / dates / enums). seen breaks inheritance cycles. Own fields
// come first so the caller keeps the subclass field when names collide. A
// parameterized superclass (`Child extends Base<OrderStatus>`) is substituted, so
// an inherited field typed as the type variable `T` becomes `OrderStatus`.
func collectFieldsWithInherited(typ schema.TypeSchema, types map[string]schema.TypeSchema, seen map[string]bool) []ownedField {
	return collectFieldsSubst(typ, types, seen, nil)
}

// collectFieldsSubst is collectFieldsWithInherited carrying the type-variable
// binding for the class being visited (nil at the leaf). It applies subst to each
// field type and composes the binding when descending into a generic superclass.
func collectFieldsSubst(typ schema.TypeSchema, types map[string]schema.TypeSchema, seen map[string]bool, subst map[string]string) []ownedField {
	if typ.Type == "" || seen[typ.Type] {
		return nil
	}
	seen[typ.Type] = true
	out := make([]ownedField, 0, len(typ.Fields))
	for _, f := range typ.Fields {
		ft := f.Type
		if len(subst) > 0 {
			ft = substituteTypeVars(ft, subst)
		}
		out = append(out, ownedField{field: schema.Field{Name: f.Name, Type: ft}, owner: typ})
	}
	for _, base := range typ.Extends {
		parent, ok := resolveExtendsType(base, typ, types)
		if !ok {
			continue
		}
		out = append(out, collectFieldsSubst(parent, types, seen, parentSubst(base, parent, typ, types, subst))...)
	}
	return out
}

// parentSubst maps the parent's type-variable names to the concrete types the
// subclass binds them to (`extends Base<OrderStatus>` -> {T: <FQN of OrderStatus>}).
// inbound (the child's own binding) is applied to the extends ref first so nested
// inheritance composes (Child<U> extends Base<U> with U already bound). A concrete
// arg is resolved to its FQN so downstream resolution is context-free; an
// unresolved arg / type variable is left as-is (degrades to the unsubstituted
// behavior). Returns nil when the parent is non-generic or the arity does not line up.
func parentSubst(baseRef string, parent, child schema.TypeSchema, types map[string]schema.TypeSchema, inbound map[string]string) map[string]string {
	if len(parent.TypeParams) == 0 {
		return nil
	}
	ref := baseRef
	if len(inbound) > 0 {
		ref = substituteTypeVars(ref, inbound)
	}
	args := extractGenericArgs(ref)
	if len(args) != len(parent.TypeParams) {
		return nil
	}
	m := make(map[string]string, len(args))
	for i, p := range parent.TypeParams {
		arg := strings.TrimSpace(args[i])
		if resolved, ok := resolveExtendsType(arg, child, types); ok && resolved.Type != "" {
			arg = resolved.Type
		}
		m[p] = arg
	}
	return m
}

// substituteTypeVars replaces whole-identifier type variables in a type string
// using subst, e.g. {T: OrderStatus} turns "List<T>" into "List<OrderStatus>".
// Dotted FQNs are single tokens, so a single-letter key never matches inside one.
func substituteTypeVars(typ string, subst map[string]string) string {
	var b, ident strings.Builder
	flush := func() {
		if ident.Len() == 0 {
			return
		}
		s := ident.String()
		ident.Reset()
		if repl, ok := subst[s]; ok {
			b.WriteString(repl)
		} else {
			b.WriteString(s)
		}
	}
	for _, r := range typ {
		if isIdentRune(r) {
			ident.WriteRune(r)
			continue
		}
		flush()
		b.WriteRune(r)
	}
	flush()
	return b.String()
}

func isIdentRune(r rune) bool {
	return r == '_' || r == '$' || r == '.' ||
		(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// resolveExtendsType resolves a superclass ref (as written, e.g. "BaseDTO" or a
// generic "Base<String>") to its TypeSchema within types, using the subclass's
// imports/package — mirroring schema.resolveType over the described type map.
func resolveExtendsType(ref string, owner schema.TypeSchema, types map[string]schema.TypeSchema) (schema.TypeSchema, bool) {
	base := eraseRPCGeneric(ref)
	if t, ok := types[base]; ok {
		return t, true
	}
	if strings.Contains(base, ".") {
		return schema.TypeSchema{}, false
	}
	if fqn, ok := owner.Imports[base]; ok {
		if t, ok := types[fqn]; ok {
			return t, true
		}
	}
	if lastDot := strings.LastIndex(owner.Type, "."); lastDot > 0 {
		if t, ok := types[owner.Type[:lastDot]+"."+base]; ok {
			return t, true
		}
	}
	return schema.TypeSchema{}, false
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
