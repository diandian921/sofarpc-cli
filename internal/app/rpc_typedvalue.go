package app

import (
	"strings"

	"github.com/diandian921/sofarpc-mcp/internal/javavalue"
	"github.com/diandian921/sofarpc-mcp/internal/schema"
)

const maxTypePlanDepth = 128

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
