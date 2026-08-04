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
		"rootless-e2e-log-redactor",
		`pipeline_status=("${PIPESTATUS[@]}")`,
		`test_status="${pipeline_status[0]}"`,
		`redactor_status="${pipeline_status[1]}"`,
		"P12_RUNTIME_ID_CYCLE_1",
		"P12_RUNTIME_ID_CYCLE_2",
		"Run two clean rootless cycles and restart",
		"TestTrustedLinuxWorkcellRootlessCleanupE2E",
		"trap cleanup EXIT",
		"systemd-run --user",
		"--property=Delegate=yes",
		`sudo systemctl restart "$user_manager_unit"`,
		`DBUS_SESSION_BUS_ADDRESS="unix:path=$runtime_root/bus"`,
		`Delegate=cpu memory pids`,
		`delegate_dropin="/run/systemd/system/${user_manager_unit}.d/50-p12-rootless-delegate.conf"`,
		`P12 rootless category=cgroup_${controller}`,
		`for controller in cpu memory pids; do`,
		`grep -qw "$controller" "$controllers"`,
		`sudo rm -f "$delegate_dropin"`,
		`service_unit="p12-rootless-podman-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}.service"`,
		"Stage PostgreSQL fixture image",
		`archive="$RUNNER_TEMP/p12-postgres-17-alpine.tar"`,
		`docker image inspect --format '{{.Id}}' docker.io/library/postgres:17-alpine`,
		`docker image tag docker.io/library/postgres:17-alpine "$postgres_image"`,
		`docker image save --output "$archive" "$postgres_image"`,
		`podman --url "unix://$socket" load --input "$archive"`,
		`postgres_image_id="$(python3 - "$archive"`,
		`bundle.extractfile("manifest.json")`,
		`blobs/sha256/`,
		`print("sha256:" + match.group(1))`,
		`postgres_image="localhost/p12-postgres-fixture:${postgres_image_id#sha256:}"`,
		`images_file="$RUNNER_TEMP/p12-podman-images.txt"`,
		`podman --url "unix://$socket" images --no-trunc --format '{{.ID}}'`,
		`loaded_postgres_image_id="$(python3 - "$postgres_image_id" "$images_file"`,
		`matches != {target}`,
		`podman --url "unix://$socket" tag "$loaded_postgres_image_id" "$postgres_image"`,
		`inspected_postgres_image_id="$(podman --url "unix://$socket" image inspect --format '{{.Id}}' "$postgres_image")"`,
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
	stageImage := strings.Index(text, "Stage PostgreSQL fixture image")
	disableRootful := strings.Index(text, "sudo chmod 000")
	startRootless := strings.Index(text, "start_service\n")
	loadRootless := strings.Index(text, `podman --url "unix://$socket" load --input`)
	deriveImage := strings.Index(text, `postgres_image_id="$(python3 - "$archive"`)
	localImage := strings.LastIndex(text, `postgres_image="localhost/p12-postgres-fixture:${postgres_image_id#sha256:}"`)
	inventoryImage := strings.Index(text, `podman --url "unix://$socket" images --no-trunc --format '{{.ID}}'`)
	tagImage := strings.Index(text, `podman --url "unix://$socket" tag "$loaded_postgres_image_id" "$postgres_image"`)
	verifyTag := strings.Index(text, `inspected_postgres_image_id="$(podman --url "unix://$socket" image inspect --format '{{.Id}}' "$postgres_image")"`)
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
