package direct

import (
	"encoding/binary"
	"fmt"
	"math"
	"unicode/utf8"
)

const (
	intZero       = 0x90
	intByteZero   = 0xc8
	intShortZero  = 0xd4
	longZero      = 0xe0
	longByteZero  = 0xf8
	longShortZero = 0x3c
	longInt       = 0x77
	typeRef       = 0x75
	refByte       = 0x4a
	refShort      = 0x4b
)

type decodedResponse struct {
	IsError     bool
	ErrorMsg    string
	AppResponse interface{}
	Props       map[string]string
}

type classDef struct {
	Name   string
	Fields []string
}

type reader struct {
	data    []byte
	offset  int
	refs    []interface{}
	classes []classDef
	types   []string
	depth   int
}

func decodeSofaResponse(data []byte) (decodedResponse, error) {
	r := &reader{data: data}
	v, err := r.readValue()
	if err != nil {
		return decodedResponse{}, err
	}
	obj, ok := v.(map[string]interface{})
	if !ok {
		return decodedResponse{}, fmt.Errorf("unexpected response root %T", v)
	}
	class, _ := obj["type"].(string)
	fields, _ := obj["fields"].(map[string]interface{})
	if class != responseClass {
		return decodedResponse{IsError: true, ErrorMsg: class, AppResponse: v}, nil
	}
	out := decodedResponse{AppResponse: fields["appResponse"], Props: stringProps(fields["responseProps"])}
	if b, ok := fields["isError"].(bool); ok {
		out.IsError = b
	}
	if s, ok := fields["errorMsg"].(string); ok {
		out.ErrorMsg = s
	}
	return out, nil
}

func (r *reader) readValue() (interface{}, error) {
	r.depth++
	defer func() { r.depth-- }()
	if r.depth > 128 {
		return nil, fmt.Errorf("hessian nesting too deep")
	}
	tag, err := r.byte()
	if err != nil {
		return nil, err
	}
	switch {
	case tag == 'N':
		return nil, nil
	case tag == 'T':
		return true, nil
	case tag == 'F':
		return false, nil
	case tag >= 0x80 && tag <= 0xbf:
		return int64(int(tag) - intZero), nil
	case tag >= 0xc0 && tag <= 0xcf:
		b, err := r.byte()
		if err != nil {
			return nil, err
		}
		return int64((int(tag)-intByteZero)<<8 | int(b)), nil
	case tag >= 0xd0 && tag <= 0xd7:
		b1, err := r.byte()
		if err != nil {
			return nil, err
		}
		b2, err := r.byte()
		if err != nil {
			return nil, err
		}
		return int64((int(tag)-intShortZero)<<16 | int(b1)<<8 | int(b2)), nil
	case tag == 'I':
		n, err := r.int32()
		return int64(n), err
	case tag >= 0xd8 && tag <= 0xef:
		return int64(int(tag) - longZero), nil
	case tag >= 0xf0 && tag <= 0xff:
		b, err := r.byte()
		if err != nil {
			return nil, err
		}
		return int64((int(tag)-longByteZero)<<8 | int(b)), nil
	case tag >= 0x38 && tag <= 0x3f:
		b1, err := r.byte()
		if err != nil {
			return nil, err
		}
		b2, err := r.byte()
		if err != nil {
			return nil, err
		}
		return int64((int(tag)-longShortZero)<<16 | int(b1)<<8 | int(b2)), nil
	case tag == longInt:
		n, err := r.int32()
		return int64(n), err
	case tag == 'L' || tag == 'd':
		return r.int64()
	case tag == 0x67:
		return float64(0), nil
	case tag == 0x68:
		return float64(1), nil
	case tag == 0x69:
		b, err := r.byte()
		return float64(int8(b)), err
	case tag == 0x6a:
		n, err := r.uint16()
		return float64(int16(n)), err
	case tag == 0x6b:
		n, err := r.int32()
		return float64(math.Float32frombits(uint32(n))), err
	case tag == 'D':
		n, err := r.int64()
		return math.Float64frombits(uint64(n)), err
	case tag == 'S' || tag == 's' || tag <= 0x1f:
		return r.stringWithTag(tag)
	case tag == 'B' || tag == 'b' || (tag >= 0x20 && tag <= 0x2f):
		return r.bytesWithTag(tag)
	case tag == 'V':
		return r.list()
	case tag == 'v':
		return r.fixedList()
	case tag == 'M':
		return r.mapValue()
	case tag == 'O':
		if err := r.classDef(); err != nil {
			return nil, err
		}
		return r.readValue()
	case tag == 'o':
		return r.object()
	case tag == 'R':
		n, err := r.int32()
		if err != nil {
			return nil, err
		}
		return r.ref(int(n))
	case tag == refByte:
		n, err := r.byte()
		if err != nil {
			return nil, err
		}
		return r.ref(int(n))
	case tag == refShort:
		n, err := r.uint16()
		if err != nil {
			return nil, err
		}
		return r.ref(int(n))
	default:
		return nil, fmt.Errorf("unsupported hessian tag 0x%02x", tag)
	}
}

func (r *reader) list() (interface{}, error) {
	typ, err := r.typeName()
	if err != nil {
		return nil, err
	}
	n, err := r.length()
	if err != nil {
		return nil, err
	}
	var items []interface{}
	if n >= 0 {
		items = make([]interface{}, 0, n)
		for i := 0; i < n; i++ {
			v, err := r.readValue()
			if err != nil {
				return nil, err
			}
			items = append(items, v)
		}
		if b, ok := r.peek(); ok && b == 'z' {
			r.offset++
		}
	} else {
		for {
			b, ok := r.peek()
			if !ok {
				return nil, fmt.Errorf("unterminated list")
			}
			if b == 'z' {
				r.offset++
				break
			}
			v, err := r.readValue()
			if err != nil {
				return nil, err
			}
			items = append(items, v)
		}
	}
	if typ == "" {
		r.addRef(items)
		return items, nil
	}
	obj := map[string]interface{}{"type": typ, "items": items}
	r.addRef(obj)
	return obj, nil
}

func (r *reader) fixedList() (interface{}, error) {
	ref, err := r.intValue()
	if err != nil {
		return nil, err
	}
	n, err := r.intValue()
	if err != nil {
		return nil, err
	}
	if ref < 0 || ref >= len(r.types) {
		return nil, fmt.Errorf("bad type ref %d", ref)
	}
	items := make([]interface{}, 0, n)
	for i := 0; i < n; i++ {
		v, err := r.readValue()
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	obj := map[string]interface{}{"type": r.types[ref], "items": items}
	r.addRef(obj)
	return obj, nil
}

func (r *reader) mapValue() (interface{}, error) {
	typ, err := r.typeName()
	if err != nil {
		return nil, err
	}
	entries := map[string]interface{}{}
	var out interface{} = entries
	if typ != "" {
		out = map[string]interface{}{"type": typ, "entries": entries}
	}
	r.addRef(out)
	for {
		b, ok := r.peek()
		if !ok {
			return nil, fmt.Errorf("unterminated map")
		}
		if b == 'z' {
			r.offset++
			break
		}
		k, err := r.readValue()
		if err != nil {
			return nil, err
		}
		v, err := r.readValue()
		if err != nil {
			return nil, err
		}
		entries[fmt.Sprint(k)] = v
	}
	return out, nil
}

func (r *reader) classDef() error {
	name, err := r.lenString()
	if err != nil {
		return err
	}
	n, err := r.intValue()
	if err != nil {
		return err
	}
	fields := make([]string, n)
	for i := 0; i < n; i++ {
		s, err := r.stringValue()
		if err != nil {
			return err
		}
		fields[i] = s
	}
	r.classes = append(r.classes, classDef{Name: name, Fields: fields})
	return nil
}

func (r *reader) object() (interface{}, error) {
	ref, err := r.intValue()
	if err != nil {
		return nil, err
	}
	if ref < 0 || ref >= len(r.classes) {
		return nil, fmt.Errorf("bad class ref %d", ref)
	}
	def := r.classes[ref]
	fields := map[string]interface{}{}
	obj := map[string]interface{}{
		"type":       def.Name,
		"fields":     fields,
		"fieldNames": append([]string(nil), def.Fields...),
	}
	r.addRef(obj)
	for _, name := range def.Fields {
		v, err := r.readValue()
		if err != nil {
			return nil, err
		}
		fields[name] = v
	}
	return obj, nil
}

func (r *reader) typeName() (string, error) {
	b, ok := r.peek()
	if !ok {
		return "", nil
	}
	switch b {
	case 't':
		r.offset++
		n, err := r.uint16()
		if err != nil {
			return "", err
		}
		s, err := r.utf8(int(n))
		if err != nil {
			return "", err
		}
		r.types = append(r.types, s)
		return s, nil
	case 'T', typeRef:
		r.offset++
		ref, err := r.intValue()
		if err != nil {
			return "", err
		}
		if ref < 0 || ref >= len(r.types) {
			return "", fmt.Errorf("bad type ref %d", ref)
		}
		return r.types[ref], nil
	default:
		return "", nil
	}
}

func (r *reader) length() (int, error) {
	b, ok := r.peek()
	if !ok {
		return -1, nil
	}
	switch b {
	case 0x6e:
		r.offset++
		n, err := r.byte()
		return int(n), err
	case 'l':
		r.offset++
		return r.int32()
	default:
		return -1, nil
	}
}

func (r *reader) intValue() (int, error) {
	v, err := r.readValue()
	if err != nil {
		return 0, err
	}
	switch n := v.(type) {
	case int64:
		return int(n), nil
	case int:
		return n, nil
	default:
		return 0, fmt.Errorf("expected int, got %T", v)
	}
}

func (r *reader) stringValue() (string, error) {
	v, err := r.readValue()
	if err != nil {
		return "", err
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("expected string, got %T", v)
	}
	return s, nil
}

func (r *reader) stringWithTag(tag byte) (string, error) {
	if tag <= 0x1f {
		return r.utf8(int(tag))
	}
	var done bool
	var out []byte
	for {
		n, err := r.uint16()
		if err != nil {
			return "", err
		}
		chunk, err := r.rawUTF8(int(n))
		if err != nil {
			return "", err
		}
		out = append(out, chunk...)
		done = tag == 'S'
		if done {
			break
		}
		next, err := r.byte()
		if err != nil {
			return "", err
		}
		if next != 's' && next != 'S' {
			return "", fmt.Errorf("bad string continuation")
		}
		tag = next
	}
	return string(out), nil
}

func (r *reader) bytesWithTag(tag byte) ([]byte, error) {
	if tag >= 0x20 && tag <= 0x2f {
		return r.raw(int(tag - 0x20))
	}
	var out []byte
	for {
		n, err := r.uint16()
		if err != nil {
			return nil, err
		}
		chunk, err := r.raw(int(n))
		if err != nil {
			return nil, err
		}
		out = append(out, chunk...)
		if tag == 'B' {
			return out, nil
		}
		next, err := r.byte()
		if err != nil {
			return nil, err
		}
		if next != 'b' && next != 'B' {
			return nil, fmt.Errorf("bad bytes continuation")
		}
		tag = next
	}
}

func (r *reader) lenString() (string, error) {
	n, err := r.intValue()
	if err != nil {
		return "", err
	}
	return r.utf8(n)
}

func (r *reader) ref(n int) (interface{}, error) {
	if n < 0 || n >= len(r.refs) {
		return nil, fmt.Errorf("bad ref %d", n)
	}
	return r.refs[n], nil
}

func (r *reader) addRef(v interface{}) {
	r.refs = append(r.refs, v)
}

func (r *reader) peek() (byte, bool) {
	if r.offset >= len(r.data) {
		return 0, false
	}
	return r.data[r.offset], true
}

func (r *reader) byte() (byte, error) {
	if r.offset >= len(r.data) {
		return 0, fmt.Errorf("unexpected EOF")
	}
	b := r.data[r.offset]
	r.offset++
	return b, nil
}

func (r *reader) uint16() (uint16, error) {
	if len(r.data[r.offset:]) < 2 {
		return 0, fmt.Errorf("unexpected EOF")
	}
	n := binary.BigEndian.Uint16(r.data[r.offset : r.offset+2])
	r.offset += 2
	return n, nil
}

func (r *reader) int32() (int, error) {
	if len(r.data[r.offset:]) < 4 {
		return 0, fmt.Errorf("unexpected EOF")
	}
	n := int(int32(binary.BigEndian.Uint32(r.data[r.offset : r.offset+4])))
	r.offset += 4
	return n, nil
}

func (r *reader) int64() (int64, error) {
	if len(r.data[r.offset:]) < 8 {
		return 0, fmt.Errorf("unexpected EOF")
	}
	n := int64(binary.BigEndian.Uint64(r.data[r.offset : r.offset+8]))
	r.offset += 8
	return n, nil
}

func (r *reader) utf8(chars int) (string, error) {
	raw, err := r.rawUTF8(chars)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (r *reader) rawUTF8(chars int) ([]byte, error) {
	start := r.offset
	count := 0
	for r.offset < len(r.data) && count < chars {
		_, size := utf8.DecodeRune(r.data[r.offset:])
		if size == 0 {
			return nil, fmt.Errorf("bad utf8")
		}
		r.offset += size
		count++
	}
	if count != chars {
		return nil, fmt.Errorf("unexpected EOF")
	}
	return r.data[start:r.offset], nil
}

func (r *reader) raw(n int) ([]byte, error) {
	if n < 0 || len(r.data[r.offset:]) < n {
		return nil, fmt.Errorf("unexpected EOF")
	}
	out := append([]byte(nil), r.data[r.offset:r.offset+n]...)
	r.offset += n
	return out, nil
}

func stringProps(v interface{}) map[string]string {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]interface{}); ok {
		if entries, ok := m["entries"].(map[string]interface{}); ok {
			m = entries
		}
		out := make(map[string]string, len(m))
		for k, v := range m {
			out[k] = fmt.Sprint(v)
		}
		return out
	}
	return map[string]string{"value": fmt.Sprint(v)}
}
