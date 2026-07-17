package direct

import (
	"encoding/hex"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/diandian921/sofarpc-mcp/internal/javavalue"
)

// This file pins the exact bytes the writer produces for the java.time and
// BigInteger "special" types. Today the app layer builds the caucho *Handle /
// signum-mag object tree and hands it down; the pushdown (see
// docs/special-type-encoding-pushdown-design.md) will move that shaping into the
// direct writer so the app can emit a neutral Scalar instead. These are
// characterization goldens of the CURRENT (object-form) encoding: whatever path
// produces the value, the wire bytes must stay byte-identical, so the refactor is
// verified in normal CI (no JVM) by re-encoding the scalar form to the same hex.
//
// The hex is Go's own deterministic output (writeTypedValue sorts object fields),
// which differs from the Java-oracle decode goldens in hessian_golden_test.go
// (Java's field order); both are valid Hessian for the same value.

func specialInt(n int) javavalue.TypedValue {
	return javavalue.Scalar("java.lang.Integer", json.Number(strconv.Itoa(n)))
}

func specialLong(n int64) javavalue.TypedValue {
	return javavalue.Scalar("java.lang.Long", json.Number(strconv.FormatInt(n, 10)))
}

// objectFormLocalDate etc. mirror the app layer's current handle construction
// (internal/app/rpc_specialtypes.go) so this test is the pre-refactor baseline.
func objectFormLocalDate(year, month, day int) javavalue.TypedValue {
	return javavalue.Object("com.caucho.hessian.io.jdk8.LocalDateHandle", map[string]javavalue.TypedValue{
		"year":  specialInt(year),
		"month": specialInt(month),
		"day":   specialInt(day),
	})
}

func objectFormLocalTime(hour, minute, second, nano int) javavalue.TypedValue {
	return javavalue.Object("com.caucho.hessian.io.jdk8.LocalTimeHandle", map[string]javavalue.TypedValue{
		"hour":   specialInt(hour),
		"minute": specialInt(minute),
		"second": specialInt(second),
		"nano":   specialInt(nano),
	})
}

func encodeHex(t *testing.T, v javavalue.TypedValue) string {
	t.Helper()
	w := newWriter()
	if err := w.writeValue(v); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return hex.EncodeToString(w.bytes())
}

func TestSpecialTypeEncodingGolden(t *testing.T) {
	// mag words for 0x7fffffffffffffff (max int64): [0x7fffffff, 0xffffffff],
	// each stored as a signed int32 word (0xffffffff -> -1), matching magFromBigInt.
	bigIntMag := javavalue.List("[int", []javavalue.TypedValue{
		specialInt(2147483647),
		specialInt(-1),
	})
	cases := []struct {
		name  string
		value javavalue.TypedValue
		want  string
	}{
		{
			name:  "local-date",
			value: objectFormLocalDate(2024, 1, 15),
			want:  "4f490000002a636f6d2e63617563686f2e6865737369616e2e696f2e6a646b382e4c6f63616c4461746548616e646c65490000000303646179056d6f6e746804796561726f4900000000490000000f490000000149000007e8",
		},
		{
			name:  "local-time",
			value: objectFormLocalTime(10, 30, 0, 0),
			want:  "4f490000002a636f6d2e63617563686f2e6865737369616e2e696f2e6a646b382e4c6f63616c54696d6548616e646c65490000000404686f7572066d696e757465046e616e6f067365636f6e646f4900000000490000000a490000001e49000000004900000000",
		},
		{
			name: "local-date-time",
			value: javavalue.Object("com.caucho.hessian.io.jdk8.LocalDateTimeHandle", map[string]javavalue.TypedValue{
				"date": objectFormLocalDate(2024, 1, 15),
				"time": objectFormLocalTime(10, 30, 0, 0),
			}),
			want: "4f490000002e636f6d2e63617563686f2e6865737369616e2e696f2e6a646b382e4c6f63616c4461746554696d6548616e646c65490000000204646174650474696d656f49000000004f490000002a636f6d2e63617563686f2e6865737369616e2e696f2e6a646b382e4c6f63616c4461746548616e646c65490000000303646179056d6f6e746804796561726f4900000001490000000f490000000149000007e84f490000002a636f6d2e63617563686f2e6865737369616e2e696f2e6a646b382e4c6f63616c54696d6548616e646c65490000000404686f7572066d696e757465046e616e6f067365636f6e646f4900000002490000000a490000001e49000000004900000000",
		},
		{
			name: "instant",
			value: javavalue.Object("com.caucho.hessian.io.jdk8.InstantHandle", map[string]javavalue.TypedValue{
				"seconds": specialLong(1705314600),
				"nanos":   specialInt(0),
			}),
			want: "4f4900000028636f6d2e63617563686f2e6865737369616e2e696f2e6a646b382e496e7374616e7448616e646c654900000002056e616e6f73077365636f6e64736f490000000049000000004c0000000065a50928",
		},
		{
			name: "big-integer",
			value: javavalue.Object("java.math.BigInteger", map[string]javavalue.TypedValue{
				"signum":             specialInt(1),
				"bitCount":           specialInt(0),
				"bitLength":          specialInt(0),
				"lowestSetBit":       specialInt(0),
				"firstNonzeroIntNum": specialInt(0),
				"mag":                bigIntMag,
			}),
			want: "4f49000000146a6176612e6d6174682e426967496e7465676572490000000608626974436f756e74096269744c656e6774681266697273744e6f6e7a65726f496e744e756d0c6c6f77657374536574426974036d6167067369676e756d6f49000000004900000000490000000049000000004900000000567400045b696e746e02497fffffff49ffffffff7a4900000001",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := encodeHex(t, tc.value)
			if got != tc.want {
				t.Fatalf("%s encode hex:\n got  %s\n want %s", tc.name, got, tc.want)
			}
		})
	}
}
