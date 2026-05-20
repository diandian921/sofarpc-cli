package mcp

func objectSchema(properties map[string]interface{}, required ...string) map[string]interface{} {
	if properties == nil {
		properties = map[string]interface{}{}
	}
	schema := map[string]interface{}{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func freeObjectSchema(description string) map[string]interface{} {
	return map[string]interface{}{"type": "object", "description": description, "additionalProperties": true}
}

func stringMapSchema(description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "object",
		"description": description,
		"additionalProperties": map[string]interface{}{
			"type": "string",
		},
	}
}

func stringSchema(description string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": description}
}

func enumStringSchema(description string, values ...string) map[string]interface{} {
	enum := make([]interface{}, 0, len(values))
	for _, value := range values {
		enum = append(enum, value)
	}
	return map[string]interface{}{"type": "string", "description": description, "enum": enum}
}

func numberSchema(description string) map[string]interface{} {
	return map[string]interface{}{"type": "integer", "description": description}
}

func boolSchema(description string) map[string]interface{} {
	return map[string]interface{}{"type": "boolean", "description": description}
}

func arraySchema(description string) map[string]interface{} {
	return map[string]interface{}{"type": "array", "description": description}
}
