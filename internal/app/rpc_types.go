package app

import (
	"fmt"
	"strings"

	"github.com/diandian921/sofarpc-mcp/internal/javavalue"
	"github.com/diandian921/sofarpc-mcp/internal/schema"
)

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

// RPCParamTypes returns the normalized RPC identity parameter types for a method — the
// same wire argTypes the ordered-invocation planner uses. Search/describe candidates
// advertise these so an agent copying paramTypes into sofarpc_invoke gets
// "java.lang.String" or the erased FQN, not a raw source token like "String" or "List<Foo>".
func RPCParamTypes(method schema.Method) []string {
	return rpcParamTypesForMethod(method)
}

// rpcTypeResolver is the single owner of Java source type resolution inside app.
// identity returns the erased wire/method identity while value preserves generic
// arguments for javavalue construction. Both forms share the same base-name
// precedence, so imports, type parameters, and same-package lookup cannot drift.
type rpcTypeResolver struct {
	imports            map[string]string
	packageName        string
	declaredTypeParams []string
	knownTypes         map[string]schema.TypeSchema
}

func (r rpcTypeResolver) identity(javaType string) string {
	base := javavalue.BaseJavaType(javaType)
	if base == "" {
		return javaType
	}
	return r.resolveBase(base)
}

// value 把短名 + 泛型字符串解析成 resolved-with-generics 完整 FQN。
// Wildcard generic("?", "? extends X", "? super X")整段保留,让调用方安全
// 降级为 untyped value;数组维度在递归解析后原样追加。
func (r rpcTypeResolver) value(javaType string) string {
	typ := strings.TrimSpace(javaType)
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
	resolvedBase := r.resolveBase(base)
	if genericRaw == "" {
		return resolvedBase + suffix
	}
	args := extractGenericArgs(typ)
	resolved := make([]string, len(args))
	for i, arg := range args {
		resolved[i] = r.value(arg)
	}
	return resolvedBase + "<" + strings.Join(resolved, ", ") + ">" + suffix
}

// resolveBase 把无泛型的短名解析成 FQN。
// 顺序:Java built-in → 已带 "." → 显式 import → declared type params 精确匹配 → same-pkg
// schema lookup → type variable 启发式 fallback → pkg fallback。
func (r rpcTypeResolver) resolveBase(base string) string {
	if base == "" {
		return base
	}
	mapped := rpcParamType(base)
	if mapped != base || strings.Contains(mapped, ".") || isPrimitiveRPCType(mapped) {
		return mapped
	}
	if imported, ok := r.imports[base]; ok {
		return imported
	}
	// 精确匹配 declared type params —— same-pkg DTO 同名时按 type var 处理
	for _, tp := range r.declaredTypeParams {
		if tp == base {
			return base
		}
	}
	if r.packageName != "" {
		fqn := r.packageName + "." + base
		if _, ok := r.knownTypes[fqn]; ok {
			return fqn
		}
	}
	if isLikelyTypeVariable(base) {
		return base
	}
	if r.packageName != "" {
		return r.packageName + "." + base
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

// isLikelyTypeVariable conservatively recognizes one uppercase letter followed
// only by optional digits (T, K, T1). Acronym class names such as ID and URL are
// not type variables and must continue through same-package resolution.
func isLikelyTypeVariable(s string) bool {
	if len(s) == 0 || s[0] < 'A' || s[0] > 'Z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func rpcTypeResolverForMethod(method schema.Method, knownTypes map[string]schema.TypeSchema) rpcTypeResolver {
	return rpcTypeResolver{
		imports:            method.Imports,
		packageName:        method.Package,
		declaredTypeParams: method.TypeParams,
		knownTypes:         knownTypes,
	}
}

func rpcTypeResolverForType(owner schema.TypeSchema, knownTypes map[string]schema.TypeSchema) rpcTypeResolver {
	pkg := ""
	if lastDot := strings.LastIndex(owner.Type, "."); lastDot > 0 {
		pkg = owner.Type[:lastDot]
	}
	return rpcTypeResolver{
		imports:            owner.Imports,
		packageName:        pkg,
		declaredTypeParams: owner.TypeParams,
		knownTypes:         knownTypes,
	}
}

// rpcParamTypeForMethod returns the *identity* form of a parameter type:
// fully-qualified Java class name with all generics erased.
// Used for method overload matching (sameParamTypes), user-facing method
// signatures (methodSignatures), and wire-level paramTypes (Request.ArgTypes).
// Equivalent in spirit to Java reflection's method.getParameterTypes().
// DO NOT use this when building javavalue trees — element types will be lost.
// For javavalue construction, use rpcValueTypeForMethod.
func rpcParamTypeForMethod(typ string, method schema.Method) string {
	return rpcTypeResolverForMethod(method, nil).identity(typ)
}

// rpcValueTypeForMethod returns the *value* form of a parameter type:
// fully-qualified Java class name with generic arguments preserved and
// recursively resolved (e.g. "java.util.List<com.x.dto.MaterialItem>").
// Used ONLY when constructing javavalue.TypedValue trees that need to know
// nested element / value types for proper hessian serialization.
// MUST NOT leak to wire ArgTypes — hessian writer's BaseJavaType backstops,
// but call sites should never plumb this string into Request.ArgTypes.
func rpcValueTypeForMethod(typ string, method schema.Method, types map[string]schema.TypeSchema) string {
	return rpcTypeResolverForMethod(method, types).value(typ)
}

// rpcFieldTypeForType returns the *identity* form of a field type. See
// rpcParamTypeForMethod doc. For javavalue construction inside DTO fields,
// use rpcValueTypeForType.
func rpcFieldTypeForType(typ string, owner schema.TypeSchema) string {
	return rpcTypeResolverForType(owner, nil).identity(typ)
}

// rpcValueTypeForType returns the *value* form of a field type:
// generic-aware FQN for javavalue tree construction. See rpcValueTypeForMethod.
func rpcValueTypeForType(typ string, owner schema.TypeSchema, types map[string]schema.TypeSchema) string {
	return rpcTypeResolverForType(owner, types).value(typ)
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
	case "BigInteger":
		return "java.math.BigInteger"
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
	closeIdx := strings.LastIndex(javaType, ">")
	if closeIdx <= open {
		return nil
	}
	inner := javaType[open+1 : closeIdx]
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
