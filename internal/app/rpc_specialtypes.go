package app

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/diandian921/sofarpc-mcp/internal/direct"
	"github.com/diandian921/sofarpc-mcp/internal/javavalue"
)

// This file is plan-time validation only. The Hessian wire shapes of the
// java.time / BigInteger types (caucho *Handle, signum/mag) live in the direct
// (codec) layer; the app layer stays type-neutral and validates a value against
// its declared special type via direct.CanonicalizeSpecialScalar, turning a bad
// value into an ARGUMENT_TYPE_MISMATCH before any request is sent.

// validateSpecialArgs rejects an argument whose declared java.time or BigInteger
// type failed to canonicalize (a malformed ISO date or non-integer BigInteger).
// Catching it at plan time yields a clear ARGUMENT_TYPE_MISMATCH instead of a
// server-side deserialization error. It also validates scalar map keys against
// their declared generic key type so a JSON object key cannot silently remain
// String when the provider expects Long. Recurses into DTO fields, list items,
// and map entries. Both the schema-coerced and the paramTypes / explicit-address
// paths run their args through the same coercion (typedValueForJavaType), so this
// is correct for all of them.
func validateSpecialArgs(args []javavalue.TypedValue) error {
	for i, a := range args {
		if t := firstMalformedSpecial(a); t != "" {
			return &DomainError{
				Kind:    ErrArgumentTypeMismatch,
				Message: fmt.Sprintf("argument %d is not a valid %s value", i, t),
				Details: map[string]interface{}{"index": i, "type": t},
			}
		}
		if typ, value, ok := firstInvalidMapKey(a); ok {
			return &DomainError{
				Kind:    ErrArgumentTypeMismatch,
				Message: fmt.Sprintf("argument %d map key %q is not a valid %s value", i, value, typ),
				Details: map[string]interface{}{"index": i, "type": typ, "key": value},
			}
		}
	}
	return nil
}

func firstInvalidMapKey(v javavalue.TypedValue) (string, string, bool) {
	switch v.Kind {
	case javavalue.KindObject:
		for _, field := range v.Fields {
			if typ, value, bad := firstInvalidMapKey(field); bad {
				return typ, value, true
			}
		}
	case javavalue.KindList:
		for _, item := range v.Items {
			if typ, value, bad := firstInvalidMapKey(item); bad {
				return typ, value, true
			}
		}
	case javavalue.KindMap:
		for _, entry := range v.Entries {
			if entry.Key.Kind == javavalue.KindScalar && !validMapKeyScalar(entry.Key.JavaType, entry.Key.Scalar) {
				return entry.Key.JavaType, fmt.Sprint(entry.Key.Scalar), true
			}
			if typ, value, bad := firstInvalidMapKey(entry.Value); bad {
				return typ, value, true
			}
		}
	}
	return "", "", false
}

func validMapKeyScalar(javaType string, value interface{}) bool {
	if value == nil {
		return true
	}
	s := strings.TrimSpace(fmt.Sprint(value))
	switch rpcParamType(javavalue.BaseJavaType(javaType)) {
	case "byte", "java.lang.Byte":
		_, err := strconv.ParseInt(s, 10, 8)
		return err == nil
	case "short", "java.lang.Short":
		_, err := strconv.ParseInt(s, 10, 16)
		return err == nil
	case "int", "java.lang.Integer":
		_, err := strconv.ParseInt(s, 10, 32)
		return err == nil
	case "long", "java.lang.Long":
		_, err := strconv.ParseInt(s, 10, 64)
		return err == nil
	case "float", "java.lang.Float":
		_, err := strconv.ParseFloat(s, 32)
		return err == nil
	case "double", "java.lang.Double":
		_, err := strconv.ParseFloat(s, 64)
		return err == nil
	case "boolean", "java.lang.Boolean":
		_, err := strconv.ParseBool(s)
		return err == nil
	case "char", "java.lang.Character":
		return len([]rune(s)) == 1
	case "java.util.Date":
		return isEpochMillisScalar(value)
	default:
		return true
	}
}

// firstMalformedSpecial returns the java type of the first special-typed scalar
// that fails validation (recursing into DTO fields, list items, and map entries),
// or "". A special value is now carried as a neutral scalar, so "malformed" means
// direct.CanonicalizeSpecialScalar reports it as supported but invalid; a valid
// one canonicalizes cleanly. Ordinary scalar types report supported=false.
func firstMalformedSpecial(v javavalue.TypedValue) string {
	switch v.Kind {
	case javavalue.KindScalar:
		if v.Scalar == nil {
			return ""
		}
		if _, supported, valid := direct.CanonicalizeSpecialScalar(v.JavaType, v.Scalar); supported {
			if !valid {
				return v.JavaType
			}
			return ""
		}
		if v.JavaType == "java.util.Date" && !isEpochMillisScalar(v.Scalar) {
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
			// Keys now carry their declared type too, so validate both sides.
			if t := firstMalformedSpecial(e.Key); t != "" {
				return t
			}
			if t := firstMalformedSpecial(e.Value); t != "" {
				return t
			}
		}
	}
	return ""
}

func isEpochMillisScalar(value interface{}) bool {
	switch x := value.(type) {
	case json.Number:
		if _, err := x.Int64(); err == nil {
			return true
		}
		if f, err := x.Float64(); err == nil {
			return math.Trunc(f) == f && f >= math.MinInt64 && f <= math.MaxInt64
		}
	case int, int8, int16, int32, int64:
		return true
	case uint:
		return uint64(x) <= math.MaxInt64
	case uint8, uint16, uint32:
		return true
	case uint64:
		return x <= math.MaxInt64
	case float32:
		f := float64(x)
		return math.Trunc(f) == f && f >= math.MinInt64 && f <= math.MaxInt64
	case float64:
		return math.Trunc(x) == x && x >= math.MinInt64 && x <= math.MaxInt64
	case string:
		_, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return err == nil
	}
	return false
}
