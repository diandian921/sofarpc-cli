package javavalue

import "sort"

type Kind string

const (
	KindScalar Kind = "scalar"
	KindObject Kind = "object"
	KindList   Kind = "list"
	KindMap    Kind = "map"
)

type TypedValue struct {
	JavaType string
	Kind     Kind
	Scalar   interface{}
	Fields   map[string]TypedValue
	Items    []TypedValue
	Entries  []MapEntry
}

type MapEntry struct {
	Key   TypedValue
	Value TypedValue
}

func Scalar(javaType string, value interface{}) TypedValue {
	return TypedValue{JavaType: javaType, Kind: KindScalar, Scalar: value}
}

func Object(javaType string, fields map[string]TypedValue) TypedValue {
	if fields == nil {
		fields = map[string]TypedValue{}
	}
	return TypedValue{JavaType: javaType, Kind: KindObject, Fields: fields}
}

func List(javaType string, items []TypedValue) TypedValue {
	if items == nil {
		items = []TypedValue{}
	}
	return TypedValue{JavaType: javaType, Kind: KindList, Items: items}
}

func Map(javaType string, entries []MapEntry) TypedValue {
	if entries == nil {
		entries = []MapEntry{}
	}
	return TypedValue{JavaType: javaType, Kind: KindMap, Entries: entries}
}

func (v TypedValue) Display() interface{} {
	out := map[string]interface{}{
		"javaType": v.JavaType,
		"kind":     string(v.Kind),
	}
	switch v.Kind {
	case KindObject:
		fields := map[string]interface{}{}
		for _, key := range sortedKeys(v.Fields) {
			fields[key] = v.Fields[key].Display()
		}
		out["fields"] = fields
	case KindList:
		items := make([]interface{}, len(v.Items))
		for i, item := range v.Items {
			items[i] = item.Display()
		}
		out["items"] = items
	case KindMap:
		entries := make([]map[string]interface{}, len(v.Entries))
		for i, entry := range v.Entries {
			entries[i] = map[string]interface{}{"key": entry.Key.Display(), "value": entry.Value.Display()}
		}
		out["entries"] = entries
	default:
		out["value"] = v.Scalar
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
