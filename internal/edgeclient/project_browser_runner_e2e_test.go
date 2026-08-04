//go:build !windows

package edgeclient

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "browser-launcher" {
		profile := ""
		chrome := []string{}
		for i := 2; i < len(os.Args); i++ {
			if os.Args[i] == "--profile" && i+1 < len(os.Args) {
				profile = os.Args[i+1]
				i++
				continue
			}
			if os.Args[i] == "--" {
				chrome = append(chrome, os.Args[i+1:]...)
				break
			}
		}
		if err := RunProjectBrowserLauncher(profile, chrome); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestProjectBrowserRunnerAgainstRealChromium(t *testing.T) {
	if os.Getenv("MCP_DEVBOX_BROWSER_E2E") != "1" {
		t.Skip("real Chromium acceptance is opt-in")
	}
	if _, err := os.Stat(projectBrowserChromium); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>Managed Browser E2E</title><input id="name"><button id="go" onclick="document.cookie='managed=ok; path=/';document.querySelector('#result').textContent='hello '+document.querySelector('#name').value">Go</button><main id="result">idle</main>`)
	})
	mux.HandleFunc("/cookie", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>Cookie Check</title><main id="cookie"></main><script>document.querySelector('#cookie').textContent=document.cookie</script>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	profile := filepath.Join(t.TempDir(), "profile")
	if err := os.Mkdir(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	runner := NewProjectBrowserRunner()
	origin := browserOrigin(server.URL)
	first, err := runner.Run(context.Background(), BrowserPageRequest{ProfilePath: profile, NetworkScope: "general", InitialOrigin: origin, ViewportWidth: 900, ViewportHeight: 700, TimeoutSeconds: 60, Capture: "both", Steps: []BrowserStep{{Action: "navigate", URL: server.URL}, {Action: "type", Selector: "#name", SelectorType: "css", Text: "Charlie", Clear: true}, {Action: "click", Selector: "#go", SelectorType: "css"}, {Action: "wait", Selector: "#result", SelectorType: "css"}}})
	if err != nil {
		t.Fatal(err)
	}
	if first.URL != server.URL+"/" || first.Title != "Managed Browser E2E" || !strings.Contains(first.Text, "hello Charlie") || len(first.Screenshot) < 2 || first.Screenshot[0] != 0xff || first.Screenshot[1] != 0xd8 || len(first.Cookies) == 0 {
		t.Fatalf("first=%+v screenshot=%d cookies=%d", first, len(first.Screenshot), len(first.Cookies))
	}
	second, err := runner.Run(context.Background(), BrowserPageRequest{ProfilePath: profile, NetworkScope: "general", InitialOrigin: origin, ViewportWidth: 900, ViewportHeight: 700, TimeoutSeconds: 60, Capture: "text", Cookies: first.Cookies, Steps: []BrowserStep{{Action: "navigate", URL: server.URL + "/cookie"}, {Action: "wait", Selector: "#cookie", SelectorType: "css"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(second.Text, "managed=ok") {
		t.Fatalf("cookie profile did not persist: %q", second.Text)
	}
}

func TestProjectBrowserManagerAgainstRealChromium(t *testing.T) {
	if os.Getenv("MCP_DEVBOX_BROWSER_E2E") != "1" {
		t.Skip("real Chromium acceptance is opt-in")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>Manager E2E</title><input id="name"><button id="go" onclick="document.cookie='durable=yes; path=/';document.querySelector('#result').textContent='saved '+document.querySelector('#name').value">Go</button><main id="result">idle</main>`)
	})
	mux.HandleFunc("/cookie", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><title>Manager Cookie</title><main id="cookie">%s</main>`, r.Header.Get("Cookie"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	workspace := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	resolution := ProjectResolution{Project: Project{Alias: "mcp-devbox", Owner: "charle-z", Repository: "mcp-devbox"}, TargetAlias: "parrot-trusted-linux", Workspace: Workspace{ID: "ws_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Path: workspace, Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}}
	state := filepath.Join(t.TempDir(), "browser-state")
	manager, err := OpenProjectBrowserManager(ProjectBrowserManagerConfig{Root: state, Runner: NewProjectBrowserRunner()})
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := manager.Create(context.Background(), ProjectBrowserCreateRequest{IdempotencyKey: "create-real", Resolution: resolution, NetworkScope: "general", InitialURL: server.URL, ViewportWidth: 900, ViewportHeight: 700})
	if err != nil {
		t.Fatal(err)
	}
	first, err := manager.Run(context.Background(), ProjectBrowserRunRequest{OperationID: "eo_cccccccccccccccccccccccccccccccc", IdempotencyKey: "run-real-1", Resolution: resolution, SessionID: created.SessionID, Capture: "both", TimeoutSeconds: 60, Steps: []BrowserStep{{Action: "navigate", URL: server.URL}, {Action: "type", Selector: "#name", SelectorType: "css", Text: "state", Clear: true}, {Action: "click", Selector: "#go", SelectorType: "css"}, {Action: "wait", Selector: "#result", SelectorType: "css"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first.Text, "saved state") || first.ArtifactID == "" {
		t.Fatalf("first=%+v", first)
	}
	chunk, err := manager.ReadArtifact(ProjectBrowserArtifactReadRequest{Resolution: resolution, SessionID: created.SessionID, ArtifactID: first.ArtifactID, Offset: 0, Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(chunk.DataBase64)
	if err != nil || len(raw) < 2 || raw[0] != 0xff || raw[1] != 0xd8 {
		t.Fatalf("artifact=%x err=%v", raw, err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	manager, err = OpenProjectBrowserManager(ProjectBrowserManagerConfig{Root: state, Runner: NewProjectBrowserRunner()})
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Run(context.Background(), ProjectBrowserRunRequest{OperationID: "eo_dddddddddddddddddddddddddddddddd", IdempotencyKey: "run-real-2", Resolution: resolution, SessionID: created.SessionID, Capture: "text", TimeoutSeconds: 60, Steps: []BrowserStep{{Action: "navigate", URL: server.URL + "/cookie"}, {Action: "wait", Selector: "#cookie", SelectorType: "css"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(second.Text, "durable=yes") {
		t.Fatalf("durable cookie missing: %q", second.Text)
	}
	closed, err := manager.CloseSession(ProjectBrowserCloseRequest{Resolution: resolution, SessionID: created.SessionID})
	if err != nil || closed.State != "closed" {
		t.Fatalf("closed=%+v err=%v", closed, err)
	}
	cleaned, err := manager.Cleanup(ProjectBrowserCleanupRequest{Resolution: resolution, SessionID: created.SessionID})
	if err != nil || cleaned.Removed != 1 || cleaned.Artifacts != 1 {
		t.Fatalf("cleaned=%+v err=%v", cleaned, err)
	}
	_ = manager.Close()
}
