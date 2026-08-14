package workflowpolicy

import (
	"os"
	"strings"
	"testing"
)

func TestTrustedLinuxWorkcellRootlessDiagnosticsAreFailureOnlyAndRedacted(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/trusted-linux-workcell-e2e.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, required := range []string{
		`P12_SOURCE_SHA: ${{ github.event.pull_request.head.sha || github.sha }}`,
		`ref: ${{ github.event.pull_request.head.sha || github.sha }}`,
		"rootless-e2e-log-redactor",
		`pipeline_status=("${PIPESTATUS[@]}")`,
		`test_status="${pipeline_status[0]}"`,
		`redactor_status="${pipeline_status[1]}"`,
		"P12_RUNTIME_ID_CYCLE_1",
		"P12_RUNTIME_ID_CYCLE_2",
		"Run two clean rootless cycles and restart",
		"TestTrustedLinuxWorkcellRootlessCleanupE2E",
		`-test.run=^TestTrustedLinuxWorkcellRootlessCleanupE2E$ -test.count=1 -test.v`,
		`tee -a artifacts/p12-rootless-test.log`,
		`TestTrustedLinuxWorkcellRootless(?:Cleanup|Restart)?E2E`,
		"trap cleanup EXIT",
		`setsid "$podman_bin" system service`,
		"bubblewrap podman crun conmon uidmap slirp4netns fuse-overlayfs python3-venv",
		`test "$(/usr/bin/podman --version)" = "podman version 3.4.4"`,
		`sudo ln -sfn /usr/bin/podman /usr/local/bin/podman`,
		`test "$(readlink -f /usr/local/bin/podman)" = "/usr/bin/podman"`,
		`test "$(/usr/local/bin/podman --version)" = "podman version 3.4.4"`,
		`P12_PODMAN_BIN=/usr/bin/podman`,
		`P12_ROOTLESS_TOOL_PATH=/usr/bin:/usr/sbin:/bin:/sbin`,
		`containers_conf="$RUNNER_TEMP/p12-rootless-containers.conf"`,
		`printf '[engine]\nruntime = "/usr/bin/crun"\nconmon_path = ["/usr/bin/conmon"]\n' >"$containers_conf"`,
		`test -x /usr/bin/crun`,
		`test -x /usr/bin/conmon`,
		`test "$(CONTAINERS_CONF="$containers_conf" /usr/bin/podman info --format '{{.Host.OCIRuntime.Path}}')" = "/usr/bin/crun"`,
		`test "$(CONTAINERS_CONF="$containers_conf" /usr/bin/podman info --format '{{.Host.Conmon.Path}}')" = "/usr/bin/conmon"`,
		`test -f "$containers_conf"`,
		`test ! -L "$containers_conf"`,
		`test "$(stat -c '%u' "$containers_conf")" = "$(id -u)"`,
		`test "$(stat -c '%a' "$containers_conf")" = "600"`,
		`CONTAINERS_CONF="$containers_conf" /usr/bin/podman --version`,
		`P12_ROOTLESS_CYCLE_CONTAINERS_CONF=$containers_conf`,
		`CONTAINERS_CONF=$containers_conf`,
		`PATH="$P12_ROOTLESS_TOOL_PATH" "$RUNNER_TEMP/podman-compose/bin/podman-compose" --version`,
		`podman_bin="$P12_PODMAN_BIN"`,
		"Verify managed browser with production Chromium path",
		`go test -c -o "$RUNNER_TEMP/p12-browser.test" ./internal/edgeclient`,
		`MCP_DEVBOX_BROWSER_E2E=1 "$RUNNER_TEMP/p12-browser.test"`,
		`-test.run='^TestProjectBrowser(Runner|Manager)AgainstRealChromium$'`,
		"Classify hosted-runner browser harness acceptance",
		`classification="$RUNNER_TEMP/browser-harness-acceptance.tsv"`,
		`source_sha="$P12_SOURCE_SHA"`,
		`tree="$(git rev-parse "${source_sha}^{tree}")"`,
		`/system.slice/*`,
		"GitHub-hosted Ubuntu 22.04 runs the job in a root-owned /system.slice cgroup whose cgroup.procs is not writable by the job user",
		"not-reproducible",
		"c27053c56b6214e52862ead675b874670f322295",
		"Host-specific browser harness acceptance moved to Edge",
		"Upload browser harness acceptance boundary",
		`browser-harness-acceptance-${{ env.P12_SOURCE_SHA }}`,
		`${{ runner.temp }}/browser-harness-acceptance.tsv`,
		"Stage PostgreSQL fixture image",
		`archive="$RUNNER_TEMP/p12-postgres-17-alpine.tar"`,
		`docker image inspect --format '{{.Id}}' docker.io/library/postgres:17-alpine`,
		`docker image tag docker.io/library/postgres:17-alpine "$postgres_image"`,
		`docker image save --output "$archive" "$postgres_image"`,
		`"$podman_bin" --url "unix://$socket" load --input "$archive"`,
		`postgres_image_id="$(python3 - "$archive"`,
		`bundle.extractfile("manifest.json")`,
		`blobs/sha256/`,
		`print("sha256:" + match.group(1))`,
		`postgres_image="localhost/p12-postgres-fixture:${postgres_image_id#sha256:}"`,
		`images_file="$RUNNER_TEMP/p12-podman-images.txt"`,
		`"$podman_bin" --url "unix://$socket" images --no-trunc --format '{{.ID}}'`,
		`loaded_postgres_image_id="$(python3 - "$postgres_image_id" "$images_file"`,
		`matches != {target}`,
		`"$podman_bin" --url "unix://$socket" tag "$loaded_postgres_image_id" "$postgres_image"`,
		`inspected_postgres_image_id="$("$podman_bin" --url "unix://$socket" image inspect --format '{{.Id}}' "$postgres_image")"`,
		`P12 rootless category=postgres_image_inventory`,
		`P12 rootless category=postgres_image_loaded_id`,
		`P12 rootless category=postgres_image_conflict`,
		`P12 rootless category=postgres_image_tag`,
		`P12 rootless category=postgres_image_inspect`,
		`P12 rootless category=postgres_image_identity`,
		`if [ "${inspected_postgres_image_id#sha256:}" != "${postgres_image_id#sha256:}" ]; then`,
		`export P12_POSTGRES_IMAGE="$postgres_image"`,
		"rootless-cycle-1-report.json",
		"rootless-cycle-2-report.json",
		"Annotate rootless E2E failure",
		"Upload rootless failure diagnostic",
		"failure() && hashFiles('artifacts/p12-rootless-test.log') != ''",
		"rm -f artifacts/p12-rootless-test.log",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("rootless diagnostic workflow missing %q", required)
		}
	}
	if strings.Count(text, `ref: ${{ github.event.pull_request.head.sha || github.sha }}`) != 2 {
		t.Error("trusted host and rootless jobs must both check out the exact source head")
	}
	classification := strings.Index(text, "Classify hosted-runner browser harness acceptance")
	browserSmoke := strings.Index(text, "Verify managed browser with production Chromium path")
	if classification < 0 || browserSmoke < 0 || classification >= browserSmoke {
		t.Errorf("host-specific classification must precede Chromium smoke: classification=%d smoke=%d", classification, browserSmoke)
	}
	stageImage := strings.Index(text, "Stage PostgreSQL fixture image")
	disableRootful := strings.Index(text, "sudo chmod 000")
	startRootless := strings.Index(text, "start_service\n")
	loadRootless := strings.Index(text, `"$podman_bin" --url "unix://$socket" load --input`)
	deriveImage := strings.Index(text, `postgres_image_id="$(python3 - "$archive"`)
	localImage := strings.LastIndex(text, `postgres_image="localhost/p12-postgres-fixture:${postgres_image_id#sha256:}"`)
	inventoryImage := strings.Index(text, `"$podman_bin" --url "unix://$socket" images --no-trunc --format '{{.ID}}'`)
	tagImage := strings.Index(text, `"$podman_bin" --url "unix://$socket" tag "$loaded_postgres_image_id" "$postgres_image"`)
	verifyTag := strings.Index(text, `inspected_postgres_image_id="$("$podman_bin" --url "unix://$socket" image inspect --format '{{.Id}}' "$postgres_image")"`)
	exportImage := strings.Index(text, `export P12_POSTGRES_IMAGE="$postgres_image"`)
	runCycles := strings.Index(text, "P12_ROOTLESS_E2E=1")
	if stageImage < 0 || disableRootful < 0 || stageImage >= disableRootful {
		t.Error("PostgreSQL fixture image must be staged before rootful socket isolation")
	}
	if startRootless < 0 || loadRootless < 0 || deriveImage < 0 || localImage < 0 || inventoryImage < 0 || tagImage < 0 || verifyTag < 0 || exportImage < 0 || runCycles < 0 || startRootless >= loadRootless || loadRootless >= deriveImage || deriveImage >= localImage || localImage >= inventoryImage || inventoryImage >= tagImage || tagImage >= verifyTag || verifyTag >= exportImage || exportImage >= runCycles {
		t.Errorf("PostgreSQL fixture ordering invalid: start=%d load=%d derive=%d local=%d inventory=%d tag=%d inspect=%d export=%d run=%d", startRootless, loadRootless, deriveImage, localImage, inventoryImage, tagImage, verifyTag, exportImage, runCycles)
	}
	for _, forbidden := range []string{
		"tail -n 30 artifacts/p12-rootless-test.log",
		"artifacts/p12-podman-service.log\n          if-no-files-found",
		"MCP_DEVBOX_BROWSER_HARNESS_E2E=1",
		"sudo systemd-run",
		"--property=Delegate=yes",
		"P12_TOOLBOX_CGROUP_PARENT",
		"p12-podman-delegated-wrapper.sh",
		`chown "$run_uid:$run_gid" "$root/cgroup.procs"`,
		"github.com/containers/podman/v5@v5.4.2",
		"github.com/containers/crun/releases/download/1.21",
		"continue-on-error",
		"\n          podman --version",
		"\n            podman system migrate",
		"setsid podman system service",
		"\n              if [ -S \"$socket\" ] && podman --url",
		"\n          podman --url",
		`-test.run=^TestTrustedLinuxWorkcellRootlessCleanupE2E$ -test.count=1 >/dev/null`,
		"MCP_DEVBOX_BROWSER_E2E=1 go test ./internal/edgeclient",
		"user_manager=",
		"hosted runner now exposes a user-owned cgroup subtree",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("unsafe rootless diagnostic workflow contains %q", forbidden)
		}
	}

	e2eBody, err := os.ReadFile("../edgeclient/trusted_linux_workcell_rootless_cycles_e2e_test.go")
	if err != nil {
		t.Fatal(err)
	}
	e2e := string(e2eBody)
	for _, required := range []string{"--health-cmd", `[]string{"container", "pod", "network", "volume"}`, "p12RootlessRuntimeIDs", "P12_POSTGRES_IMAGE", "p12PostgresImage", `append(prefix, "image", "inspect", "--format", "{{.Id}}", image)`, "PostgreSQL fixture image identity changed after rootless load", `network := "p12-pg-net-" + suffix`} {
		if !strings.Contains(e2e, required) {
			t.Errorf("rootless lifecycle E2E missing %q", required)
		}
	}
	composeCycle := strings.Index(e2e, "composeRan := p12ComposeCycleE2E")
	postgresNetwork := strings.Index(e2e, `network := "p12-pg-net-" + suffix`)
	if composeCycle < 0 || postgresNetwork < 0 || composeCycle >= postgresNetwork {
		t.Error("PostgreSQL must create a fresh dedicated network after podman-compose cleanup")
	}
}
