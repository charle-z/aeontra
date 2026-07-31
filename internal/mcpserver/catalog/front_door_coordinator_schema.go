package catalog

func enumProp(description string, values ...string) map[string]any {
	return enumStringProp(description, values...)
}
