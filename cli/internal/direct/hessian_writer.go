package direct

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

const javaTypeKey = "@type"

type typedObject struct {
	name   string
	fields map[string]interface{}
}

type javaList []interface{}
type javaMap map[string]interface{}

type writer struct {
	buf     []byte
	classes map[string]int
}

func newWriter() *writer {
	return &writer{buf: make([]byte, 0, 512), classes: map[string]int{}}
}

func (w *writer) bytes() []byte {
	return append([]byte(nil), w.buf...)
}

func (w *writer) writeValue(v interface{}) error {
	switch x := v.(type) {
	case nil:
		w.buf = append(w.buf, 'N')
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
		return w.writeList("java.util.ArrayList", normalizeList(x))
	case javaList:
		return w.writeList("java.util.ArrayList", []interface{}(x))
	case map[string]interface{}:
		return w.writeMap("", normalizePlainMap(x))
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
	return w.writeObject(obj.name, keys, values)
}

func (w *writer) writeObject(class string, fields []string, values []interface{}) error {
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
	for _, v := range values {
		if err := w.writeValue(v); err != nil {
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
	n := utf8.RuneCountInString(s)
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
	w.writeInt(int64(utf8.RuneCountInString(s)))
	w.buf = append(w.buf, s...)
}

func (w *writer) writeType(s string) {
	w.buf = append(w.buf, 't')
	w.writeUint16(uint16(utf8.RuneCountInString(s)))
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

func normalizeArgs(types []string, args []interface{}) []interface{} {
	out := make([]interface{}, len(args))
	for i, arg := range args {
		argType := ""
		if i < len(types) {
			argType = types[i]
		}
		out[i] = normalizeArg(argType, arg)
	}
	return out
}

func normalizeArg(argType string, v interface{}) interface{} {
	if m, ok := stringMap(v); ok {
		if explicit := explicitType(m); explicit != "" {
			return typedFromMap(explicit, m)
		}
		if shouldWrapArg(argType) {
			return typedFromMap(eraseJavaType(argType), m)
		}
		return javaMap(normalizePlainMap(m))
	}
	return normalizeValue(v)
}

func normalizeValue(v interface{}) interface{} {
	switch x := v.(type) {
	case json.Number:
		return numberValue(x)
	case []interface{}:
		return javaList(normalizeList(x))
	case map[string]interface{}:
		if explicit := explicitType(x); explicit != "" {
			return typedFromMap(explicit, x)
		}
		return javaMap(normalizePlainMap(x))
	default:
		return x
	}
}

func normalizeList(values []interface{}) []interface{} {
	out := make([]interface{}, len(values))
	for i, v := range values {
		out[i] = normalizeValue(v)
	}
	return out
}

func normalizePlainMap(values map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(values))
	for k, v := range values {
		out[k] = normalizeValue(v)
	}
	return out
}

func typedFromMap(class string, values map[string]interface{}) typedObject {
	fields := make(map[string]interface{}, len(values))
	for k, v := range values {
		if k == javaTypeKey || k == "__type" {
			continue
		}
		fields[k] = normalizeValue(v)
	}
	return typedObject{name: class, fields: fields}
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

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
