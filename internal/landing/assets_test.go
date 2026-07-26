package landing

import (
	"regexp"
	"strings"
	"testing"
)

func TestEmbeddedLandingAssetsAreSelfContainedAccessibleAndHonest(t *testing.T) {
	index := string(mustLandingAsset(t, "assets/index.html"))
	css := string(mustLandingAsset(t, "assets/app.css"))
	js := string(mustLandingAsset(t, "assets/app.js"))
	requestPath := string(mustLandingAsset(t, "assets/request-path.svg"))
	socialCard := string(mustLandingAsset(t, "assets/social-card.svg"))

	indexLower := strings.ToLower(index)
	for _, forbidden := range []string{
		"<style", " style=", " onclick=", " onload=", "javascript:",
		"/repos/", "/state/", "mcp_devbox_", "authorization: bearer", "workspace_id", "device_id",
	} {
		if strings.Contains(indexLower, forbidden) {
			t.Errorf("landing index contains forbidden detail or inline capability %q", forbidden)
		}
	}
	for _, required := range []string{
		`<header`, `<nav`, `<main`, `<section`, `<footer`,
		`href="#content"`, `aria-label="Primary"`,
		`/landing/assets/app.css`, `/landing/assets/app.js`,
		`/landing/assets/request-path.svg`, `/landing/assets/social-card.svg`,
		`property="og:title"`, `property="og:description"`, `property="og:type"`, `property="og:image"`,
		`data-status="implemented"`, `data-status="experimental"`, `data-status="planned"`,
		`Hosted on CubePath`, `presentation-only`, `local simulation`,
		`65%`, `six calibration runs`, `zero OOM`,
		`/healthz`, `/version`, `/console`,
	} {
		if !strings.Contains(index, required) {
			t.Errorf("landing index missing %q", required)
		}
	}
	assertLandingScriptsAreExternal(t, index)
	if regexp.MustCompile(`[0-9a-f]{40}`).MatchString(indexLower) {
		t.Fatal("landing hardcodes a commit instead of loading /version")
	}
	if strings.Contains(indexLower, "sha256:") {
		t.Fatal("landing hardcodes a catalog hash instead of loading /version")
	}

	cssLower := strings.ToLower(css)
	for _, required := range []string{
		"#0000a8", "border-radius:0", "prefers-reduced-motion", ":focus-visible",
		"@media (max-width:760px)", "@media (max-width:420px)", "overflow-x:auto",
	} {
		if !strings.Contains(cssLower, required) {
			t.Errorf("landing CSS missing %q", required)
		}
	}
	for _, forbidden := range []string{"linear-gradient", "radial-gradient", "backdrop-filter", "url(http", "url(//"} {
		if strings.Contains(cssLower, forbidden) {
			t.Errorf("landing CSS contains forbidden styling or remote asset %q", forbidden)
		}
	}

	jsLower := strings.ToLower(js)
	for _, required := range []string{
		`fetch("/version"`, "prefers-reduced-motion", "textcontent", "aria-pressed", "escape",
		"public identity temporarily unavailable", "intersectionobserver",
	} {
		if !strings.Contains(jsLower, required) {
			t.Errorf("landing JavaScript missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"/mcp", "/console/status", "innerhtml", "outerhtml", "localstorage", "sessionstorage",
		"indexeddb", "document.cookie", "eval(", "new function", "websocket", "eventsource",
	} {
		if strings.Contains(jsLower, forbidden) {
			t.Errorf("landing JavaScript contains forbidden browser or control-plane capability %q", forbidden)
		}
	}
	if strings.Count(js, `fetch("/version"`) != 1 {
		t.Fatalf("landing JavaScript must make exactly one public runtime request, got %d", strings.Count(js, `fetch("/version"`))
	}

	for name, asset := range map[string]string{"request-path.svg": requestPath, "social-card.svg": socialCard} {
		lower := strings.ToLower(asset)
		for _, required := range []string{"<svg", "<title", "<desc"} {
			if !strings.Contains(lower, required) {
				t.Errorf("%s missing %q", name, required)
			}
		}
		for _, forbidden := range []string{"href=\"http", "src=\"http", "xlink:href", "<script", "foreignobject"} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("%s contains forbidden external or executable content %q", name, forbidden)
			}
		}
	}
}

func assertLandingScriptsAreExternal(t *testing.T, index string) {
	t.Helper()
	remaining := index
	for {
		start := strings.Index(strings.ToLower(remaining), "<script")
		if start < 0 {
			return
		}
		remaining = remaining[start:]
		tagEnd := strings.Index(remaining, ">")
		closeAt := strings.Index(strings.ToLower(remaining), "</script>")
		if tagEnd < 0 || closeAt < 0 || closeAt < tagEnd {
			t.Fatal("malformed script element")
		}
		tag := strings.ToLower(remaining[:tagEnd+1])
		body := strings.TrimSpace(remaining[tagEnd+1 : closeAt])
		if !strings.Contains(tag, " src=") || body != "" {
			t.Fatal("landing index contains an inline script")
		}
		remaining = remaining[closeAt+len("</script>"):]
	}
}

func mustLandingAsset(t *testing.T, path string) []byte {
	t.Helper()
	content, err := embeddedAssets.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return content
}
