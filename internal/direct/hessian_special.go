package direct

import (
	"encoding/json"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/diandian921/sofarpc-mcp/internal/javavalue"
)

// This file owns the Hessian wire shapes of the java.time and BigInteger types:
// the alipay Hessian jdk8 *Handle proxies (the form Java writes via writeReplace
// and reconstructs via readResolve) and BigInteger's signum/mag object. The app
// layer hands these down as a neutral javavalue.Scalar(javaType, canonicalString)
// and this codec-level code turns them into the object tree, mirroring how Date
// and BigDecimal are already handled in writeJavaScalar.
//
// The builders produce a javavalue.Object and the caller writes it through
// writeTypedValue, so the object goes through the same field-sorting path as any
// other object and the bytes are identical to the previous app-built form
// (pinned by TestSpecialTypeEncodingGolden).

// CanonicalizeSpecialScalar validates a java.time / BigInteger value and returns
// its canonical string form — the exact form the writer re-parses when encoding
// (java.time: the trimmed ISO-8601 string; BigInteger: the decimal string). It
// returns supported=false for an ordinary Java type, and supported=true with
// valid=false when javaType is special but value is malformed. The explicit
// three-state result lets callers discover support without maintaining a second
// type list. Reusing the writer's own parse guarantees "valid" implies "the
// writer will encode it".
func CanonicalizeSpecialScalar(javaType string, value interface{}) (canonical string, supported, valid bool) {
	switch javaType {
	case "java.time.LocalDate", "java.time.LocalDateTime", "java.time.LocalTime", "java.time.Instant":
		s, ok := value.(string)
		if !ok {
			return "", true, false
		}
		s = strings.TrimSpace(s)
		if _, ok := javaTimeHandleValue(javaType, s); ok {
			return s, true, true
		}
		return "", true, false
	case "java.math.BigInteger":
		n, ok := parseBigInt(value)
		if !ok {
			return "", true, false
		}
		return n.String(), true, true
	}
	return "", false, false
}

func hessianIntField(n int) javavalue.TypedValue {
	return javavalue.Scalar("java.lang.Integer", json.Number(strconv.Itoa(n)))
}

func hessianLongField(n int64) javavalue.TypedValue {
	return javavalue.Scalar("java.lang.Long", json.Number(strconv.FormatInt(n, 10)))
}

// javaTimeHandleValue builds the *Handle object for a java.time scalar given as
// an ISO-8601 string. Returns false for a non-string or unparseable value so the
// caller can surface an encode error.
func javaTimeHandleValue(javaType string, value interface{}) (javavalue.TypedValue, bool) {
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
	case "java.time.LocalTime":
		for _, layout := range []string{"15:04:05.999999999", "15:04:05", "15:04"} {
			if t, err := time.Parse(layout, s); err == nil {
				return localTimeHandle(t), true
			}
		}
	case "java.time.Instant":
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return instantHandle(t.UTC()), true
		}
	}
	return javavalue.TypedValue{}, false
}

func localDateHandle(t time.Time) javavalue.TypedValue {
	return javavalue.Object("com.caucho.hessian.io.jdk8.LocalDateHandle", map[string]javavalue.TypedValue{
		"year":  hessianIntField(t.Year()),
		"month": hessianIntField(int(t.Month())),
		"day":   hessianIntField(t.Day()),
	})
}

func localTimeHandle(t time.Time) javavalue.TypedValue {
	return javavalue.Object("com.caucho.hessian.io.jdk8.LocalTimeHandle", map[string]javavalue.TypedValue{
		"hour":   hessianIntField(t.Hour()),
		"minute": hessianIntField(t.Minute()),
		"second": hessianIntField(t.Second()),
		"nano":   hessianIntField(t.Nanosecond()),
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
		"seconds": hessianLongField(t.Unix()),
		"nanos":   hessianIntField(t.Nanosecond()),
	})
}

// bigIntegerHandleValue builds BigInteger's serialized signum + mag object from a
// decimal string / integer value. Returns false for a non-integer / unparseable
// value so the caller can surface an encode error.
func bigIntegerHandleValue(value interface{}) (javavalue.TypedValue, bool) {
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
		items[i] = hessianIntField(int(int32(w)))
	}
	return javavalue.Object("java.math.BigInteger", map[string]javavalue.TypedValue{
		"signum":             hessianIntField(n.Sign()),
		"bitCount":           hessianIntField(0),
		"bitLength":          hessianIntField(0),
		"lowestSetBit":       hessianIntField(0),
		"firstNonzeroIntNum": hessianIntField(0),
		"mag":                javavalue.List("[int", items),
	})
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
