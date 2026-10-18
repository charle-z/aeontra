package workflowpolicy

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestFrontDoorCoordinatorVolumeBootstrapDropsPrivileges(t *testing.T) {
	dockerfile, err := os.ReadFile("../../Dockerfile.front-door-coordinator")
	if err != nil {
		t.Fatal(err)
	}
	entrypoint, err := os.ReadFile("../../scripts/front-door-coordinator-entrypoint.sh")
	if err != nil {
		t.Fatal(err)
	}
	smoke, err := os.ReadFile("../../scripts/test-front-door-coordinator-volume.sh")
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("sh", "-n", "../../scripts/front-door-coordinator-entrypoint.sh").CombinedOutput(); err != nil {
		t.Fatalf("entrypoint shell syntax: %v: %s", err, output)
	}
	if output, err := exec.Command("sh", "-n", "../../scripts/test-front-door-coordinator-volume.sh").CombinedOutput(); err != nil {
		t.Fatalf("volume smoke shell syntax: %v: %s", err, output)
	}

	for _, required := range []string{
		"apk add --no-cache ca-certificates su-exec",
		"USER 0:0",
		`ENTRYPOINT ["/usr/local/bin/mcp-front-door-coordinator-entrypoint"]`,
	} {
		if !strings.Contains(string(dockerfile), required) {
			t.Errorf("Dockerfile.front-door-coordinator does not contain %q", required)
		}
	}
	for _, required := range []string{
		`state_root="${MCP_FRONT_DOOR_COORDINATOR_STATE_ROOT:-/coordinator-state}"`,
		`[ "$state_root" != "/coordinator-state" ]`,
		`[ -L "$state_root" ]`,
		`chown 10003:10003 "$state_root"`,
		`chmod 0700 "$state_root"`,
		`[ ! -f "$journal" ] || [ -L "$journal" ]`,
		`exec su-exec 10003:10003 /usr/local/bin/mcp-front-door-coordinator`,
	} {
		if !strings.Contains(string(entrypoint), required) {
			t.Errorf("coordinator entrypoint does not contain %q", required)
		}
	}
	for _, required := range []string{
		`docker volume create "$volume"`,
		`--env COOLIFY_URL=http+host-gateway://control.example:1`,
		`--volume "$volume:/coordinator-state"`,
		`awk '/^Uid:/ {print $2; exit}' /proc/1/status`,
		`su-exec 10003:10003 sh -c`,
		`.write-probe`,
	} {
		if !strings.Contains(string(smoke), required) {
			t.Errorf("coordinator volume smoke does not contain %q", required)
		}
	}
	if strings.Contains(string(smoke), "COOLIFY_URL=http://control.example") {
		t.Error("coordinator smoke uses a public HTTP Coolify origin")
	}
	if strings.Contains(string(smoke), "--add-host") {
		t.Error("coordinator smoke still depends on a Docker host alias")
	}
	for _, forbidden := range []string{"chown -R", "chmod -R", "eval ", "exec sh", "exec /bin/sh"} {
		if strings.Contains(string(entrypoint), forbidden) {
			t.Errorf("coordinator entrypoint contains forbidden %q", forbidden)
		}
	}
}
