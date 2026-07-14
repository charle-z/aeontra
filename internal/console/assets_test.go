package console

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedConsoleAssetsAreSelfContainedAndHardened(t *testing.T) {
	assets := map[string]string{
		"index": string(mustAssetForTest(t, "assets/index.html")),
		"css":   string(mustAssetForTest(t, "assets/app.css")),
		"js":    string(mustAssetForTest(t, "assets/app.js")),
	}
	indexLower := strings.ToLower(assets["index"])
	for _, forbidden := range []string{"http://", "https://", "//cdn", "<style", " style=", " onload=", " onclick="} {
		if strings.Contains(indexLower, forbidden) {
			t.Errorf("index contains forbidden external or inline capability %q", forbidden)
		}
	}
	for _, required := range []string{`id="root"`, "/console/assets/app.css", "/console/assets/app.js"} {
		if !strings.Contains(assets["index"], required) {
			t.Errorf("index missing %q", required)
		}
	}
	assertScriptsAreExternal(t, assets["index"])

	css := strings.ToLower(assets["css"])
	for _, required := range []string{"#0000a8", "border-radius:0", "prefers-reduced-motion", ":focus-visible", "max-width:720px"} {
		if !strings.Contains(css, required) {
			t.Errorf("CSS missing %q", required)
		}
	}
	for _, forbidden := range []string{"linear-gradient", "radial-gradient", "border-radius:999", "backdrop-filter"} {
		if strings.Contains(css, forbidden) {
			t.Errorf("CSS contains forbidden Neo-BIOS styling %q", forbidden)
		}
	}

	js := strings.ToLower(assets["js"])
	for _, required := range []string{
		"mcp devbox operations firmware", "item specific help", "system", "agents", "tasks", "brain", "graph", "edge", "observability", "security", "events",
		"arrowleft", "arrowright", "arrowup", "arrowdown", "f1", "f5", "f8", "f9", "f10", "prefers-reduced-motion", "pointerdown",
	} {
		if !strings.Contains(js, required) {
			t.Errorf("JavaScript missing %q", required)
		}
	}
}

func TestConsoleApplicationSourceAvoidsSensitiveBrowserCapabilities(t *testing.T) {
	root := filepath.Join("..", "..", "web", "console", "src")
	forbidden := []string{
		"http://", "https://", "//cdn", "localstorage", "sessionstorage", "indexeddb",
		"document.cookie", "dangerouslysetinnerhtml", ".innerhtml", "eval(", "new function",
		"serviceworker", "websocket",
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || (!strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".tsx") && !strings.HasSuffix(path, ".css")) {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		lower := strings.ToLower(string(data))
		for _, capability := range forbidden {
			if strings.Contains(lower, capability) {
				t.Errorf("%s contains forbidden application capability %q", path, capability)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertScriptsAreExternal(t *testing.T, index string) {
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
			t.Fatal("console index contains an inline script")
		}
		remaining = remaining[closeAt+len("</script>"):]
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
