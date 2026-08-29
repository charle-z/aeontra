package landing

import (
	"bytes"
	"encoding/xml"
	"image/png"
	"regexp"
	"strings"
	"testing"
)

func TestEmbeddedLandingAssetsDefineTheAeontraPublicSite(t *testing.T) {
	index := string(mustLandingAsset(t, "assets/index.html"))
	css := string(mustLandingAsset(t, "assets/app.css"))
	js := string(mustLandingAsset(t, "assets/app.js"))
	socialCard := string(mustLandingAsset(t, "assets/social-card.svg"))
	socialCardPNG := mustLandingAsset(t, "assets/social-card.png")
	favicon := string(mustLandingAsset(t, "assets/favicon.svg"))
	robots := string(mustLandingAsset(t, "assets/robots.txt"))
	sitemap := string(mustLandingAsset(t, "assets/sitemap.xml"))

	indexLower := strings.ToLower(index)
	for _, forbidden := range []string{
		"<style", " style=", " onclick=", " onload=", "javascript:",
		"/repos/", "/state/", "mcp_devbox_", "authorization: bearer", "workspace_id", "device_id",
		"pixelgrama", "cubepath", "presentation-only", "ai-powered", "revolutionary", "seamless",
	} {
		if strings.Contains(indexLower, forbidden) {
			t.Errorf("landing index contains stale, private, or generic content %q", forbidden)
		}
	}
	for _, required := range []string{
		`<header`, `<nav`, `<main`, `<section`, `<footer`,
		`href="#content"`, `aria-label="Primary"`,
		`/landing/assets/app.css`, `/landing/assets/app.js`, `/landing/assets/social-card.png`,
		`rel="canonical" href="https://aeontra.com/"`, `rel="icon" href="/favicon.svg"`,
		`property="og:title"`, `property="og:description"`, `property="og:type"`,
		`property="og:url" content="https://aeontra.com/"`,
		`property="og:image" content="https://aeontra.com/landing/assets/social-card.png"`,
		`name="twitter:title"`, `name="twitter:description"`, `name="twitter:image"`,
		`href="https://github.com/charle-z/aeontra"`,
		`href="https://github.com/charle-z/aeontra/blob/main/docs/public-alpha.md"`,
		`id="system"`, `id="authority"`, `id="edge"`, `id="start"`,
		`data-runtime-status`, `data-runtime-version`, `data-runtime-tools`, `data-runtime-commit`,
		`data-mode="read-only"`, `data-mode="ask"`, `data-mode="allow"`,
		`data-copy-command`, `data-language-toggle`,
		`id="proof"`, `data-en="A concrete repository path"`,
		`issues/new?template=alpha_feedback.yml`,
		`The Go module and binaries retain their historical names for compatibility.`,
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
		"--paper:", "--ink:", "--signal:", "border-radius: 0", "prefers-reduced-motion", ":focus-visible",
		"@media (max-width: 760px)", "@media (max-width: 420px)", "overflow-wrap: anywhere",
		".proof-section", "justify-content: flex-start", "font-size: clamp(3.15rem, 5.6vw, 5.5rem)",
	} {
		if !strings.Contains(cssLower, required) {
			t.Errorf("landing CSS missing %q", required)
		}
	}
	for _, forbidden := range []string{"linear-gradient", "radial-gradient", "backdrop-filter", "url(http", "url(//"} {
		if strings.Contains(cssLower, forbidden) {
			t.Errorf("landing CSS contains generic or remote styling %q", forbidden)
		}
	}

	jsLower := strings.ToLower(js)
	for _, required := range []string{
		`fetch("/version"`, "textcontent", "aria-selected", "navigator.language", "abortcontroller",
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

	var socialSVG struct{ XMLName xml.Name }
	if err := xml.Unmarshal([]byte(socialCard), &socialSVG); err != nil || socialSVG.XMLName.Local != "svg" {
		t.Fatalf("social card is not a well-formed SVG: name=%q err=%v", socialSVG.XMLName.Local, err)
	}
	for _, required := range []string{"AEONTRA", "BOUNDARIES FOR SOFTWARE AGENTS", "OPEN SOURCE"} {
		if !strings.Contains(socialCard, required) {
			t.Errorf("social card missing %q", required)
		}
	}
	for _, forbidden := range []string{"Pixelgrama", "CubePath", "href=\"http", "xlink:href", "<script", "foreignObject"} {
		if strings.Contains(socialCard, forbidden) {
			t.Errorf("social card contains stale or executable content %q", forbidden)
		}
	}
	if _, err := png.Decode(bytes.NewReader(socialCardPNG)); err != nil {
		t.Fatalf("social card PNG is invalid: %v", err)
	}
	if !strings.Contains(favicon, "<svg") || !strings.Contains(favicon, "A/") {
		t.Fatal("favicon does not contain the Aeontra mark")
	}
	for _, required := range []string{"User-agent: *", "Allow: /", "Sitemap: https://aeontra.com/sitemap.xml"} {
		if !strings.Contains(robots, required) {
			t.Errorf("robots.txt missing %q", required)
		}
	}
	for _, required := range []string{"<urlset", "<loc>https://aeontra.com/</loc>"} {
		if !strings.Contains(sitemap, required) {
			t.Errorf("sitemap.xml missing %q", required)
		}
	}
}

func TestLandingNarrativeIsConcreteBilingualAndOperatorOwned(t *testing.T) {
	index := string(mustLandingAsset(t, "assets/index.html"))

	for _, required := range []string{
		`data-en="Give software agents a defined place to work."`,
		`data-es="Dale a los agentes de software un lugar definido para trabajar."`,
		`data-en="Aeontra is an open-source MCP control plane for repositories, CI, deployments and private Edge workers."`,
		`data-es="Aeontra es un plano de control MCP de código abierto para repositorios, CI, despliegues y workers Edge privados."`,
		`data-en="The client requests work. Aeontra enforces the configured boundary and records the result."`,
		`data-es="El cliente solicita trabajo. Aeontra aplica el límite configurado y registra el resultado."`,
		`data-en="Read only"`, `data-en="Ask"`, `data-en="Allow"`,
		`data-en="Policy is loaded by the operator, not written by the model."`,
		`data-en="Linux / Parrot / WSL"`, `data-en="Native Windows"`,
		`go install github.com/charle-z/mcp-devbox/cmd/mcp-devbox@latest`,
		`mcp-devbox serve --root /absolute/path/to/disposable-repo --mode read-only`,
		`Apache-2.0`,
	} {
		if !strings.Contains(index, required) {
			t.Errorf("landing narrative missing %q", required)
		}
	}

	for _, slogan := range []string{
		"secure by default", "not just", "not a shell", "the future of", "supercharge", "unleash",
	} {
		if strings.Contains(strings.ToLower(index), slogan) {
			t.Errorf("landing uses stock contrast or marketing language %q", slogan)
		}
	}
}

func TestLandingRuntimeAndModeInteractionsRemainLocalAndBounded(t *testing.T) {
	index := string(mustLandingAsset(t, "assets/index.html"))
	js := string(mustLandingAsset(t, "assets/app.js"))

	if got := strings.Count(index, `class="primary-action`); got != 2 {
		t.Fatalf("landing has %d primary actions, want exactly 2", got)
	}
	if got := strings.Count(index, `role="tab"`); got != 3 {
		t.Fatalf("landing has %d authority tabs, want 3", got)
	}
	if got := strings.Count(index, `role="tabpanel"`); got != 3 {
		t.Fatalf("landing has %d authority panels, want 3", got)
	}
	for _, required := range []string{"ArrowLeft", "ArrowRight", "Home", "End", "clipboard.writeText"} {
		if !strings.Contains(js, required) {
			t.Errorf("landing interactions missing %q", required)
		}
	}
}

func mustLandingAsset(t *testing.T, path string) []byte {
	t.Helper()
	content, err := embeddedAssets.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(content) == 0 {
		t.Fatalf("asset %s is empty", path)
	}
	return content
}

func assertLandingScriptsAreExternal(t *testing.T, index string) {
	t.Helper()
	for _, match := range regexp.MustCompile(`(?is)<script\b([^>]*)>`).FindAllStringSubmatch(index, -1) {
		if !strings.Contains(match[1], "src=") {
			t.Fatalf("landing contains inline script tag %q", match[0])
		}
	}
}
