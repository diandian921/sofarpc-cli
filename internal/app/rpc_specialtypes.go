package app

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/diandian921/sofarpc-mcp/internal/javavalue"
)

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

// validateSpecialArgs rejects an argument whose declared java.time or BigInteger
// type failed to encode: a valid one is coerced to the expected object form, so a
// leftover scalar of that type means the input (a malformed ISO date or
// non-integer BigInteger) could not be parsed. Catching it at plan time yields a
// clear ARGUMENT_TYPE_MISMATCH instead of a server-side deserialization error.
// It also validates scalar map keys against their declared generic key type so a
// JSON object key cannot silently remain String when the provider expects Long.
// Recurses into DTO fields, list items, and map entries. Both the schema-coerced
// and the paramTypes / explicit-address paths run their args through the same
// java.time/BigInteger coercion (typedValueForJavaType), so this is correct for
// all of them.
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

// firstMalformedSpecial returns the java type of the first un-coerced special
// value in v (recursing into DTO fields, list items, and map values), or "".
func firstMalformedSpecial(v javavalue.TypedValue) string {
	switch v.Kind {
	case javavalue.KindScalar:
		if v.Scalar == nil {
			return ""
		}
		if isSpecialEncodedType(v.JavaType) {
			return v.JavaType
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
