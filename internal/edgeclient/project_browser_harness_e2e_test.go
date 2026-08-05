//go:build !windows

package edgeclient

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type browserHarnessE2EDiagnosticRunner struct {
	command string
	output  string
	err     error
}

func (runner *browserHarnessE2EDiagnosticRunner) Run(ctx context.Context, executable string, args, environment []string) ([]byte, error) {
	output, err := (execContainerCommandRunner{}).Run(ctx, executable, args, environment)
	runner.command = executable + " " + strings.Join(args, " ")
	runner.output = boundedBrowserHarnessDiagnostic(string(output))
	runner.err = err
	return output, err
}

func boundedBrowserHarnessDiagnostic(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 2048 {
		value = value[:2048]
	}
	return value
}

func browserHarnessE2ERootlessEnvironment(endpoint *RootlessContainerEndpoint, toolPath string) ([]string, error) {
	environment, err := rootlessContainerClientEnvironment(endpoint, toolPath)
	if err != nil {
		return nil, err
	}
	path := strings.TrimSpace(os.Getenv("P12_ROOTLESS_CLIENT_CONTAINERS_CONF"))
	if path == "" {
		return environment, nil
	}
	path = filepath.Clean(path)
	info, err := os.Lstat(path)
	if err != nil || !filepath.IsAbs(path) || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return nil, ErrProjectToolboxUnsafeState
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return nil, ErrProjectToolboxUnsafeState
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "[engine]\ncgroup_manager=\"cgroupfs\"\n[network]\nnetwork_backend=\"cni\"\n" {
		return nil, ErrProjectToolboxUnsafeState
	}
	return append(environment, "CONTAINERS_CONF="+path), nil
}

func TestBrowserHarnessE2ERootlessEnvironmentAcceptsOnlyManagedCgroupfsCNIConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "containers.conf")
	if err := os.WriteFile(path, []byte("[engine]\ncgroup_manager=\"cgroupfs\"\n[network]\nnetwork_backend=\"cni\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("P12_ROOTLESS_CLIENT_CONTAINERS_CONF", path)
	environment, err := browserHarnessE2ERootlessEnvironment(nil, openCodeDefaultToolPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(environment, "\n"), "CONTAINERS_CONF="+path) {
		t.Fatalf("managed config missing from environment: %v", environment)
	}
	invalid := filepath.Join(t.TempDir(), "invalid.conf")
	if err := os.WriteFile(invalid, []byte("[engine]\ncgroup_manager=\"systemd\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("P12_ROOTLESS_CLIENT_CONTAINERS_CONF", invalid)
	if _, err := browserHarnessE2ERootlessEnvironment(nil, openCodeDefaultToolPath); !errors.Is(err, ErrProjectToolboxUnsafeState) {
		t.Fatalf("invalid managed config err=%v", err)
	}
}

func browserHarnessE2EInstallScript(installBrowser string) string {
	return `set -eu
export DEBIAN_FRONTEND=noninteractive
install -d -m 0700 /var/lib/mcp-devbox
install_log=/var/lib/mcp-devbox/browser-install.log
{
apt-get update
apt-get install -y --no-install-recommends python3 python3-venv python3-pip ca-certificates curl file
python3 -m venv /var/lib/mcp-devbox/browser-python
/var/lib/mcp-devbox/browser-python/bin/pip install --disable-pip-version-check --no-input playwright==1.61.0
` + installBrowser + `
PLAYWRIGHT_BROWSERS_PATH=/var/lib/mcp-devbox/browser-browsers /var/lib/mcp-devbox/browser-python/bin/playwright install --dry-run firefox
} >"$install_log" 2>&1
/var/lib/mcp-devbox/browser-python/bin/python -c 'from playwright.sync_api import sync_playwright; print("dependency-installed")'`
}

func TestProjectBrowserHarnessInstallFixtureCreatesPrivateRootBeforeLogging(t *testing.T) {
	script := browserHarnessE2EInstallScript("playwright install-deps chromium")
	root := strings.Index(script, "install -d -m 0700 /var/lib/mcp-devbox")
	log := strings.Index(script, "install_log=/var/lib/mcp-devbox/browser-install.log")
	redirect := strings.Index(script, `>"$install_log" 2>&1`)
	venv := strings.Index(script, "python3 -m venv /var/lib/mcp-devbox/browser-python")
	if root < 0 || log < 0 || redirect < 0 || venv < 0 || root >= log || log >= redirect || root >= venv {
		t.Fatalf("unsafe install fixture ordering root=%d log=%d redirect=%d venv=%d", root, log, redirect, venv)
	}
}

func TestProjectBrowserHarnessRealPlaywrightE2E(t *testing.T) {
	if os.Getenv("MCP_DEVBOX_BROWSER_HARNESS_E2E") != "1" {
		t.Skip("real rootless browser harness acceptance is opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	endpoint, err := DiscoverRootlessContainerEndpoint(os.Geteuid(), "")
	if err != nil || endpoint == nil {
		t.Fatalf("rootless endpoint err=%v", err)
	}
	stateRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	workspace := Workspace{ID: "ws_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Path: workspaceRoot, Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	hostSentinel := filepath.Join(t.TempDir(), "host-only-sentinel")
	if err := os.WriteFile(hostSentinel, []byte("host-only"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := rootlessContainerClientEnvironment(endpoint, openCodeDefaultToolPath); err != nil {
		t.Fatalf("rootless environment validation failed: %v", err)
	}
	diagnosticRunner := &browserHarnessE2EDiagnosticRunner{}
	manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{StateRoot: stateRoot, Endpoint: endpoint, Runner: diagnosticRunner, environment: browserHarnessE2ERootlessEnvironment, cgroupParent: os.Getenv("P12_TOOLBOX_CGROUP_PARENT")})
	if err != nil {
		t.Fatal(err)
	}
	created, reused, err := manager.Create(ctx, ProjectToolboxCreateRequest{ProjectAlias: "browser-e2e", TargetAlias: "parrot", Workspace: workspace, CPUMillis: 4000, MemoryMiB: 4096, ProcessLimit: 2048})
	if err != nil || reused || created.State != ProjectToolboxRunning || created.CPUMillis != 4000 || created.MemoryMiB != 4096 || created.ProcessLimit != 2048 {
		t.Fatalf("created=%+v reused=%v err=%v last_command=%q last_error=%v last_output=%q", created, reused, err, diagnosticRunner.command, diagnosticRunner.err, diagnosticRunner.output)
	}
	defer func() {
		_, _ = manager.Cleanup(context.Background(), ProjectToolboxCleanupRequest{ProjectAlias: "browser-e2e", TargetAlias: "parrot", Workspace: workspace})
	}()

	installBrowser := "PLAYWRIGHT_BROWSERS_PATH=/var/lib/mcp-devbox/browser-browsers /var/lib/mcp-devbox/browser-python/bin/playwright install --with-deps chromium"
	browserExecutable := ""
	if hostChromium := os.Getenv("P12_CHROMIUM_BIN"); hostChromium != "" {
		info, statErr := os.Stat(hostChromium)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("host Chromium invalid: %v", statErr)
		}
		runtimeRoot := filepath.Join(workspaceRoot, "browser-runtime")
		if err := os.Mkdir(runtimeRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		copy := exec.Command("cp", "-a", filepath.Dir(hostChromium)+"/.", runtimeRoot)
		if output, copyErr := copy.CombinedOutput(); copyErr != nil {
			t.Fatalf("copy Chromium: %v %s", copyErr, output)
		}
		browserExecutable = "/workspace/browser-runtime/" + filepath.Base(hostChromium)
		installBrowser = "/var/lib/mcp-devbox/browser-python/bin/playwright install-deps chromium"
	}
	installScript := browserHarnessE2EInstallScript(installBrowser)
	install := []string{"/bin/sh", "-lc", installScript}
	installed, err := manager.Exec(ctx, ProjectToolboxExecRequest{ProjectAlias: "browser-e2e", TargetAlias: "parrot", Workspace: workspace, Argv: install})
	if err != nil || !strings.Contains(installed.Output, "dependency-installed") {
		t.Fatalf("install output=%q err=%v last_command=%q last_error=%v last_output=%q", installed.Output, err, diagnosticRunner.command, diagnosticRunner.err, diagnosticRunner.output)
	}

	fixtures := filepath.Join(workspaceRoot, "browser-e2e")
	if err := os.Mkdir(fixtures, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtures, "upload.txt"), []byte("upload-payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	serverScript := `from http.server import BaseHTTPRequestHandler, HTTPServer
class H(BaseHTTPRequestHandler):
    def log_message(self, *args): pass
    def html(self, body, code=200, headers=None):
        self.send_response(code)
        for k,v in (headers or {}).items(): self.send_header(k,v)
        self.send_header('Content-Type','text/html; charset=utf-8'); self.end_headers(); self.wfile.write(body.encode())
    def do_GET(self):
        if self.path == '/':
            self.html('''<!doctype html><title>Harness Local</title><form method="post" action="/login"><input id="user" name="user"><button id="login">Login</button></form><input id="upload" type="file"><button id="upload-button" onclick="const f=new FormData();f.append('file',document.querySelector('#upload').files[0]);fetch('/upload',{method:'POST',body:f}).then(r=>r.text()).then(t=>document.querySelector('#result').textContent=t)">Upload</button><a id="download" href="/download">Download</a><main id="result">idle</main>''')
        elif self.path == '/protected':
            self.html('logged-in' if 'auth=ok' in self.headers.get('Cookie','') else 'unauthorized', 200 if 'auth=ok' in self.headers.get('Cookie','') else 401)
        elif self.path == '/download':
            body=b'download-payload'; self.send_response(200); self.send_header('Content-Type','text/plain'); self.send_header('Content-Disposition','attachment; filename="sample.txt"'); self.send_header('Content-Length',str(len(body))); self.end_headers(); self.wfile.write(body)
        else: self.html('not-found',404)
    def do_POST(self):
        length=int(self.headers.get('Content-Length','0')); body=self.rfile.read(length)
        if self.path == '/login': self.html('logged-in',headers={'Set-Cookie':'auth=ok; Path=/; Max-Age=86400; HttpOnly; SameSite=Lax'})
        elif self.path == '/upload': self.html('upload-ok' if b'upload-payload' in body else 'upload-failed',200 if b'upload-payload' in body else 400)
        else: self.html('not-found',404)
HTTPServer(('127.0.0.1',18765),H).serve_forever()
`
	if err := os.WriteFile(filepath.Join(fixtures, "server.py"), []byte(serverScript), 0o600); err != nil {
		t.Fatal(err)
	}
	service, _, err := manager.ServiceStart(ctx, ProjectToolboxServiceStartRequest{ProjectAlias: "browser-e2e", TargetAlias: "parrot", Workspace: workspace, Name: "browser-fixture", Argv: []string{"/var/lib/mcp-devbox/browser-python/bin/python", "server.py"}, CWD: "browser-e2e"})
	if err != nil || service.State != "running" {
		t.Fatalf("service=%+v err=%v", service, err)
	}
	defer manager.ServiceStop(context.Background(), ProjectToolboxServiceRequest{ProjectAlias: "browser-e2e", TargetAlias: "parrot", Workspace: workspace, ServiceID: service.ServiceID})

	firstScript := `import json, os, time
from playwright.sync_api import sync_playwright
run=os.environ['MCP_BROWSER_RUN_DIR']; artifacts=os.environ['MCP_BROWSER_ARTIFACTS_DIR']; downloads=os.environ['MCP_BROWSER_DOWNLOADS_DIR']; profile=os.environ['MCP_BROWSER_PROFILE_DIR']
launch={'headless':True}; browser_bin=os.environ.get('E2E_CHROMIUM_BIN'); launch.update({'executable_path':browser_bin} if browser_bin else {})
upload='/workspace/browser-e2e/upload.txt'
assert not os.path.exists(os.environ['HOST_SENTINEL'])
assert not os.path.exists('/mnt/c')
assert not os.path.exists('/var/run/docker.sock')
with sync_playwright() as p:
    context=p.chromium.launch_persistent_context(profile,accept_downloads=True,downloads_path=downloads,record_video_dir=artifacts,**launch)
    context.tracing.start(screenshots=True,snapshots=True,sources=True)
    page=context.pages[0]
    page.goto('http://127.0.0.1:18765/',wait_until='domcontentloaded')
    page.fill('#user','charlie'); page.click('#login'); page.wait_for_selector('text=logged-in')
    page.goto('http://127.0.0.1:18765/protected'); assert page.text_content('body') == 'logged-in'
    page.goto('http://127.0.0.1:18765/'); page.set_input_files('#upload',upload); page.click('#upload-button'); page.wait_for_selector('text=upload-ok')
    with page.expect_download() as event: page.click('#download')
    event.value.save_as(os.path.join(downloads,'sample.txt'))
    assert open(os.path.join(downloads,'sample.txt')).read() == 'download-payload'
    assert page.evaluate('() => { window.__arbitrary = {ok:true, value: 6*7}; return window.__arbitrary.value }') == 42
    page.screenshot(path=os.path.join(artifacts,'localhost.png'),full_page=True)
    page.pdf(path=os.path.join(artifacts,'localhost.pdf'))
    page.goto('https://example.com',wait_until='domcontentloaded',timeout=30000); assert 'Example Domain' in page.title()
    page.screenshot(path=os.path.join(artifacts,'public.png'))
    context.tracing.stop(path=os.path.join(artifacts,'trace.zip'))
    time.sleep(2)
    context.close()
print(json.dumps({'playwright':'arbitrary','public':True,'localhost':True,'upload':True,'download':True,'javascript':True,'host_isolated':True}),flush=True)
`
	resumeScript := `import json, os
from playwright.sync_api import sync_playwright
with sync_playwright() as p:
    launch={'headless':True}; browser_bin=os.environ.get('E2E_CHROMIUM_BIN'); launch.update({'executable_path':browser_bin} if browser_bin else {})
    context=p.chromium.launch_persistent_context(os.environ['MCP_BROWSER_PROFILE_DIR'],**launch)
    page=context.pages[0]; page.goto('http://127.0.0.1:18765/protected'); assert page.text_content('body') == 'logged-in'
    page.screenshot(path=os.path.join(os.environ['MCP_BROWSER_ARTIFACTS_DIR'],'resumed.png')); context.close()
print(json.dumps({'cookie_persisted':True,'resumed':True}),flush=True)
`
	if err := os.WriteFile(filepath.Join(fixtures, "first.py"), []byte(firstScript), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtures, "resume.py"), []byte(resumeScript), 0o600); err != nil {
		t.Fatal(err)
	}

	firstEnv := map[string]string{"HOST_SENTINEL": hostSentinel}
	if browserExecutable != "" {
		firstEnv["E2E_CHROMIUM_BIN"] = browserExecutable
	}
	first, reused, err := manager.BrowserHarnessStart(ctx, ProjectBrowserHarnessStartRequest{ProjectAlias: "browser-e2e", TargetAlias: "parrot", Workspace: workspace, IdempotencyKey: "real-first", Profile: "login", Argv: []string{"/var/lib/mcp-devbox/browser-python/bin/python", "first.py"}, CWD: "browser-e2e", Environment: firstEnv, TimeoutSeconds: 300, StorageMiB: 1024})
	if err != nil || reused {
		t.Fatalf("first=%+v reused=%v err=%v", first, reused, err)
	}
	reopened, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{StateRoot: stateRoot, Endpoint: endpoint, environment: browserHarnessE2ERootlessEnvironment, cgroupParent: os.Getenv("P12_TOOLBOX_CGROUP_PARENT")})
	if err != nil {
		t.Fatal(err)
	}
	first = waitBrowserHarnessTerminal(t, ctx, reopened, workspace, first.RunID)
	if first.State != "exited" || !first.ExitKnown || first.ExitCode != 0 || !strings.Contains(first.Stdout, `"playwright": "arbitrary"`) {
		t.Fatalf("first terminal=%+v", first)
	}
	artifacts, err := reopened.BrowserHarnessArtifactList(ProjectBrowserHarnessArtifactListRequest{ProjectAlias: "browser-e2e", TargetAlias: "parrot", Workspace: workspace, RunID: first.RunID, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, a := range artifacts {
		paths[a.Path] = true
	}
	for _, want := range []string{"artifacts/localhost.png", "artifacts/localhost.pdf", "artifacts/public.png", "artifacts/trace.zip", "downloads/sample.txt"} {
		if !paths[want] {
			t.Fatalf("missing %s artifacts=%+v", want, artifacts)
		}
	}
	trace, err := reopened.BrowserHarnessArtifactRead(ProjectBrowserHarnessArtifactReadRequest{ProjectAlias: "browser-e2e", TargetAlias: "parrot", Workspace: workspace, RunID: first.RunID, Path: "artifacts/trace.zip", Offset: 0, Limit: 64})
	if err != nil || trace.MediaType != "application/zip" || trace.DataBase64 == "" {
		t.Fatalf("trace=%+v err=%v", trace, err)
	}
	if _, err := base64.StdEncoding.DecodeString(trace.DataBase64); err != nil {
		t.Fatal(err)
	}

	resumeEnv := map[string]string{}
	if browserExecutable != "" {
		resumeEnv["E2E_CHROMIUM_BIN"] = browserExecutable
	}
	second, _, err := reopened.BrowserHarnessStart(ctx, ProjectBrowserHarnessStartRequest{ProjectAlias: "browser-e2e", TargetAlias: "parrot", Workspace: workspace, IdempotencyKey: "real-resume", Profile: "login", Argv: []string{"/var/lib/mcp-devbox/browser-python/bin/python", "resume.py"}, CWD: "browser-e2e", Environment: resumeEnv, TimeoutSeconds: 120, StorageMiB: 256})
	if err != nil {
		t.Fatal(err)
	}
	second = waitBrowserHarnessTerminal(t, ctx, reopened, workspace, second.RunID)
	if second.State != "exited" || !strings.Contains(second.Stdout, `"cookie_persisted": true`) {
		t.Fatalf("second=%+v", second)
	}

	cancelRun, _, err := reopened.BrowserHarnessStart(ctx, ProjectBrowserHarnessStartRequest{ProjectAlias: "browser-e2e", TargetAlias: "parrot", Workspace: workspace, IdempotencyKey: "real-cancel", Profile: "cancel", Argv: []string{"/var/lib/mcp-devbox/browser-python/bin/python", "-c", "import time; print('cancel-ready',flush=True); time.sleep(600)"}, TimeoutSeconds: 900, StorageMiB: 128})
	if err != nil {
		t.Fatal(err)
	}
	cancelRun = waitBrowserHarnessOutput(t, ctx, reopened, workspace, cancelRun.RunID, "cancel-ready")
	stopped, err := reopened.BrowserHarnessStop(ctx, ProjectBrowserHarnessStopRequest{ProjectAlias: "browser-e2e", TargetAlias: "parrot", Workspace: workspace, RunID: cancelRun.RunID, GraceSeconds: 3})
	if err != nil || stopped.State != "stopped" {
		t.Fatalf("stopped=%+v err=%v", stopped, err)
	}
	cleaned, err := reopened.BrowserHarnessCleanup(ProjectBrowserHarnessCleanupRequest{ProjectAlias: "browser-e2e", TargetAlias: "parrot", Workspace: workspace, RemoveProfile: true})
	if err != nil || cleaned.Runs != 3 || cleaned.Profiles != 2 || cleaned.Artifacts < 6 {
		t.Fatalf("cleaned=%+v err=%v", cleaned, err)
	}
}

func waitBrowserHarnessTerminal(t *testing.T, ctx context.Context, manager *ProjectToolboxManager, workspace Workspace, runID string) ProjectBrowserHarnessSnapshot {
	t.Helper()
	var snapshot ProjectBrowserHarnessSnapshot
	for {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		default:
		}
		var err error
		snapshot, err = manager.BrowserHarnessStatus(ctx, ProjectBrowserHarnessStatusRequest{ProjectAlias: "browser-e2e", TargetAlias: "parrot", Workspace: workspace, RunID: runID, StdoutOffset: 0, StderrOffset: 0, Limit: projectBrowserHarnessOutputLimit})
		if err != nil {
			t.Fatal(err)
		}
		if browserHarnessTerminal(snapshot.State) {
			return snapshot
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func waitBrowserHarnessOutput(t *testing.T, ctx context.Context, manager *ProjectToolboxManager, workspace Workspace, runID, want string) ProjectBrowserHarnessSnapshot {
	t.Helper()
	for {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		default:
		}
		snapshot, err := manager.BrowserHarnessStatus(ctx, ProjectBrowserHarnessStatusRequest{ProjectAlias: "browser-e2e", TargetAlias: "parrot", Workspace: workspace, RunID: runID, StdoutOffset: 0, StderrOffset: 0, Limit: projectBrowserHarnessOutputLimit})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(snapshot.Stdout, want) {
			return snapshot
		}
		if browserHarnessTerminal(snapshot.State) {
			t.Fatalf("run ended before output: %+v", snapshot)
		}
		time.Sleep(250 * time.Millisecond)
	}
}
