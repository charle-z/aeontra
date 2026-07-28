package landing

import (
	"encoding/xml"
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

func TestLandingHeroExplainsProblemSolutionAutonomyAndProofFirst(t *testing.T) {
	index := string(mustLandingAsset(t, "assets/index.html"))
	css := string(mustLandingAsset(t, "assets/app.css"))
	socialCard := string(mustLandingAsset(t, "assets/social-card.svg"))

	for _, required := range []string{
		`data-es="Una shell general entrega al modelo más autoridad de la que la mayoría de tareas necesita."`,
		`data-es="ChatGPT trabajando sobre infraestructura real, sin entregarle una shell libre."`,
		`data-es="MCP Devbox permite que un agente lea, modifique, pruebe, publique y despliegue proyectos mediante herramientas limitadas, políticas inmutables, secretos denegados y operaciones verificables."`,
		`data-es="El propietario elige entre solo lectura, revisión explícita o autonomía dentro de límites previamente configurados."`,
		`data-es="Pixelgrama fue construido, probado, publicado y desplegado mediante MCP Devbox sobre CubePath."`,
		`data-en="A general shell gives the model more authority than most tasks require."`,
		`data-en="ChatGPT working on real infrastructure without receiving a free shell."`,
		`data-en="MCP Devbox lets an agent read, change, test, publish and deploy projects through narrow tools, immutable policy, denied secrets and verifiable operations."`,
		`data-en="The owner chooses between read-only access, explicit review or autonomy within preconfigured limits."`,
		`data-en="Pixelgrama was built, tested, published and deployed through MCP Devbox on CubePath."`,
		`href="/showcase/pixelgrama-evidence.json"`,
		`href="#authority"`,
		`href="https://github.com/charle-z/mcp-devbox"`,
		`data-es="Ver la prueba completa"`,
		`data-es="Explorar el modelo de autoridad"`,
		`data-es="Abrir el repositorio"`,
	} {
		if !strings.Contains(index, required) {
			t.Errorf("landing hero missing %q", required)
		}
	}
	var socialSVG struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal([]byte(socialCard), &socialSVG); err != nil || socialSVG.XMLName.Local != "svg" {
		t.Fatalf("social card is not a well-formed SVG: name=%q err=%v", socialSVG.XMLName.Local, err)
	}

	for _, required := range []string{
		"MCP DEVBOX — BOUNDED AUTONOMY",
		"REAL INFRASTRUCTURE.",
		"NO FREE SHELL.",
		"Pixelgrama built and deployed through MCP Devbox.",
	} {
		if !strings.Contains(socialCard, required) {
			t.Errorf("social card missing benefit-first statement %q", required)
		}
	}

	if got := strings.Count(index, `class="hero-action"`); got != 3 {
		t.Fatalf("landing hero has %d primary actions, want exactly 3", got)
	}

	heroAt := strings.Index(index, `class="hero-grid"`)
	statusAt := strings.Index(index, `class="capability-status"`)
	authorityAt := strings.Index(index, `<section id="authority"`)
	if heroAt < 0 || statusAt <= heroAt || authorityAt <= statusAt {
		t.Fatalf("landing hierarchy is not hero then product status then authority: hero=%d status=%d authority=%d", heroAt, statusAt, authorityAt)
	}

	bootStart := strings.Index(index, `class="boot-window"`)
	bootEnd := strings.Index(index, `class="machine"`)
	if bootStart < 0 || bootEnd <= bootStart {
		t.Fatal("landing boot summary boundaries are invalid")
	}
	boot := index[bootStart:bootEnd]
	for _, required := range []string{
		"CHATGPT WORKING ON REAL INFRASTRUCTURE WITHOUT RECEIVING A FREE SHELL.",
		"narrow tools and verifiable operations",
		"read-only / review / bounded autonomy",
		"Pixelgrama, built and deployed on CubePath",
	} {
		if !strings.Contains(boot, required) {
			t.Errorf("boot summary missing benefit-first statement %q", required)
		}
	}
	for _, forbidden := range []string{"policy explorer", "runtime identity", "tool count"} {
		if strings.Contains(strings.ToLower(boot), forbidden) {
			t.Errorf("boot summary leads with technical component %q", forbidden)
		}
	}

	compactCSS := strings.NewReplacer(" ", "", "\n", "", "\t", "", "\r", "").Replace(strings.ToLower(css))
	for _, required := range []string{
		".hero-grid{display:grid;grid-template-columns:minmax(0,1.45fr)minmax(18rem,.55fr)",
		".hero-actions{display:flex;flex-wrap:wrap",
		".hero-action:first-child{background:var(--cyan);color:var(--black)",
		".hero-proof{align-self:center;min-width:0;border:1pxsolidvar(--green)",
		".hero-boundary{margin:0;padding-top:.65rem;border-top:1pxsolidvar(--gray-low)",
		".hero-grid,.status-grid,.policy-grid,.cards{grid-template-columns:1fr",
	} {
		if !strings.Contains(compactCSS, required) {
			t.Errorf("landing hero CSS missing %q", required)
		}
	}
}

func TestLandingPresentationRetouchesRemainWideFirmwareAndBilingual(t *testing.T) {
	index := string(mustLandingAsset(t, "assets/index.html"))
	css := string(mustLandingAsset(t, "assets/app.css"))
	js := string(mustLandingAsset(t, "assets/app.js"))
	requestPath := string(mustLandingAsset(t, "assets/request-path.svg"))

	compactCSS := strings.NewReplacer(" ", "", "\n", "", "\t", "", "\r", "").Replace(strings.ToLower(css))
	for _, required := range []string{
		".wrap{width:min(1200px,100%)",
		".prose{max-width:72ch",
		"scrollbar-width:thin",
		"scrollbar-color:var(--gray-low)var(--blue-dark)",
		"*::-webkit-scrollbar{",
		"*::-webkit-scrollbar-track{background:var(--blue-dark)",
		"*::-webkit-scrollbar-thumb{background:var(--gray-low)",
		"*::-webkit-scrollbar-thumb:hover{background:var(--cyan)",
		".diagramimg{display:block;width:100%;height:auto;min-width:0",
	} {
		if !strings.Contains(compactCSS, required) {
			t.Errorf("landing CSS missing presentation contract %q", required)
		}
	}
	if strings.Contains(compactCSS, ".diagramimg{display:block;width:100%;min-width:42rem") {
		t.Fatal("desktop request diagram still forces horizontal scrolling")
	}
	if strings.Contains(compactCSS, "max-width:78ch") {
		t.Fatal("non-prose landing content retains the old narrow reading width")
	}

	requestLower := strings.ToLower(requestPath)
	for _, required := range []string{
		`viewbox="0 0 1400 820"`,
		"read / allowlisted command",
		"execute + audit",
		"consequential",
		"single-use plan",
		"owner approval",
	} {
		if !strings.Contains(requestLower, required) {
			t.Errorf("request-path.svg missing accurate branch marker %q", required)
		}
	}
	var svg struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal([]byte(requestPath), &svg); err != nil || svg.XMLName.Local != "svg" {
		t.Fatalf("request-path.svg is not a well-formed SVG: name=%q err=%v", svg.XMLName.Local, err)
	}

	for _, required := range []string{
		`id="languageToggle"`,
		`data-language="es"`,
		`data-language="en"`,
		`data-es="`,
		`data-en="`,
	} {
		if !strings.Contains(index, required) {
			t.Errorf("landing index missing bilingual contract %q", required)
		}
	}

	translatableTag := regexp.MustCompile(`(?is)<[^>]+(?:data-es|data-en)="[^"]*"[^>]*>`)
	dataES := regexp.MustCompile(`(?is)data-es="([^"]*)"`)
	dataEN := regexp.MustCompile(`(?is)data-en="([^"]*)"`)
	tags := translatableTag.FindAllString(index, -1)
	if len(tags) < 60 {
		t.Fatalf("landing has only %d bilingual nodes; expected broad page coverage", len(tags))
	}
	for _, tag := range tags {
		es := dataES.FindStringSubmatch(tag)
		en := dataEN.FindStringSubmatch(tag)
		if len(es) != 2 || strings.TrimSpace(es[1]) == "" {
			t.Errorf("translatable node has empty or missing data-es: %s", tag)
		}
		if len(en) != 2 || strings.TrimSpace(en[1]) == "" {
			t.Errorf("translatable node has empty or missing data-en: %s", tag)
		}
	}

	jsLower := strings.ToLower(js)
	for _, required := range []string{
		"navigator.language",
		"indexof(\"es\") === 0",
		"document.documentelement.lang",
		"getattribute(\"data-es\")",
		"getattribute(\"data-en\")",
		"explanation",
		"rule",
	} {
		if !strings.Contains(jsLower, required) {
			t.Errorf("landing JavaScript missing bilingual behavior %q", required)
		}
	}
	for _, verdict := range []string{"DENIED", "ALLOWED", "PLAN REQUIRED"} {
		if !strings.Contains(js, verdict) {
			t.Errorf("policy identifier verdict %q was translated or removed", verdict)
		}
	}
}

func TestLandingDensePanelsStayAlignedAndCompact(t *testing.T) {
	css := string(mustLandingAsset(t, "assets/app.css"))
	compactCSS := strings.NewReplacer(" ", "", "\n", "", "\t", "", "\r", "").Replace(strings.ToLower(css))

	for _, required := range []string{
		".policy-requests{display:grid;grid-template-columns:repeat(2,minmax(0,1fr))",
		".verdict{min-height:0;display:grid;grid-template-rows:autominmax(4rem,1fr)auto",
		".verdictpre{min-height:4rem",
		".evidence-logp{display:grid;grid-template-columns:10ch9chminmax(0,1fr)",
		".evidence-logtime{color:var(--cyan-low);white-space:nowrap",
		".label{display:block;width:auto;margin:0;font-weight:700;white-space:nowrap",
	} {
		if !strings.Contains(compactCSS, required) {
			t.Errorf("landing dense-panel CSS missing %q", required)
		}
	}

	if strings.Contains(compactCSS, ".label{display:inline-block;width:7ch") {
		t.Fatal("evidence status column still permits ACCEPTED to wrap")
	}
	if strings.Contains(compactCSS, ".verdict{min-height:16rem") || strings.Contains(compactCSS, ".verdictpre{min-height:8.5rem") {
		t.Fatal("policy verdict still reserves the oversized empty panel")
	}
}

func TestLandingFinalPolishKeepsPublicSurfaceDetachedAndRowsBalanced(t *testing.T) {
	index := string(mustLandingAsset(t, "assets/index.html"))
	css := string(mustLandingAsset(t, "assets/app.css"))
	requestPath := string(mustLandingAsset(t, "assets/request-path.svg"))
	compactCSS := strings.NewReplacer(" ", "", "\n", "", "\t", "", "\r", "").Replace(strings.ToLower(css))

	for _, required := range []string{
		`id="public-landing-outside-request-path"`,
		"OUTSIDE REQUEST PATH",
		`class="card card-wide"`,
		`class="runtime-checks sub prose"`,
		`class="runtime-check"`,
	} {
		if !strings.Contains(index+requestPath, required) {
			t.Errorf("landing final polish missing %q", required)
		}
	}
	for _, required := range []string{
		".card-wide{grid-column:1/-1",
		".runtime-checks{display:flex;flex-wrap:wrap",
		".runtime-check+.runtime-check::before{content:\"·\"",
	} {
		if !strings.Contains(compactCSS, required) {
			t.Errorf("landing final polish CSS missing %q", required)
		}
	}
	if strings.Contains(requestPath, `M890 130 H1060`) || strings.Contains(requestPath, `1050,120 1075,130 1050,140`) {
		t.Fatal("public landing is still connected to the MCP Devbox request path")
	}
	if got := strings.Count(index, `class="runtime-check"`); got != 3 {
		t.Fatalf("runtime independent checks must have exactly three wrapped items, got %d", got)
	}
}

func TestLandingRuntimeFailureStateIsExplicit(t *testing.T) {
	js := string(mustLandingAsset(t, "assets/app.js"))
	for _, required := range []string{
		`fields[key].textContent = "unavailable"`,
		`message.className = "runtime-error warn"`,
		`message.setAttribute("data-runtime-state", "unavailable")`,
		`message.setAttribute("data-runtime-state", "available")`,
		`.catch(unavailable)`,
	} {
		if !strings.Contains(js, required) {
			t.Errorf("landing runtime failure state missing %q", required)
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
