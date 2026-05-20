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
			javaType = rpcParamTypeForMethod(method.Parameters[i].Type, method)
		}
		out[i] = typedValueForJavaType(value, javaType, desc.Types, 0)
	}
	return out
}

func typedValueForParam(value interface{}, paramType string, method schema.Method, desc schema.Description) javavalue.TypedValue {
	return typedValueForJavaType(value, rpcParamTypeForMethod(paramType, method), desc.Types, 0)
}

func typedValueForJavaType(value interface{}, javaType string, types map[string]schema.TypeSchema, depth int) javavalue.TypedValue {
	if depth > maxTypePlanDepth {
		return javavalue.Scalar(javaType, value)
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
				fieldType := rpcFieldTypeForType(field.Type, typ)
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
		entries := make([]javavalue.MapEntry, 0, len(raw))
		for name, child := range raw {
			entries = append(entries, javavalue.MapEntry{
				Key:   javavalue.Scalar("java.lang.String", name),
				Value: typedValueForJavaType(child, "", types, depth+1),
			})
		}
		return javavalue.Map(javaType, entries)
	case []interface{}:
		items := make([]javavalue.TypedValue, len(raw))
		for i, child := range raw {
			items[i] = typedValueForJavaType(child, "", types, depth+1)
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
