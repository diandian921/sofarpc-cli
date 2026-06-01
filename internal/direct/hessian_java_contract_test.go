//go:build hessian_oracle

package direct

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/diandian921/sofarpc-cli/internal/javavalue"
)

type javaHessianContract struct {
	java      string
	classpath string
}

var (
	javaContractOnce    sync.Once
	javaContractCache   javaHessianContract
	javaContractSkip    string
	javaContractErr     error
	javaContractTempDir string
)

func TestMain(m *testing.M) {
	code := m.Run()
	if javaContractTempDir != "" {
		_ = os.RemoveAll(javaContractTempDir)
	}
	os.Exit(code)
}

func TestHessianJavaContractGoEncodedValuesReadableByJava(t *testing.T) {
	contract := requireJavaHessianContract(t)

	cases := []struct {
		name  string
		value javavalue.TypedValue
		mode  string
		want  string
	}{
		{
			name:  "integer",
			value: javavalue.Scalar("java.lang.Integer", json.Number("5")),
			mode:  "decode-any",
			want:  "java.lang.Integer:5",
		},
		{
			name:  "long",
			value: javavalue.Scalar("java.lang.Long", json.Number("433905635109773312")),
			mode:  "decode-any",
			want:  "java.lang.Long:433905635109773312",
		},
		{
			name:  "double whole number",
			value: javavalue.Scalar("java.lang.Double", json.Number("2.0")),
			mode:  "decode-any",
			want:  "java.lang.Double:2.0",
		},
		{
			name:  "utf16 string",
			value: javavalue.Scalar("java.lang.String", "a🙂b"),
			mode:  "decode-any",
			want:  "java.lang.String:a🙂b",
		},
		{
			name: "list with null",
			value: javavalue.List("java.util.ArrayList", []javavalue.TypedValue{
				javavalue.Scalar("", nil),
				javavalue.Scalar("java.lang.Long", json.Number("1")),
				javavalue.Scalar("java.lang.String", "two"),
			}),
			mode: "decode-any",
			want: "List[null,java.lang.Long:1,java.lang.String:two]",
		},
		{
			name: "map long key",
			value: javavalue.Map("java.util.LinkedHashMap", []javavalue.MapEntry{
				{Key: javavalue.Scalar("java.lang.Long", json.Number("7")), Value: javavalue.Scalar("java.lang.String", "seven")},
				{Key: javavalue.Scalar("java.lang.String", "name"), Value: javavalue.Scalar("java.lang.String", "alice")},
			}),
			mode: "decode-any",
			want: "Map{java.lang.Long:7=java.lang.String:seven,java.lang.String:name=java.lang.String:alice}",
		},
		{
			name:  "big decimal",
			value: javavalue.Scalar("java.math.BigDecimal", "1000.50"),
			mode:  "decode-any",
			want:  "java.math.BigDecimal:1000.50",
		},
		{
			name:  "byte array",
			value: javavalue.Scalar("byte[]", []interface{}{json.Number("1"), json.Number("2"), json.Number("255")}),
			mode:  "decode-any",
			want:  "byte[]:0102ff",
		},
		{
			name: "enum",
			value: javavalue.Object("HessianContractHelper$Status", map[string]javavalue.TypedValue{
				"name": javavalue.Scalar("java.lang.String", "ACTIVE"),
			}),
			mode: "decode-status",
			want: "Status:ACTIVE",
		},
		{
			name:  "null enum",
			value: javavalue.Scalar("HessianContractHelper$Status", nil),
			mode:  "decode-status",
			want:  "null",
		},
		{
			name: "dto",
			value: javavalue.Object("HessianContractHelper$QueryRequest", map[string]javavalue.TypedValue{
				"mpCode": javavalue.Scalar("java.lang.Long", json.Number("433905635109773312")),
				"ratio":  javavalue.Scalar("java.lang.Double", json.Number("2.0")),
				"emoji":  javavalue.Scalar("java.lang.String", "a🙂b"),
			}),
			mode: "decode-query-request",
			want: "QueryRequest{mpCode=java.lang.Long:433905635109773312,ratio=java.lang.Double:2.0,emoji=java.lang.String:a🙂b}",
		},
		{
			name: "dto with enum field",
			value: javavalue.Object("HessianContractHelper$EnumRequest", map[string]javavalue.TypedValue{
				"status": javavalue.Object("HessianContractHelper$Status", map[string]javavalue.TypedValue{
					"name": javavalue.Scalar("java.lang.String", "ACTIVE"),
				}),
			}),
			mode: "decode-enum-request",
			want: "EnumRequest{status=Status:ACTIVE}",
		},
		{
			name: "dto with null enum field",
			value: javavalue.Object("HessianContractHelper$EnumRequest", map[string]javavalue.TypedValue{
				"status": javavalue.Scalar("HessianContractHelper$Status", nil),
			}),
			mode: "decode-enum-request",
			want: "EnumRequest{status=null}",
		},
		{
			name: "nested dto",
			value: javavalue.Object("HessianContractHelper$ComplexResponse", map[string]javavalue.TypedValue{
				"primary": javavalue.Object("HessianContractHelper$QueryResponse", map[string]javavalue.TypedValue{
					"success": javavalue.Scalar("java.lang.Boolean", true),
					"amount":  javavalue.Scalar("java.math.BigDecimal", "1.23"),
					"tags": javavalue.List("java.util.ArrayList", []javavalue.TypedValue{
						javavalue.Scalar("java.lang.String", "P"),
					}),
				}),
				"history": javavalue.List("java.util.ArrayList", []javavalue.TypedValue{
					javavalue.Object("HessianContractHelper$QueryResponse", map[string]javavalue.TypedValue{
						"success": javavalue.Scalar("java.lang.Boolean", false),
						"amount":  javavalue.Scalar("java.math.BigDecimal", "0.00"),
						"tags": javavalue.List("java.util.ArrayList", []javavalue.TypedValue{
							javavalue.Scalar("java.lang.String", "H"),
						}),
					}),
				}),
				"attributes": javavalue.Map("java.util.LinkedHashMap", []javavalue.MapEntry{
					{Key: javavalue.Scalar("java.lang.String", "mpCode"), Value: javavalue.Scalar("java.lang.Long", json.Number("433905635109773312"))},
					{Key: javavalue.Scalar("java.lang.String", "nullable"), Value: javavalue.Scalar("", nil)},
					{Key: javavalue.Scalar("java.lang.String", "ratio"), Value: javavalue.Scalar("java.lang.Double", json.Number("2.0"))},
				}),
				"mixed": javavalue.List("java.util.ArrayList", []javavalue.TypedValue{
					javavalue.Scalar("", nil),
					javavalue.Scalar("java.lang.String", "x"),
					javavalue.Scalar("java.lang.Long", json.Number("9")),
				}),
			}),
			mode: "decode-complex-response",
			want: "ComplexResponse{primary=QueryResponse{success=java.lang.Boolean:true,amount=java.math.BigDecimal:1.23,tags=List[java.lang.String:P]},history=List[QueryResponse{success=java.lang.Boolean:false,amount=java.math.BigDecimal:0.00,tags=List[java.lang.String:H]}],attributes=Map{java.lang.String:mpCode=java.lang.Long:433905635109773312,java.lang.String:nullable=null,java.lang.String:ratio=java.lang.Double:2.0},mixed=List[null,java.lang.String:x,java.lang.Long:9]}",
		},
		{
			name: "local-date write",
			value: javavalue.Object("com.caucho.hessian.io.jdk8.LocalDateHandle", map[string]javavalue.TypedValue{
				"year":  javavalue.Scalar("java.lang.Integer", json.Number("2024")),
				"month": javavalue.Scalar("java.lang.Integer", json.Number("1")),
				"day":   javavalue.Scalar("java.lang.Integer", json.Number("15")),
			}),
			mode: "decode-any",
			want: "java.time.LocalDate:2024-01-15",
		},
		{
			name: "local-date-time write",
			value: javavalue.Object("com.caucho.hessian.io.jdk8.LocalDateTimeHandle", map[string]javavalue.TypedValue{
				"date": javavalue.Object("com.caucho.hessian.io.jdk8.LocalDateHandle", map[string]javavalue.TypedValue{
					"year":  javavalue.Scalar("java.lang.Integer", json.Number("2024")),
					"month": javavalue.Scalar("java.lang.Integer", json.Number("1")),
					"day":   javavalue.Scalar("java.lang.Integer", json.Number("15")),
				}),
				"time": javavalue.Object("com.caucho.hessian.io.jdk8.LocalTimeHandle", map[string]javavalue.TypedValue{
					"hour":   javavalue.Scalar("java.lang.Integer", json.Number("10")),
					"minute": javavalue.Scalar("java.lang.Integer", json.Number("30")),
					"second": javavalue.Scalar("java.lang.Integer", json.Number("0")),
					"nano":   javavalue.Scalar("java.lang.Integer", json.Number("0")),
				}),
			}),
			mode: "decode-any",
			want: "java.time.LocalDateTime:2024-01-15T10:30",
		},
		{
			name: "instant write",
			value: javavalue.Object("com.caucho.hessian.io.jdk8.InstantHandle", map[string]javavalue.TypedValue{
				"seconds": javavalue.Scalar("java.lang.Long", json.Number("1705314600")),
				"nanos":   javavalue.Scalar("java.lang.Integer", json.Number("0")),
			}),
			mode: "decode-any",
			want: "java.time.Instant:2024-01-15T10:30:00Z",
		},
		{
			name: "big-integer write",
			value: javavalue.Object("java.math.BigInteger", map[string]javavalue.TypedValue{
				"signum":             javavalue.Scalar("java.lang.Integer", json.Number("1")),
				"bitCount":           javavalue.Scalar("java.lang.Integer", json.Number("0")),
				"bitLength":          javavalue.Scalar("java.lang.Integer", json.Number("0")),
				"lowestSetBit":       javavalue.Scalar("java.lang.Integer", json.Number("0")),
				"firstNonzeroIntNum": javavalue.Scalar("java.lang.Integer", json.Number("0")),
				"mag": javavalue.List("[int", []javavalue.TypedValue{
					javavalue.Scalar("java.lang.Integer", json.Number("2147483647")),
					javavalue.Scalar("java.lang.Integer", json.Number("-1")),
				}),
			}),
			mode: "decode-any",
			want: "java.math.BigInteger:9223372036854775807",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWriter()
			if err := w.writeValue(tc.value); err != nil {
				t.Fatalf("writeValue: %v", err)
			}
			got := contract.run(t, tc.mode, hex.EncodeToString(w.bytes()))
			if got != tc.want {
				t.Fatalf("java decoded %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHessianJavaContractJavaEncodedValuesReadableByGo(t *testing.T) {
	contract := requireJavaHessianContract(t)

	cases := []struct {
		name  string
		check func(t *testing.T, got interface{})
	}{
		{
			name: "string-emoji",
			check: func(t *testing.T, got interface{}) {
				if got != "a🙂b" {
					t.Fatalf("got %#v", got)
				}
			},
		},
		{
			name: "long",
			check: func(t *testing.T, got interface{}) {
				if got != int64(433905635109773312) {
					t.Fatalf("got %#v", got)
				}
			},
		},
		{
			name: "integer",
			check: func(t *testing.T, got interface{}) {
				if got != int64(5) {
					t.Fatalf("got %#v", got)
				}
			},
		},
		{
			name: "double-whole",
			check: func(t *testing.T, got interface{}) {
				if got != float64(2) {
					t.Fatalf("got %#v", got)
				}
			},
		},
		{
			name: "list-with-null",
			check: func(t *testing.T, got interface{}) {
				items := listItems(t, got)
				if len(items) != 3 || items[0] != nil || items[1] != int64(1) || items[2] != "two" {
					t.Fatalf("items = %#v", items)
				}
			},
		},
		{
			name: "map-long-key",
			check: func(t *testing.T, got interface{}) {
				entries := mapEntries(t, got)
				if entries["7"] != "seven" || entries["name"] != "alice" {
					t.Fatalf("entries = %#v", entries)
				}
			},
		},
		{
			name: "big-decimal",
			check: func(t *testing.T, got interface{}) {
				obj := objectFields(t, got, "java.math.BigDecimal")
				if obj["value"] != "1000.50" {
					t.Fatalf("fields = %#v", obj)
				}
			},
		},
		{
			name: "bytes",
			check: func(t *testing.T, got interface{}) {
				b, ok := got.([]byte)
				if !ok || hex.EncodeToString(b) != "0102ff" {
					t.Fatalf("got %#v", got)
				}
			},
		},
		{
			name: "query-response",
			check: func(t *testing.T, got interface{}) {
				fields := objectFields(t, got, "HessianContractHelper$QueryResponse")
				if fields["success"] != true {
					t.Fatalf("fields = %#v", fields)
				}
				amount := objectFields(t, fields["amount"], "java.math.BigDecimal")
				if amount["value"] != "113795.2485" {
					t.Fatalf("amount = %#v", amount)
				}
				tags := listItems(t, fields["tags"])
				if len(tags) != 2 || tags[0] != "A" || tags[1] != "B" {
					t.Fatalf("tags = %#v", tags)
				}
			},
		},
		{
			name: "enum",
			check: func(t *testing.T, got interface{}) {
				fields := objectFields(t, got, "HessianContractHelper$Status")
				if fields["name"] != "ACTIVE" {
					t.Fatalf("fields = %#v", fields)
				}
			},
		},
		{
			name: "enum-response",
			check: func(t *testing.T, got interface{}) {
				fields := objectFields(t, got, "HessianContractHelper$EnumResponse")
				status := objectFields(t, fields["status"], "HessianContractHelper$Status")
				if status["name"] != "ACTIVE" {
					t.Fatalf("status = %#v", status)
				}
				history := listItems(t, fields["history"])
				if len(history) != 1 {
					t.Fatalf("history = %#v", history)
				}
				item := objectFields(t, history[0], "HessianContractHelper$Status")
				if item["name"] != "INACTIVE" {
					t.Fatalf("history[0] = %#v", item)
				}
			},
		},
		{
			name: "nested-response",
			check: func(t *testing.T, got interface{}) {
				fields := objectFields(t, got, "HessianContractHelper$ComplexResponse")
				primary := objectFields(t, fields["primary"], "HessianContractHelper$QueryResponse")
				if primary["success"] != true {
					t.Fatalf("primary = %#v", primary)
				}
				history := listItems(t, fields["history"])
				if len(history) != 1 {
					t.Fatalf("history = %#v", history)
				}
				attrs := mapEntries(t, fields["attributes"])
				if attrs["mpCode"] != int64(433905635109773312) || attrs["nullable"] != nil || attrs["ratio"] != float64(2) {
					t.Fatalf("attributes = %#v", attrs)
				}
				mixed := listItems(t, fields["mixed"])
				if len(mixed) != 3 || mixed[0] != nil || mixed[1] != "x" || mixed[2] != int64(9) {
					t.Fatalf("mixed = %#v", mixed)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rawHex := contract.run(t, "encode", tc.name)
			data, err := hex.DecodeString(rawHex)
			if err != nil {
				t.Fatalf("decode helper hex: %v", err)
			}
			got, err := (&reader{data: data}).readValue()
			if err != nil {
				t.Fatalf("readValue: %v; data=%s", err, rawHex)
			}
			tc.check(t, got)
		})
	}
}

func TestHessianJavaContractGoldenBytesMatchJavaOracle(t *testing.T) {
	contract := requireJavaHessianContract(t)

	for _, tc := range hessianJavaGoldenCases {
		t.Run(tc.name, func(t *testing.T) {
			got := contract.run(t, "encode", tc.name)
			if got != tc.hex {
				t.Fatalf("golden hex drifted from Java oracle\ncase: %s\njava:   %s\ngolden: %s", tc.name, got, tc.hex)
			}
		})
	}
	t.Run("big-integer", func(t *testing.T) {
		got := contract.run(t, "encode", "big-integer")
		if got != hessianBigIntegerGoldenHex {
			t.Fatalf("BigInteger golden hex drifted from Java oracle\njava:   %s\ngolden: %s", got, hessianBigIntegerGoldenHex)
		}
	})
	t.Run("circular", func(t *testing.T) {
		got := contract.run(t, "encode", "circular")
		if got != hessianCircularGoldenHex {
			t.Fatalf("circular golden hex drifted from Java oracle\njava:   %s\ngolden: %s", got, hessianCircularGoldenHex)
		}
	})
}

func requireJavaHessianContract(t *testing.T) javaHessianContract {
	t.Helper()
	javaContractOnce.Do(func() {
		javac, err := exec.LookPath("javac")
		if err != nil {
			javaContractSkip = "javac not found; skipping Java Hessian contract tests"
			return
		}
		java, err := exec.LookPath("java")
		if err != nil {
			javaContractSkip = "java not found; skipping Java Hessian contract tests"
			return
		}
		jar := findHessianJar()
		if jar == "" {
			javaContractSkip = "hessian jar not found in local Maven repository; skipping Java Hessian contract tests"
			return
		}
		outDir, err := os.MkdirTemp("", "sofarpc-hessian-contract-*")
		if err != nil {
			javaContractErr = err
			return
		}
		javaContractTempDir = outDir
		src := filepath.Join("testdata", "java", "HessianContractHelper.java")
		cmd := exec.Command(javac, "-cp", jar, "-d", outDir, src)
		if output, err := cmd.CombinedOutput(); err != nil {
			javaContractErr = fmt.Errorf("compile Java Hessian helper: %w\n%s", err, output)
			return
		}
		javaContractCache = javaHessianContract{java: java, classpath: strings.Join([]string{jar, outDir}, string(os.PathListSeparator))}
	})
	if javaContractSkip != "" {
		t.Skip(javaContractSkip)
	}
	if javaContractErr != nil {
		t.Fatal(javaContractErr)
	}
	return javaContractCache
}

func findHessianJar() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	candidates := []string{
		filepath.Join(home, ".m2", "repository", "hessian", "hessian", "3.2.16.alipay", "hessian-3.2.16.alipay.jar"),
		filepath.Join(home, ".m2", "repository", "hessian", "hessian", "3.2.4.alipay", "hessian-3.2.4.alipay.jar"),
		filepath.Join(home, ".m2", "repository", "com", "caucho", "hessian", "3.1.5", "hessian-3.1.5.jar"),
	}
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func (c javaHessianContract) run(t *testing.T, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"-cp", c.classpath, "HessianContractHelper"}, args...)
	cmd := exec.Command(c.java, cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("java helper failed: %v\n%s", err, output)
	}
	return strings.TrimSpace(string(output))
}

func listItems(t *testing.T, got interface{}) []interface{} {
	t.Helper()
	if items, ok := got.([]interface{}); ok {
		return items
	}
	obj, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("got %T, want list", got)
	}
	items, ok := obj["items"].([]interface{})
	if !ok {
		t.Fatalf("got %#v, want list items", got)
	}
	return items
}

func mapEntries(t *testing.T, got interface{}) map[string]interface{} {
	t.Helper()
	entries, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("got %T, want map", got)
	}
	if nested, ok := entries["entries"].(map[string]interface{}); ok {
		return nested
	}
	return entries
}

func objectFields(t *testing.T, got interface{}, wantType string) map[string]interface{} {
	t.Helper()
	obj, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("got %T, want object %s", got, wantType)
	}
	if obj["type"] != wantType {
		t.Fatalf("type = %#v, want %s; object=%#v", obj["type"], wantType, obj)
	}
	fields, ok := obj["fields"].(map[string]interface{})
	if !ok {
		t.Fatalf("fields missing in %#v", obj)
	}
	return fields
}

func TestHessianJavaContractJavaDateDecodesAsEpochMillis(t *testing.T) {
	contract := requireJavaHessianContract(t)
	rawHex := contract.run(t, "encode", "date")
	data, err := hex.DecodeString(rawHex)
	if err != nil {
		t.Fatalf("decode helper hex: %v", err)
	}
	got, err := (&reader{data: data}).readValue()
	if err != nil {
		t.Fatalf("readValue: %v; data=%s", err, rawHex)
	}
	if got != int64(0) {
		t.Fatalf("date = %#v, want epoch millis", got)
	}
}

// TestHessianBigIntegerDecodesToSignumMagFields documents the raw decode shape:
// Java serializes BigInteger field-wise, so it decodes to internal signum/mag
// fields. Presentation reconstructs the number from them (TestHessianGoldenBigIntegerFlattensToNumber).
func TestHessianBigIntegerDecodesToSignumMagFields(t *testing.T) {
	contract := requireJavaHessianContract(t)
	rawHex := contract.run(t, "encode", "big-integer")
	data, err := hex.DecodeString(rawHex)
	if err != nil {
		t.Fatalf("decode helper hex: %v", err)
	}
	got, err := (&reader{data: data}).readValue()
	if err != nil {
		t.Fatalf("readValue: %v; data=%s", err, rawHex)
	}
	fields := objectFields(t, got, "java.math.BigInteger")
	if fields["signum"] != int64(1) || fields["mag"] == nil {
		t.Fatalf("fields = %#v", fields)
	}
}

// TestHessianWriterRejectsBareBigIntegerScalar documents that the low-level writer
// has no BigInteger scalar tag: BigInteger is encoded via its serialized signum/mag
// object form (built by the app coercion; see the "big-integer write" oracle case),
// so a bare scalar is correctly rejected.
func TestHessianWriterRejectsBareBigIntegerScalar(t *testing.T) {
	w := newWriter()
	err := w.writeValueWithType("java.math.BigInteger", "9223372036854775807")
	if err == nil {
		t.Fatalf("a bare BigInteger scalar must be rejected (encode via the object form)")
	}
	if !strings.Contains(err.Error(), "object form") {
		t.Fatalf("err = %v", err)
	}
}
