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
		"setsid podman system service",
		"Stage PostgreSQL fixture image",
		`archive="$RUNNER_TEMP/p12-postgres-17-alpine.tar"`,
		`docker image save --output "$archive"`,
		`podman --url "unix://$socket" load --input "$archive"`,
		`postgres_image_id="$(python3 - "$archive"`,
		`bundle.extractfile("manifest.json")`,
		`blobs/sha256/`,
		`print("sha256:" + match.group(1))`,
		`podman --url "unix://$socket" image exists "$postgres_image_id"`,
		`postgres_image="localhost/p12-postgres-fixture:${postgres_image_id#sha256:}"`,
		`podman --url "unix://$socket" tag "$postgres_image_id" "$postgres_image"`,
		`image inspect --format '{{.Id}}' "$postgres_image"`,
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
	verifyRootless := strings.Index(text, `podman --url "unix://$socket" image exists "$postgres_image_id"`)
	tagImage := strings.Index(text, `podman --url "unix://$socket" tag "$postgres_image_id" "$postgres_image"`)
	verifyTag := strings.Index(text, `image inspect --format '{{.Id}}' "$postgres_image"`)
	exportImage := strings.Index(text, `export P12_POSTGRES_IMAGE="$postgres_image"`)
	runCycles := strings.Index(text, "P12_ROOTLESS_E2E=1")
	if stageImage < 0 || disableRootful < 0 || stageImage >= disableRootful {
		t.Error("PostgreSQL fixture image must be staged before rootful socket isolation")
	}
	if startRootless < 0 || loadRootless < 0 || deriveImage < 0 || verifyRootless < 0 || tagImage < 0 || verifyTag < 0 || exportImage < 0 || runCycles < 0 || startRootless >= loadRootless || loadRootless >= deriveImage || deriveImage >= verifyRootless || verifyRootless >= tagImage || tagImage >= verifyTag || verifyTag >= exportImage || exportImage >= runCycles {
		t.Error("PostgreSQL fixture image must load, derive its immutable ID, bind a digest-derived local tag, verify that tag, and export it before the E2E cycles")
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
	for _, required := range []string{"--health-cmd", `[]string{"container", "pod", "network", "volume"}`, "p12RootlessRuntimeIDs", "P12_POSTGRES_IMAGE", "p12PostgresImage", `append(prefix, "image", "exists", image)`, `network := "p12-pg-net-" + suffix`} {
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
