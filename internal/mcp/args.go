package mcp

import (
	"encoding/json"
	"fmt"
	"strconv"
)

func stringArg(args map[string]interface{}, key string, required bool) (string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		if required {
			return "", fmt.Errorf("%s is required", key)
		}
		return "", nil
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("%s must be a non-empty string", key)
	}
	return s, nil
}

func stringArgDefault(args map[string]interface{}, key, def string) string {
	s, err := stringArg(args, key, false)
	if err != nil || s == "" {
		return def
	}
	return s
}

func stringSliceArg(args map[string]interface{}, key string) ([]string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return nil, nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("%s must be an array of strings", key)
	}
	out := make([]string, 0, len(arr))
	for i, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be a string", key, i)
		}
		out = append(out, s)
	}
	return out, nil
}

func stringMapArg(args map[string]interface{}, key string) (map[string]string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return map[string]string{}, nil
	}
	raw, ok := v.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%s must be an object", key)
	}
	out := map[string]string{}
	for k, val := range raw {
		s, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("%s.%s must be a string", key, k)
		}
		out[k] = s
	}
	return out, nil
}

func boolArg(args map[string]interface{}, key string) bool {
	v, ok := args[key]
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

// strictBoolArg reads a boolean argument without silent coercion: a non-boolean
// value (e.g. the string "true") is rejected rather than read as false. This
// guards dryRun, where a silent false would turn an intended dry run into a real
// remote invocation.
func strictBoolArg(args map[string]interface{}, key string) (bool, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be a boolean", key)
	}
	return b, nil
}

func intArgDefault(args map[string]interface{}, key string, def int) int {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case float64:
		if n <= 0 {
			return def
		}
		return int(n)
	case json.Number:
		i, err := strconv.Atoi(n.String())
		if err != nil || i <= 0 {
			return def
		}
		return i
	default:
		return def
	}
}
