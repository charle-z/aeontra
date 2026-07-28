package docs_test

import "testing"

func TestPublicShowcaseDocumentationContract(t *testing.T) {
	implementation := readDoc(t, "landing/public-showcase.md")
	for _, required := range []string{
		"Status: **implemented in source",
		"landing-public-showcase-design",
		"internal/landing",
		"exact public route `GET /`",
		"presentation-only",
		"`/version`",
		"docs/showcase/pixelgrama-evidence.json",
		"/showcase/pixelgrama-evidence.json",
		"`read-only`, `ask`, and `allow` selector",
		"does not depend only on color",
		"reduced authority is not absolute safety",
		"arrow/Home/End keyboard navigation",
		"fails closed",
		"prefers-reduced-motion",
		"Hosted on CubePath",
		"Production closure requires",
	} {
		if !containsNormalizedProse(implementation, required) {
			t.Errorf("public showcase implementation contract missing %q", required)
		}
	}

	readme := readDoc(t, "../README.md")
	for _, required := range []string{
		"public presentation landing",
		"`/console` remains authenticated",
		"docs/landing/public-showcase.md",
	} {
		if !containsNormalizedProse(readme, required) {
			t.Errorf("README public showcase status missing %q", required)
		}
	}

	roadmap := readDoc(t, "product-roadmap.md")
	for _, required := range []string{
		"| Public showcase | Implemented in source |",
		"exact public `GET /`",
		"presentation-only",
		"live commit matches the merge",
	} {
		if !containsNormalizedProse(roadmap, required) {
			t.Errorf("product roadmap public showcase status missing %q", required)
		}
	}

	documentationMap := readDoc(t, "documentation-map.md")
	for _, required := range []string{"docs/landing/public-showcase.md", "docs/showcase/pixelgrama-evidence.json"} {
		if !containsNormalizedProse(documentationMap, required) {
			t.Errorf("documentation map does not name %q", required)
		}
	}
}
