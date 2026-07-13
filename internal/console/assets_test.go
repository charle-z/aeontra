package console

import (
	"strings"
	"testing"
)

func TestEmbeddedConsoleAssetsAreDependencyFreeAndDoNotPersistData(t *testing.T) {
	assets := map[string]string{
		"index": string(mustAssetForTest(t, "assets/index.html")),
		"css":   string(mustAssetForTest(t, "assets/app.css")),
		"js":    string(mustAssetForTest(t, "assets/app.js")),
	}
	for name, content := range assets {
		lower := strings.ToLower(content)
		for _, forbidden := range []string{
			"http://", "https://", "//cdn", "google-analytics", "segment.io",
			"localstorage", "sessionstorage", "indexeddb", "document.cookie",
			"innerhtml", "eval(", "new function", "serviceworker", "websocket",
		} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("%s contains forbidden browser capability %q", name, forbidden)
			}
		}
	}
	index := assets["index"]
	for _, required := range []string{
		"<header", "<main", "<footer", "aria-live", "Security boundary",
		"Delivery pipeline", "/console/assets/app.css", "/console/assets/app.js",
	} {
		if !strings.Contains(index, required) {
			t.Errorf("index missing %q", required)
		}
	}
	css := assets["css"]
	for _, required := range []string{"color-scheme: dark", "prefers-reduced-motion", ":focus-visible", "@media (max-width: 640px)"} {
		if !strings.Contains(css, required) {
			t.Errorf("CSS missing %q", required)
		}
	}
}

func mustAssetForTest(t *testing.T, path string) []byte {
	t.Helper()
	content, err := embeddedAssets.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return content
}
