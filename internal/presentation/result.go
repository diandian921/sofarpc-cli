package presentation

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Assertion struct {
	Path   string
	Equals interface{}
	Exists *bool
}

type AssertionOutcome struct {
	Path     string      `json:"path"`
	Passed   bool        `json:"passed"`
	Expected interface{} `json:"expected,omitempty"`
	Actual   interface{} `json:"actual,omitempty"`
	Message  string      `json:"message,omitempty"`
}

func Flatten(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		if fields, ok := x["fields"].(map[string]interface{}); ok {
			if class, _ := x["type"].(string); class != "" {
				if value, ok := flattenJavaValueObject(class, fields); ok {
					return value
				}
			}
			out := make(map[string]interface{}, len(fields))
			for _, key := range fieldOrder(x, fields) {
				out[key] = Flatten(fields[key])
			}
			return out
		}
		if entries, ok := x["entries"].(map[string]interface{}); ok {
			out := make(map[string]interface{}, len(entries))
			for k, v := range entries {
				out[k] = Flatten(v)
			}
			return out
		}
		if items, ok := x["items"].([]interface{}); ok {
			return Flatten(items)
		}
		out := make(map[string]interface{}, len(x))
		for k, v := range x {
			if k == "fieldNames" {
				continue
			}
			out[k] = Flatten(v)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, v := range x {
			out[i] = Flatten(v)
		}
		return out
	default:
		return x
	}
}

func flattenJavaValueObject(class string, fields map[string]interface{}) (interface{}, bool) {
	switch class {
	case "java.math.BigDecimal", "java.math.BigInteger":
		value, ok := fields["value"]
		if !ok {
			return nil, false
		}
		return flattenJavaNumber(value), true
	case "java.util.Date", "java.sql.Date", "java.sql.Time", "java.sql.Timestamp":
		return flattenJavaDate(fields)
	case "com.caucho.hessian.io.jdk8.LocalDateHandle",
		"com.caucho.hessian.io.jdk8.LocalTimeHandle",
		"com.caucho.hessian.io.jdk8.LocalDateTimeHandle",
		"com.caucho.hessian.io.jdk8.InstantHandle":
		return flattenJDKTime(class, fields)
	case "java.util.Optional":
		return flattenJavaOptional(fields)
	case "java.util.OptionalInt", "java.util.OptionalLong":
		return flattenJavaOptionalNumber(fields, false)
	case "java.util.OptionalDouble":
		return flattenJavaOptionalNumber(fields, true)
	default:
		if looksLikeEnumObject(class, fields) {
			return fields["name"], true
		}
		return nil, false
	}
}

func flattenJavaDate(fields map[string]interface{}) (interface{}, bool) {
	for _, key := range []string{"time", "fastTime", "value"} {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		epochMillis, ok := int64FromValue(raw)
		if !ok {
			continue
		}
		return map[string]interface{}{
			"epochMillis": epochMillis,
			"iso":         time.UnixMilli(epochMillis).UTC().Format(time.RFC3339Nano),
		}, true
	}
	return nil, false
}

// flattenJDKTime renders the alipay Hessian jdk8 *Handle proxy objects (how
// java.time values arrive on the wire) into ISO-8601 strings, mirroring what Java
// readResolve reconstructs. The raw {type, fields} decode stays faithful; this is
// the agent-facing presentation.
func flattenJDKTime(class string, fields map[string]interface{}) (interface{}, bool) {
	switch class {
	case "com.caucho.hessian.io.jdk8.LocalDateHandle":
		return localDateString(fields)
	case "com.caucho.hessian.io.jdk8.LocalTimeHandle":
		return localTimeString(fields)
	case "com.caucho.hessian.io.jdk8.LocalDateTimeHandle":
		return localDateTimeString(fields)
	case "com.caucho.hessian.io.jdk8.InstantHandle":
		return instantString(fields)
	}
	return nil, false
}

func localDateString(fields map[string]interface{}) (interface{}, bool) {
	y, ok1 := int64FromValue(fields["year"])
	m, ok2 := int64FromValue(fields["month"])
	d, ok3 := int64FromValue(fields["day"])
	if !ok1 || !ok2 || !ok3 {
		return nil, false
	}
	return fmt.Sprintf("%04d-%02d-%02d", y, m, d), true
}

func localTimeString(fields map[string]interface{}) (interface{}, bool) {
	h, ok1 := int64FromValue(fields["hour"])
	mi, ok2 := int64FromValue(fields["minute"])
	s, ok3 := int64FromValue(fields["second"])
	if !ok1 || !ok2 || !ok3 {
		return nil, false
	}
	out := fmt.Sprintf("%02d:%02d:%02d", h, mi, s)
	if n, ok := int64FromValue(fields["nano"]); ok && n > 0 {
		out += "." + strings.TrimRight(fmt.Sprintf("%09d", n), "0")
	}
	return out, true
}

func localDateTimeString(fields map[string]interface{}) (interface{}, bool) {
	dateFields, ok1 := handleFields(fields["date"])
	timeFields, ok2 := handleFields(fields["time"])
	if !ok1 || !ok2 {
		return nil, false
	}
	date, ok1 := localDateString(dateFields)
	clock, ok2 := localTimeString(timeFields)
	if !ok1 || !ok2 {
		return nil, false
	}
	return fmt.Sprintf("%vT%v", date, clock), true
}

func instantString(fields map[string]interface{}) (interface{}, bool) {
	secs, ok := int64FromValue(fields["seconds"])
	if !ok {
		return nil, false
	}
	nanos, _ := int64FromValue(fields["nanos"])
	t := time.Unix(secs, nanos).UTC()
	if nanos == 0 {
		return t.Format(time.RFC3339), true
	}
	return t.Format(time.RFC3339Nano), true
}

func handleFields(v interface{}) (map[string]interface{}, bool) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil, false
	}
	f, ok := m["fields"].(map[string]interface{})
	return f, ok
}

func flattenJavaOptional(fields map[string]interface{}) (interface{}, bool) {
	if present, ok := boolField(fields, "present"); ok && !present {
		return nil, true
	}
	for _, key := range []string{"value", "val"} {
		if value, ok := fields[key]; ok {
			return Flatten(value), true
		}
	}
	return nil, false
}

func flattenJavaOptionalNumber(fields map[string]interface{}, floating bool) (interface{}, bool) {
	if present, ok := boolField(fields, "present"); ok && !present {
		return nil, true
	}
	for _, key := range []string{"value", "val"} {
		value, ok := fields[key]
		if !ok {
			continue
		}
		if floating {
			if n, ok := float64FromValue(value); ok {
				return n, true
			}
		} else if n, ok := int64FromValue(value); ok {
			return n, true
		}
		return Flatten(value), true
	}
	return nil, false
}

func looksLikeEnumObject(class string, fields map[string]interface{}) bool {
	if !strings.HasSuffix(class, "Enum") || len(fields) != 1 {
		return false
	}
	_, ok := fields["name"].(string)
	return ok
}

func boolField(fields map[string]interface{}, key string) (bool, bool) {
	v, ok := fields[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

func int64FromValue(v interface{}) (int64, bool) {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i, true
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
	case float64:
		return int64(x), x == float64(int64(x))
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return i, err == nil
	}
	return 0, false
}

func float64FromValue(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	}
	return 0, false
}

func flattenJavaNumber(value interface{}) interface{} {
	switch x := value.(type) {
	case json.Number:
		return x
	case string:
		if n, ok := jsonNumberLiteral(x); ok {
			return n
		}
		return x
	default:
		return Flatten(x)
	}
}

func jsonNumberLiteral(s string) (json.Number, bool) {
	s = strings.TrimSpace(s)
	if s == "" || !json.Valid([]byte(s)) {
		return "", false
	}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var out interface{}
	if err := dec.Decode(&out); err != nil {
		return "", false
	}
	n, ok := out.(json.Number)
	if !ok {
		return "", false
	}
	return n, true
}

func fieldOrder(obj map[string]interface{}, fields map[string]interface{}) []string {
	raw, ok := obj["fieldNames"].([]interface{})
	if !ok {
		return sortedKeys(fields)
	}
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, item := range raw {
		s, ok := item.(string)
		if ok {
			if _, exists := fields[s]; exists {
				out = append(out, s)
				seen[s] = true
			}
		}
	}
	for _, k := range sortedKeys(fields) {
		if !seen[k] {
			out = append(out, k)
		}
	}
	return out
}

func EvaluateAssertions(result interface{}, assertions []Assertion) ([]AssertionOutcome, int) {
	if len(assertions) == 0 {
		return nil, 0
	}
	out := make([]AssertionOutcome, 0, len(assertions))
	failures := 0
	for _, assertion := range assertions {
		actual, exists := lookupPath(result, assertion.Path)
		item := AssertionOutcome{Path: assertion.Path}
		if assertion.Exists != nil {
			want := *assertion.Exists
			item.Passed = exists == want
			if !item.Passed {
				item.Expected = want
				item.Actual = exists
				item.Message = fmt.Sprintf("expected exists=%v but got %v", want, exists)
			}
		} else {
			item.Expected = assertion.Equals
			item.Actual = actual
			item.Passed = exists && valuesEqual(actual, assertion.Equals)
			if !item.Passed {
				item.Message = fmt.Sprintf("expected %v but got %v", assertion.Equals, actual)
			}
		}
		if !item.Passed {
			failures++
		}
		out = append(out, item)
	}
	return out, failures
}

func lookupPath(root interface{}, path string) (interface{}, bool) {
	if path == "" || path == "$" {
		return root, true
	}
	if !strings.HasPrefix(path, "$.") {
		return nil, false
	}
	current := root
	for _, part := range strings.Split(strings.TrimPrefix(path, "$."), ".") {
		if part == "" {
			return nil, false
		}
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		current, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func valuesEqual(left, right interface{}) bool {
	left = cloneJSONValue(left)
	right = cloneJSONValue(right)
	return reflect.DeepEqual(left, right) || fmt.Sprint(left) == fmt.Sprint(right)
}

func cloneJSONValue(v interface{}) interface{} {
	body, err := json.Marshal(v)
	if err != nil {
		return v
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.UseNumber()
	var out interface{}
	if err := dec.Decode(&out); err != nil {
		return v
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
