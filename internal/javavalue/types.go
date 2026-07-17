package javavalue

import "strings"

// BaseJavaType returns the raw Java type name used for class lookup and Hessian
// scalar dispatch. It removes an optional final modifier, generic arguments, and
// every array dimension from the trusted Java type strings produced by schema.
func BaseJavaType(javaType string) string {
	base := trimFinal(javaType)
	base, _ = splitArrayDimensions(base)
	if generic := strings.IndexByte(base, '<'); generic >= 0 {
		base = strings.TrimSpace(base[:generic])
	}
	return base
}

// IsByteArrayType reports whether javaType is exactly one-dimensional byte[] or
// java.lang.Byte[]. Multi-dimensional arrays are ordinary arrays, not Hessian
// binary values.
func IsByteArrayType(javaType string) bool {
	base, dimensions := splitArrayDimensions(trimFinal(javaType))
	return dimensions == 1 && (base == "byte" || base == "java.lang.Byte")
}

func trimFinal(javaType string) string {
	javaType = strings.TrimSpace(javaType)
	if strings.HasPrefix(javaType, "final") && len(javaType) > len("final") {
		rest := javaType[len("final")]
		if rest == ' ' || rest == '\t' || rest == '\n' || rest == '\r' || rest == '\f' {
			javaType = strings.TrimSpace(javaType[len("final"):])
		}
	}
	return javaType
}

func splitArrayDimensions(javaType string) (string, int) {
	dimensions := 0
	for {
		javaType = strings.TrimSpace(javaType)
		if !strings.HasSuffix(javaType, "]") {
			return javaType, dimensions
		}
		open := strings.LastIndexByte(javaType, '[')
		if open < 0 || strings.TrimSpace(javaType[open+1:len(javaType)-1]) != "" {
			return javaType, dimensions
		}
		javaType = javaType[:open]
		dimensions++
	}
}
