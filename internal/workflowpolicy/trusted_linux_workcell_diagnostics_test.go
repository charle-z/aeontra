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
		"sudo systemd-run",
		"--collect",
		"--property=Delegate=yes",
		"--property=CPUAccounting=yes",
		"--property=MemoryAccounting=yes",
		"--property=TasksAccounting=yes",
		`catatonit_path="$(command -v catatonit)"`,
		`test -f "$catatonit_path"`,
		`test ! -L "$catatonit_path"`,
		`test -x "$catatonit_path"`,
		"catatonit --version",
		`service_unit="${service_unit_base}-${service_generation}.service"`,
		`delegated_wrapper="$RUNNER_TEMP/p12-podman-delegated-wrapper.sh"`,
		`printf '%s\n' "$$" >"$daemon/cgroup.procs"`,
		`client_containers_conf="$RUNNER_TEMP/p12-rootless-client-containers.conf"`,
		`printf '%s\n' '[engine]' 'cgroup_manager="cgroupfs"' '[network]' 'network_backend="cni"' 'default_rootless_network_cmd="slirp4netns"' >"$client_containers_conf"`,
		`containernetworking-plugins`,
		`export P12_ROOTLESS_CLIENT_CONTAINERS_CONF="$client_containers_conf"`,
		`rm -f "$RUNNER_TEMP/p12-podman-images.txt" "$delegated_wrapper" "$client_containers_conf"`,
		`containers_conf="$8"`,
		`CONTAINERS_CONF="$containers_conf"`,
		`"$HOME" "$client_containers_conf"`,
		`info --format '{{.Host.CgroupManager}}'`,
		`P12 rootless category=cgroup_manager`,
		`CONTAINERS_CONF="$client_containers_conf" podman system migrate`,
		`module="github.com/containers/podman/v5@v5.4.2"`,
		`package="github.com/containers/podman/v5/cmd/podman@v5.4.2"`,
		`go install "$package"`,
		`h1:r1Rv6GiH0YCFS91B+f0DEgEZK8yfzzkjiECVLjLjcuQ=`,
		`be85287fcf4590961614ee37be65eeb315e5d9ff`,
		`GOFLAGS="-tags=cni,seccomp,exclude_graphdriver_btrfs,exclude_graphdriver_devicemapper,containers_image_openpgp"`,
		`sudo install -o root -g root -m 0755 "$GOBIN/podman" /usr/bin/podman`,
		`test "$(podman --version)" = "podman version 5.4.2"`,
		`https://github.com/containers/crun/releases/download/1.21/crun-1.21-linux-amd64`,
		`322c6c94b33a749ccd9f6f5ad48b0dd976eef559bbcfe15afd507a289dcdbe91`,
		`"$crun_root/crun" | sha256sum --check --strict -`,
		`sudo install -o root -g root -m 0755 "$crun_root/crun" /usr/bin/crun`,
		`test "$(crun --version | head -n 1)" = "crun version 1.21"`,
		`printf '+%s\n' "$controller" >"$root/cgroup.subtree_control"`,
		`printf '+%s\n' "$controller" >"$containers/cgroup.subtree_control"`,
		`chown "$run_uid:$run_gid" "$containers"`,
		`chown "$run_uid:$run_gid" "$root/cgroup.procs"`,
		`/usr/bin/setpriv`,
		`/usr/bin/podman system service`,
		`containers_subtree="$containers_cgroup/cgroup.subtree_control"`,
		`export P12_TOOLBOX_CGROUP_PARENT="$control_group/containers"`,
		`P12 rootless category=cgroup_${controller}`,
		`for controller in cpu memory pids; do`,
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
	if strings.Count(text, "podman system migrate") != 1 {
		t.Error("rootless workflow must perform exactly one configured Podman migration")
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
		"systemd-run --user",
		"systemctl --user",
		"user_manager_unit=",
		"loginctl enable-linger",
		"delegate_dropin=",
		"systemctl set-property --runtime",
		`--property="User=$(id -un)"`,
		`--property="Group=$(id -gn)"`,
		`--cgroup-manager=cgroupfs`,
		`podman system reset --force`,
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
