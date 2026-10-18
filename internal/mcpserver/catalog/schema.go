package catalog

func object(props map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func strProp(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func boolProp(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func closedObject(props map[string]any, required ...string) map[string]any {
	schema := object(props, required...)
	schema["additionalProperties"] = false
	return schema
}

func boundedStringProp(description string, minimum, maximum int) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": description,
		"minLength":   minimum,
		"maxLength":   maximum,
	}
}

func patternedStringProp(description, pattern string, minimum, maximum int) map[string]any {
	property := boundedStringProp(description, minimum, maximum)
	property["pattern"] = pattern
	return property
}

func slugProp(description string) map[string]any {
	return patternedStringProp(description, `^[a-z0-9]+(?:-[a-z0-9]+)*$`, 1, 64)
}

func enumStringProp(description string, values ...string) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": description,
		"enum":        values,
	}
}

func integerProp(description string, minimum, maximum int) map[string]any {
	return map[string]any{
		"type":        "integer",
		"description": description,
		"minimum":     minimum,
		"maximum":     maximum,
	}
}
