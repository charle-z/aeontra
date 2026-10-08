package debian

import (
	"strings"
	"testing"
)

func TestP16PostinstUsesClosedTransactionalEdgeLifecycle(t *testing.T) {
	postinst := repoFile(t, "packaging/debian/postinst.in")
	for _, required := range []string{
		"STATE_MIGRATION_PREPARED=0",
		"EDGE_HOME=\"$(getent passwd \"$EDGE_USER\" | cut -d: -f6)\"",
		"write_edge_home_dropins",
		"ConditionPathExists=",
		"ReadWritePaths=",
		"runuser -u \"$EDGE_USER\" -- env HOME=\"$EDGE_HOME\"",
		"/usr/local/bin/mcp-edge lifecycle \"$operation\"",
		"edge_lifecycle recover-state",
		"edge_lifecycle prepare-state-migration",
		"edge_lifecycle finalize-state-migration",
		"edge_lifecycle rollback-state-migration",
		"edge_lifecycle migration=prepared",
	} {
		if !strings.Contains(postinst, required) {
			t.Errorf("postinst missing closed P16 lifecycle contract %q", required)
		}
	}
	for _, forbidden := range []string{
		"mv \"$LEGACY_STATE\" \"$PREFERRED_STATE\"",
		"chown -hR \"$EDGE_USER:$EDGE_USER\" \"$PREFERRED_STATE\"",
		"rm -rf \"$PREFERRED_STATE\"",
	} {
		if strings.Contains(postinst, forbidden) {
			t.Errorf("postinst still contains unsafe direct state operation %q", forbidden)
		}
	}
}

func TestP16OnboardingContractReusesIdentityWithoutOpaqueIDOutput(t *testing.T) {
	onboard := repoFile(t, "cmd/mcp-edge/onboard_unix.go")
	for _, required := range []string{
		"loadOnboardingIdentity",
		"return identity, \"reused\", nil",
		"alias=%s",
		"existing Edge identity belongs to a different server",
		"existing Edge identity is invalid",
	} {
		if !strings.Contains(onboard, required) {
			t.Errorf("onboarding missing idempotent behavior %q", required)
		}
	}
	if strings.Contains(onboard, "device=%s") {
		t.Fatal("normal onboarding output still exposes opaque device id")
	}
}
