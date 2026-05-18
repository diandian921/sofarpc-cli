package direct

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/sofarpc/cli/internal/javavalue"
)

const maxHessianDepth = 128

type typedObject struct {
	name       string
	fields     map[string]interface{}
	fieldTypes map[string]string
}

type writer struct {
	buf     []byte
	classes map[string]int
	depth   int
}

func newWriter() *writer {
	return &writer{buf: make([]byte, 0, 512), classes: map[string]int{}}
}

func (w *writer) bytes() []byte {
	return append([]byte(nil), w.buf...)
}

func (w *writer) writeValue(v interface{}) error {
	return w.writeValueWithType("", v)
}

func (w *writer) writeValueWithType(javaType string, v interface{}) error {
	w.depth++
	defer func() { w.depth-- }()
	if w.depth > maxHessianDepth {
		return fmt.Errorf("hessian nesting too deep")
	}
	if v == nil {
		w.buf = append(w.buf, 'N')
		return nil
	}
	switch x := v.(type) {
	case javavalue.TypedValue:
		return w.writeTypedValue(x)
	case *javavalue.TypedValue:
		if x == nil {
			w.buf = append(w.buf, 'N')
			return nil
		}
		return w.writeTypedValue(*x)
	}
	if handled, err := w.writeJavaScalar(javaType, v); handled || err != nil {
		return err
	}
	switch x := v.(type) {
	case bool:
		if x {
			w.buf = append(w.buf, 'T')
		} else {
			w.buf = append(w.buf, 'F')
		}
	case string:
		return w.writeString(x)
	case json.Number:
		return w.writeValue(numberValue(x))
	case int:
		w.writeInt(int64(x))
	case int8:
		w.writeInt(int64(x))
	case int16:
		w.writeInt(int64(x))
	case int32:
		w.writeInt(int64(x))
	case int64:
		w.writeLong(x)
	case uint:
		w.writeLong(int64(x))
	case uint8:
		w.writeLong(int64(x))
	case uint16:
		w.writeLong(int64(x))
	case uint32:
		w.writeLong(int64(x))
	case uint64:
		if x > math.MaxInt64 {
			return fmt.Errorf("uint64 out of range: %d", x)
		}
		w.writeLong(int64(x))
	case float32:
		w.writeDouble(float64(x))
	case float64:
		if math.Trunc(x) == x && x >= math.MinInt64 && x <= math.MaxInt64 {
			w.writeLong(int64(x))
		} else {
			w.writeDouble(x)
		}
	case []string:
		items := make([]interface{}, len(x))
		for i := range x {
			items[i] = x[i]
		}
		return w.writeList("[string]", items)
	case map[string]interface{}:
		return w.writeMap("", x)
	case map[string]string:
		m := make(map[string]interface{}, len(x))
		for k, v := range x {
			m[k] = v
		}
		return w.writeMap("", m)
	case typedObject:
		return w.writeTypedObject(x)
	default:
		return fmt.Errorf("unsupported hessian value %T", v)
	}
	return nil
}

func (w *writer) writeTypedValue(value javavalue.TypedValue) error {
	switch value.Kind {
	case javavalue.KindObject:
		class := eraseJavaType(value.JavaType)
		if class == "" {
			return fmt.Errorf("object javaType is required")
		}
		keys := sortedKeys(value.Fields)
		values := make([]interface{}, len(keys))
		fieldTypes := make(map[string]string, len(keys))
		for i, key := range keys {
			child := value.Fields[key]
			values[i] = child
			if child.JavaType != "" {
				fieldTypes[key] = child.JavaType
			}
		}
		return w.writeObjectWithTypes(class, keys, values, fieldTypes)
	case javavalue.KindList:
		class := eraseJavaType(value.JavaType)
		if class == "" {
			class = "java.util.ArrayList"
		}
		items := make([]interface{}, len(value.Items))
		for i, item := range value.Items {
			items[i] = item
		}
		return w.writeList(class, items)
	case javavalue.KindMap:
		class := eraseJavaType(value.JavaType)
		if class == "java.util.Map" || class == "java.util.HashMap" || class == "java.util.LinkedHashMap" {
			class = "java.util.LinkedHashMap"
		}
		return w.writeTypedMap(class, value.Entries)
	default:
		if handled, err := w.writeJavaScalar(value.JavaType, value.Scalar); handled || err != nil {
			return err
		}
		return w.writeValueWithType("", value.Scalar)
	}
}

func (w *writer) writeTypedObject(obj typedObject) error {
	keys := sortedKeys(obj.fields)
	values := make([]interface{}, len(keys))
	for i, k := range keys {
		values[i] = obj.fields[k]
	}
	return w.writeObjectWithTypes(obj.name, keys, values, obj.fieldTypes)
}

func (w *writer) writeObject(class string, fields []string, values []interface{}) error {
	return w.writeObjectWithTypes(class, fields, values, nil)
}

func (w *writer) writeObjectWithTypes(class string, fields []string, values []interface{}, fieldTypes map[string]string) error {
	if len(fields) != len(values) {
		return fmt.Errorf("field/value mismatch for %s", class)
	}
	key := class + "\x00" + strings.Join(fields, "\x00")
	ref, ok := w.classes[key]
	if !ok {
		ref = len(w.classes)
		w.classes[key] = ref
		w.buf = append(w.buf, 'O')
		w.writeLenString(class)
		w.writeInt(int64(len(fields)))
		for _, f := range fields {
			if err := w.writeString(f); err != nil {
				return err
			}
		}
	}
	w.buf = append(w.buf, 'o')
	w.writeInt(int64(ref))
	for i, v := range values {
		if err := w.writeValueWithType(fieldTypes[fields[i]], v); err != nil {
			return err
		}
	}
	return nil
}

func (w *writer) writeMap(class string, values map[string]interface{}) error {
	w.buf = append(w.buf, 'M')
	if class != "" {
		w.writeType(class)
	}
	for _, k := range sortedKeys(values) {
		if err := w.writeString(k); err != nil {
			return err
		}
		if err := w.writeValue(values[k]); err != nil {
			return err
		}
	}
	w.buf = append(w.buf, 'z')
	return nil
}

func (w *writer) writeTypedMap(class string, entries []javavalue.MapEntry) error {
	w.buf = append(w.buf, 'M')
	if class != "" {
		w.writeType(class)
	}
	for _, entry := range entries {
		if err := w.writeTypedValue(entry.Key); err != nil {
			return err
		}
		if err := w.writeTypedValue(entry.Value); err != nil {
			return err
		}
	}
	w.buf = append(w.buf, 'z')
	return nil
}

func (w *writer) writeList(class string, values []interface{}) error {
	w.buf = append(w.buf, 'V')
	if class != "" {
		w.writeType(class)
	}
	w.writeLength(len(values))
	for _, v := range values {
		if err := w.writeValue(v); err != nil {
			return err
		}
	}
	w.buf = append(w.buf, 'z')
	return nil
}

func (w *writer) writeString(s string) error {
	n := utf16Length(s)
	encoded := hessianStringBytes(s)
	if n <= 0x1f {
		w.buf = append(w.buf, byte(n))
		w.buf = append(w.buf, encoded...)
		return nil
	}
	if n > math.MaxUint16 {
		return fmt.Errorf("string too long: %d", n)
	}
	w.buf = append(w.buf, 'S')
	w.writeUint16(uint16(n))
	w.buf = append(w.buf, encoded...)
	return nil
}

func (w *writer) writeLenString(s string) {
	encoded := hessianStringBytes(s)
	w.writeInt(int64(utf16Length(s)))
	w.buf = append(w.buf, encoded...)
}

func (w *writer) writeType(s string) {
	encoded := hessianStringBytes(s)
	w.buf = append(w.buf, 't')
	w.writeUint16(uint16(utf16Length(s)))
	w.buf = append(w.buf, encoded...)
}

func (w *writer) writeLength(n int) {
	if n >= 0 && n <= 0xff {
		w.buf = append(w.buf, 0x6e, byte(n))
		return
	}
	w.buf = append(w.buf, 'l')
	w.writeUint32(uint32(n))
}

func (w *writer) writeInt(n int64) {
	w.buf = append(w.buf, 'I')
	w.writeUint32(uint32(int32(n)))
}

func (w *writer) writeLong(n int64) {
	w.buf = append(w.buf, 'L')
	w.writeUint64(uint64(n))
}

func (w *writer) writeDouble(n float64) {
	w.buf = append(w.buf, 'D')
	w.writeUint64(math.Float64bits(n))
}

func (w *writer) writeUint16(n uint16) {
	w.buf = append(w.buf, byte(n>>8), byte(n))
}

func (w *writer) writeUint32(n uint32) {
	w.buf = append(w.buf, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
}

func (w *writer) writeUint64(n uint64) {
	w.buf = append(w.buf,
		byte(n>>56), byte(n>>48), byte(n>>40), byte(n>>32),
		byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
}

func (w *writer) writeJavaScalar(javaType string, v interface{}) (bool, error) {
	if isByteArrayType(javaType) {
		b, ok := byteSliceValue(v)
		if !ok {
			return true, fmt.Errorf("cannot encode %T as %s", v, javaType)
		}
		return true, w.writeBytes(b)
	}
	base := eraseJavaType(javaType)
	if base == "" {
		return false, nil
	}
	switch base {
	case "boolean", "java.lang.Boolean":
		b, ok := boolValue(v)
		if !ok {
			return true, fmt.Errorf("cannot encode %T as %s", v, base)
		}
		if b {
			w.buf = append(w.buf, 'T')
		} else {
			w.buf = append(w.buf, 'F')
		}
		return true, nil
	case "byte", "java.lang.Byte":
		n, ok := int64Value(v)
		if !ok || n < math.MinInt8 || n > math.MaxInt8 {
			return true, fmt.Errorf("cannot encode %v as %s", v, base)
		}
		w.writeInt(n)
		return true, nil
	case "short", "java.lang.Short":
		n, ok := int64Value(v)
		if !ok || n < math.MinInt16 || n > math.MaxInt16 {
			return true, fmt.Errorf("cannot encode %v as %s", v, base)
		}
		w.writeInt(n)
		return true, nil
	case "int", "java.lang.Integer":
		n, ok := int64Value(v)
		if !ok || n < math.MinInt32 || n > math.MaxInt32 {
			return true, fmt.Errorf("cannot encode %v as %s", v, base)
		}
		w.writeInt(n)
		return true, nil
	case "long", "java.lang.Long":
		n, ok := int64Value(v)
		if !ok {
			return true, fmt.Errorf("cannot encode %v as %s", v, base)
		}
		w.writeLong(n)
		return true, nil
	case "float", "java.lang.Float", "double", "java.lang.Double":
		n, ok := float64Value(v)
		if !ok {
			return true, fmt.Errorf("cannot encode %v as %s", v, base)
		}
		w.writeDouble(n)
		return true, nil
	case "char", "java.lang.Character", "java.lang.String":
		s, ok := stringValue(v)
		if !ok {
			return true, fmt.Errorf("cannot encode %T as %s", v, base)
		}
		return true, w.writeString(s)
	case "java.math.BigDecimal":
		s, ok := decimalString(v)
		if !ok {
			return true, fmt.Errorf("cannot encode %T as %s", v, base)
		}
		return true, w.writeTypedObject(typedObject{name: base, fields: map[string]interface{}{"value": s}})
	case "java.math.BigInteger":
		return true, fmt.Errorf("java.math.BigInteger encoding is not supported")
	default:
		return false, nil
	}
}

func (w *writer) writeBytes(b []byte) error {
	if len(b) <= 0x0f {
		w.buf = append(w.buf, byte(0x20+len(b)))
		w.buf = append(w.buf, b...)
		return nil
	}
	if len(b) > math.MaxUint16 {
		return fmt.Errorf("bytes too long: %d", len(b))
	}
	w.buf = append(w.buf, 'B')
	w.writeUint16(uint16(len(b)))
	w.buf = append(w.buf, b...)
	return nil
}

func eraseJavaType(t string) string {
	t = strings.TrimSpace(t)
	if i := strings.IndexByte(t, '<'); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	for strings.HasSuffix(t, "[]") {
		t = strings.TrimSuffix(t, "[]")
	}
	return t
}

func isByteArrayType(t string) bool {
	t = strings.TrimSpace(t)
	t = strings.TrimPrefix(t, "final ")
	return t == "byte[]" || t == "java.lang.Byte[]"
}

func numberValue(n json.Number) interface{} {
	s := n.String()
	if strings.ContainsAny(s, ".eE") {
		if f, err := n.Float64(); err == nil {
			return f
		}
		return s
	}
	if i, err := n.Int64(); err == nil {
		return i
	}
	if f, err := n.Float64(); err == nil {
		return f
	}
	return s
}

func utf16Length(s string) int {
	n := 0
	for _, r := range s {
		if r > utf8.RuneSelf && r > 0xffff {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func hessianStringBytes(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if r <= 0xffff {
			out = appendHessianUTF8Unit(out, uint16(r))
			continue
		}
		hi, lo := utf16.EncodeRune(r)
		out = appendHessianUTF8Unit(out, uint16(hi))
		out = appendHessianUTF8Unit(out, uint16(lo))
	}
	return out
}

func appendHessianUTF8Unit(out []byte, unit uint16) []byte {
	switch {
	case unit < 0x80:
		return append(out, byte(unit))
	case unit < 0x800:
		return append(out, 0xc0|byte(unit>>6), 0x80|byte(unit&0x3f))
	default:
		return append(out,
			0xe0|byte(unit>>12),
			0x80|byte((unit>>6)&0x3f),
			0x80|byte(unit&0x3f))
	}
}

func int64Value(v interface{}) (int64, bool) {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i, true
		}
		if f, err := x.Float64(); err == nil && math.Trunc(f) == f && f >= math.MinInt64 && f <= math.MaxInt64 {
			return int64(f), true
		}
	case int:
		return int64(x), true
	case int8:
		return int64(x), true
	case int16:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case uint:
		if uint64(x) <= math.MaxInt64 {
			return int64(x), true
		}
	case uint8:
		return int64(x), true
	case uint16:
		return int64(x), true
	case uint32:
		return int64(x), true
	case uint64:
		if x <= math.MaxInt64 {
			return int64(x), true
		}
	case float32:
		f := float64(x)
		if math.Trunc(f) == f && f >= math.MinInt64 && f <= math.MaxInt64 {
			return int64(f), true
		}
	case float64:
		if math.Trunc(x) == x && x >= math.MinInt64 && x <= math.MaxInt64 {
			return int64(x), true
		}
	case string:
		if i, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64); err == nil {
			return i, true
		}
	}
	return 0, false
}

func float64Value(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case int:
		return float64(x), true
	case int8:
		return float64(x), true
	case int16:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint8:
		return float64(x), true
	case uint16:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func boolValue(v interface{}) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(x))
		return b, err == nil
	default:
		return false, false
	}
}

func stringValue(v interface{}) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case json.Number:
		return x.String(), true
	case fmt.Stringer:
		return x.String(), true
	case bool:
		return strconv.FormatBool(x), true
	default:
		return "", false
	}
}

func decimalString(v interface{}) (string, bool) {
	switch x := v.(type) {
	case json.Number:
		return x.String(), true
	case string:
		return strings.TrimSpace(x), strings.TrimSpace(x) != ""
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprint(x), true
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32), true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	default:
		return "", false
	}
}

func byteSliceValue(v interface{}) ([]byte, bool) {
	switch x := v.(type) {
	case []byte:
		return append([]byte(nil), x...), true
	case []interface{}:
		out := make([]byte, len(x))
		for i, item := range x {
			n, ok := int64Value(item)
			if !ok || n < -128 || n > 255 {
				return nil, false
			}
			out[i] = byte(n)
		}
		return out, true
	case []int:
		out := make([]byte, len(x))
		for i, item := range x {
			if item < -128 || item > 255 {
				return nil, false
			}
			out[i] = byte(item)
		}
		return out, true
	default:
		return nil, false
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
