package direct

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	javaTypeKey       = "@type"
	javaFieldTypesKey = "__fieldTypes"
	maxHessianDepth   = 128
)

type typedObject struct {
	name       string
	fields     map[string]interface{}
	fieldTypes map[string]string
}

type javaList []interface{}
type javaMap map[string]interface{}

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
	case []interface{}:
		items, err := normalizeList(x)
		if err != nil {
			return err
		}
		return w.writeList("java.util.ArrayList", items)
	case javaList:
		return w.writeList("java.util.ArrayList", []interface{}(x))
	case map[string]interface{}:
		values, err := normalizePlainMap(x)
		if err != nil {
			return err
		}
		return w.writeMap("", values)
	case map[string]string:
		m := make(map[string]interface{}, len(x))
		for k, v := range x {
			m[k] = v
		}
		return w.writeMap("", m)
	case javaMap:
		return w.writeMap("java.util.LinkedHashMap", map[string]interface{}(x))
	case typedObject:
		return w.writeTypedObject(x)
	default:
		return fmt.Errorf("unsupported hessian value %T", v)
	}
	return nil
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
	if n <= 0x1f {
		w.buf = append(w.buf, byte(n))
		w.buf = append(w.buf, s...)
		return nil
	}
	if n > math.MaxUint16 {
		return fmt.Errorf("string too long: %d", n)
	}
	w.buf = append(w.buf, 'S')
	w.writeUint16(uint16(n))
	w.buf = append(w.buf, s...)
	return nil
}

func (w *writer) writeLenString(s string) {
	w.writeInt(int64(utf16Length(s)))
	w.buf = append(w.buf, s...)
}

func (w *writer) writeType(s string) {
	w.buf = append(w.buf, 't')
	w.writeUint16(uint16(utf16Length(s)))
	w.buf = append(w.buf, s...)
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
	case "java.math.BigDecimal", "java.math.BigInteger":
		s, ok := decimalString(v)
		if !ok {
			return true, fmt.Errorf("cannot encode %T as %s", v, base)
		}
		return true, w.writeTypedObject(typedObject{name: base, fields: map[string]interface{}{"value": s}})
	default:
		return false, nil
	}
}

func normalizeArgs(types []string, args []interface{}) ([]interface{}, error) {
	out := make([]interface{}, len(args))
	for i, arg := range args {
		argType := ""
		if i < len(types) {
			argType = types[i]
		}
		normalized, err := normalizeArgDepth(argType, arg, 0)
		if err != nil {
			return nil, fmt.Errorf("arg %d: %w", i, err)
		}
		out[i] = normalized
	}
	return out, nil
}

func normalizeArg(argType string, v interface{}) interface{} {
	out, err := normalizeArgDepth(argType, v, 0)
	if err != nil {
		return v
	}
	return out
}

func normalizeArgDepth(argType string, v interface{}, depth int) (interface{}, error) {
	if depth > maxHessianDepth {
		return nil, fmt.Errorf("hessian argument nesting too deep")
	}
	if m, ok := stringMap(v); ok {
		if explicit := explicitType(m); explicit != "" {
			return typedFromMapDepth(explicit, m, depth+1)
		}
		if shouldWrapArg(argType) {
			return typedFromMapDepth(eraseJavaType(argType), m, depth+1)
		}
		values, err := normalizePlainMapDepth(m, depth+1)
		if err != nil {
			return nil, err
		}
		return javaMap(values), nil
	}
	return normalizeValueDepth(argType, v, depth+1)
}

func normalizeValue(v interface{}) interface{} {
	out, err := normalizeValueDepth("", v, 0)
	if err != nil {
		return v
	}
	return out
}

func normalizeValueDepth(javaType string, v interface{}, depth int) (interface{}, error) {
	if depth > maxHessianDepth {
		return nil, fmt.Errorf("hessian argument nesting too deep")
	}
	switch x := v.(type) {
	case json.Number:
		if javaType != "" {
			return x, nil
		}
		return numberValue(x), nil
	case []interface{}:
		values, err := normalizeListDepth(x, depth+1)
		if err != nil {
			return nil, err
		}
		return javaList(values), nil
	case map[string]interface{}:
		if explicit := explicitType(x); explicit != "" {
			return typedFromMapDepth(explicit, x, depth+1)
		}
		if shouldWrapArg(javaType) {
			return typedFromMapDepth(eraseJavaType(javaType), x, depth+1)
		}
		values, err := normalizePlainMapDepth(x, depth+1)
		if err != nil {
			return nil, err
		}
		return javaMap(values), nil
	default:
		return x, nil
	}
}

func normalizeList(values []interface{}) ([]interface{}, error) {
	return normalizeListDepth(values, 0)
}

func normalizeListDepth(values []interface{}, depth int) ([]interface{}, error) {
	if depth > maxHessianDepth {
		return nil, fmt.Errorf("hessian argument nesting too deep")
	}
	out := make([]interface{}, len(values))
	for i, v := range values {
		normalized, err := normalizeValueDepth("", v, depth+1)
		if err != nil {
			return nil, err
		}
		out[i] = normalized
	}
	return out, nil
}

func normalizePlainMap(values map[string]interface{}) (map[string]interface{}, error) {
	return normalizePlainMapDepth(values, 0)
}

func normalizePlainMapDepth(values map[string]interface{}, depth int) (map[string]interface{}, error) {
	if depth > maxHessianDepth {
		return nil, fmt.Errorf("hessian argument nesting too deep")
	}
	out := make(map[string]interface{}, len(values))
	for k, v := range values {
		normalized, err := normalizeValueDepth("", v, depth+1)
		if err != nil {
			return nil, err
		}
		out[k] = normalized
	}
	return out, nil
}

func typedFromMap(class string, values map[string]interface{}) typedObject {
	out, err := typedFromMapDepth(class, values, 0)
	if err != nil {
		return typedObject{name: class, fields: map[string]interface{}{}}
	}
	return out
}

func typedFromMapDepth(class string, values map[string]interface{}, depth int) (typedObject, error) {
	if depth > maxHessianDepth {
		return typedObject{}, fmt.Errorf("hessian argument nesting too deep")
	}
	fieldTypes := fieldTypesFromMap(values[javaFieldTypesKey])
	fields := make(map[string]interface{}, len(values))
	for k, v := range values {
		if k == javaTypeKey || k == "__type" || k == javaFieldTypesKey {
			continue
		}
		normalized, err := normalizeValueDepth(fieldTypes[k], v, depth+1)
		if err != nil {
			return typedObject{}, err
		}
		fields[k] = normalized
	}
	return typedObject{name: class, fields: fields, fieldTypes: fieldTypes}, nil
}

func explicitType(values map[string]interface{}) string {
	for _, k := range []string{javaTypeKey, "__type"} {
		if v, ok := values[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func stringMap(v interface{}) (map[string]interface{}, bool) {
	switch x := v.(type) {
	case map[string]interface{}:
		return x, true
	case map[string]string:
		out := make(map[string]interface{}, len(x))
		for k, v := range x {
			out[k] = v
		}
		return out, true
	default:
		return nil, false
	}
}

func shouldWrapArg(argType string) bool {
	t := eraseJavaType(argType)
	if t == "" || !strings.Contains(t, ".") {
		return false
	}
	if strings.HasPrefix(t, "java.lang.") || strings.HasPrefix(t, "java.math.") || strings.HasPrefix(t, "java.util.") {
		return false
	}
	switch t {
	case "boolean", "byte", "char", "short", "int", "long", "float", "double", "void":
		return false
	default:
		return true
	}
}

func fieldTypesFromMap(v interface{}) map[string]string {
	if v == nil {
		return nil
	}
	out := map[string]string{}
	switch x := v.(type) {
	case map[string]string:
		for k, typ := range x {
			if strings.TrimSpace(typ) != "" {
				out[k] = strings.TrimSpace(typ)
			}
		}
	case map[string]interface{}:
		for k, raw := range x {
			if typ, ok := raw.(string); ok && strings.TrimSpace(typ) != "" {
				out[k] = strings.TrimSpace(typ)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
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

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
