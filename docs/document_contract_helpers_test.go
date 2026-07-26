package docs_test

import "strings"

func normalizeDocumentProse(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func containsNormalizedProse(document, phrase string) bool {
	return strings.Contains(normalizeDocumentProse(document), normalizeDocumentProse(phrase))
}
