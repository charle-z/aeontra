package docs_test

import "testing"

func TestPublicSiteDocumentationContract(t *testing.T) {
	implementation := readDoc(t, "landing/public-site.md")
	for _, required := range []string{
		"Status: **deployed and verified**",
		"internal/landing",
		"exact public route `GET /`",
		"open-source product site",
		"`/version`",
		"exactly one same-origin public request",
		"renders only availability and the bounded tool count",
		"`read-only`, `ask`, and `allow`",
		"does not depend only on color",
		"Aeontra limits authority",
		"arrow/Home/End keyboard navigation",
		"rejects inline script/style attributes",
		"prefers-reduced-motion",
		"no analytics",
		"Production closure requires",
	} {
		if !containsNormalizedProse(implementation, required) {
			t.Errorf("public site implementation contract missing %q", required)
		}
	}

	readme := readDoc(t, "../README.md")
	for _, required := range []string{
		"public product site",
		"`/console` remains authenticated",
		"docs/landing/public-site.md",
	} {
		if !containsNormalizedProse(readme, required) {
			t.Errorf("README public site status missing %q", required)
		}
	}

	roadmap := readDoc(t, "product-roadmap.md")
	for _, required := range []string{
		"| Public product site | Deployed and verified |",
		"https://aeontra.com/",
		"responsive browser acceptance",
		"exact site-build identity",
	} {
		if !containsNormalizedProse(roadmap, required) {
			t.Errorf("product roadmap public site status missing %q", required)
		}
	}

	documentationMap := readDoc(t, "documentation-map.md")
	for _, required := range []string{"docs/landing/public-site.md", "docs/showcase/pixelgrama-evidence.json"} {
		if !containsNormalizedProse(documentationMap, required) {
			t.Errorf("documentation map does not name %q", required)
		}
	}
}
