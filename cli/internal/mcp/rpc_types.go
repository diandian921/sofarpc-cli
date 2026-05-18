package mcp

import (
	"fmt"
	"strings"

	"github.com/sofarpc/cli/internal/schema"
)

const maxTypeAnnotationDepth = 128

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

func annotateArgumentsForMethod(values []interface{}, method schema.Method, desc schema.Description) []interface{} {
	out := make([]interface{}, len(values))
	for i, value := range values {
		out[i] = value
		if i < len(method.Parameters) {
			out[i] = annotateArgumentForParam(value, method.Parameters[i].Type, method, desc)
		}
	}
	return out
}

func annotateArgumentForParam(value interface{}, paramType string, method schema.Method, desc schema.Description) interface{} {
	return annotateValueForJavaType(value, rpcParamTypeForMethod(paramType, method), desc.Types)
}

func annotateValueForJavaType(value interface{}, javaType string, types map[string]schema.TypeSchema) interface{} {
	return annotateValueForJavaTypeDepth(value, javaType, types, 0)
}

func annotateValueForJavaTypeDepth(value interface{}, javaType string, types map[string]schema.TypeSchema, depth int) interface{} {
	if depth > maxTypeAnnotationDepth {
		return value
	}
	raw, ok := value.(map[string]interface{})
	if !ok {
		return value
	}
	typ, ok := types[eraseRPCGeneric(javaType)]
	if !ok || typ.Kind != "class" {
		return value
	}
	fieldTypes := map[string]string{}
	out := make(map[string]interface{}, len(raw)+2)
	for k, v := range raw {
		out[k] = v
	}
	if _, ok := out["@type"]; !ok {
		if _, ok := out["__type"]; !ok {
			out["@type"] = typ.Type
		}
	}
	for _, field := range typ.Fields {
		fieldType := rpcFieldTypeForType(field.Type, typ)
		if fieldType == "" {
			continue
		}
		fieldTypes[field.Name] = fieldType
		if child, ok := out[field.Name]; ok {
			out[field.Name] = annotateValueForJavaTypeDepth(child, fieldType, types, depth+1)
		}
	}
	if len(fieldTypes) > 0 {
		out["__fieldTypes"] = fieldTypes
	}
	return out
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

func isPrimitiveRPCType(typ string) bool {
	switch typ {
	case "boolean", "byte", "char", "short", "int", "long", "float", "double", "void":
		return true
	default:
		return false
	}
}
