package direct

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
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

func flattenValue(v interface{}) interface{} {
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
				out[key] = flattenValue(fields[key])
			}
			return out
		}
		if entries, ok := x["entries"].(map[string]interface{}); ok {
			out := make(map[string]interface{}, len(entries))
			for k, v := range entries {
				out[k] = flattenValue(v)
			}
			return out
		}
		if items, ok := x["items"].([]interface{}); ok {
			return flattenValue(items)
		}
		out := make(map[string]interface{}, len(x))
		for k, v := range x {
			if k == "fieldNames" {
				continue
			}
			out[k] = flattenValue(v)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, v := range x {
			out[i] = flattenValue(v)
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
	default:
		return nil, false
	}
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
		return flattenValue(x)
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
