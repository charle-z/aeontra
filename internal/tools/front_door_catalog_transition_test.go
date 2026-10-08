package tools

import (
	"testing"

	"github.com/charle-z/mcp-devbox/internal/frontdoorcoordinator"
)

const frontDoorNextCatalog = "sha256:abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd"

func TestPlanManagedFrontDoorCatalogTransitionAcceptsCoordinatorClearedValue(t *testing.T) {
	t.Parallel()
	for _, requested := range []string{frontDoorTestCatalog, frontDoorNextCatalog} {
		plan, err := planManagedFrontDoorCatalogTransition([]coolifyEnvironmentVariable{
			managedCatalogEntry("primary", frontDoorExpectedCatalogKey, frontDoorTestCatalog),
			managedCatalogEntry("transition", frontDoorTransitionCatalogKey, ""),
		}, "coolify-token", requested)
		if err != nil {
			t.Fatal(err)
		}
		if plan.Primary != requested || plan.RemoveUUID != "" {
			t.Fatalf("plan=%+v", plan)
		}
		if requested == frontDoorTestCatalog {
			if plan.Changed || plan.Transition != "" {
				t.Fatalf("cleared transition must be a single-catalog noop: %+v", plan)
			}
		} else if !plan.Changed || plan.Transition != frontDoorTestCatalog {
			t.Fatalf("new rollout must retain exactly the current primary: %+v", plan)
		}
	}
}

func TestManagedCatalogRejectsUnauthenticatedOrInvalidClearedState(t *testing.T) {
	t.Parallel()
	unsigned := managedCatalogEntry("transition", frontDoorTransitionCatalogKey, "")
	unsigned.Comment = ""
	tampered := managedCatalogEntry("transition", frontDoorTransitionCatalogKey, "")
	tampered.Comment += "tampered"
	wrongToken := managedCatalogEntry("transition", frontDoorTransitionCatalogKey, "")
	wrongToken.Comment = frontdoorcoordinator.ManagedEnvironmentComment("other-token", wrongToken.Key, "")
	wrongPosture := managedCatalogEntry("transition", frontDoorTransitionCatalogKey, "")
	wrongPosture.IsBuildtime = true
	for _, entry := range []coolifyEnvironmentVariable{
		unsigned, tampered, wrongToken, wrongPosture,
		managedCatalogEntry("primary", frontDoorExpectedCatalogKey, ""),
		managedCatalogEntry("transition", frontDoorTransitionCatalogKey, " "),
		managedCatalogEntry("transition", frontDoorTransitionCatalogKey, "not-a-hash"),
	} {
		if _, err := authenticatedManagedCatalog(entry, "coolify-token"); err == nil {
			t.Fatalf("invalid empty/catalog state accepted: key=%s", entry.Key)
		}
	}
}

func managedCatalogEntry(uuid, key, value string) coolifyEnvironmentVariable {
	return coolifyEnvironmentVariable{
		UUID: uuid, Key: key, Value: value,
		Comment:   frontdoorcoordinator.ManagedEnvironmentComment("coolify-token", key, value),
		IsLiteral: true, IsRuntime: true,
	}
}

func TestPlanManagedFrontDoorCatalogTransitionAddsCurrentPrimary(t *testing.T) {
	t.Parallel()
	plan, err := planManagedFrontDoorCatalogTransition([]coolifyEnvironmentVariable{
		managedCatalogEntry("primary", frontDoorExpectedCatalogKey, frontDoorTestCatalog),
	}, "coolify-token", frontDoorNextCatalog)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Primary != frontDoorNextCatalog || plan.Transition != frontDoorTestCatalog || plan.RemoveUUID != "" || !plan.Changed {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestPlanManagedFrontDoorCatalogTransitionRetiresOldCatalog(t *testing.T) {
	t.Parallel()
	plan, err := planManagedFrontDoorCatalogTransition([]coolifyEnvironmentVariable{
		managedCatalogEntry("primary", frontDoorExpectedCatalogKey, frontDoorNextCatalog),
		managedCatalogEntry("transition", frontDoorTransitionCatalogKey, frontDoorTestCatalog),
	}, "coolify-token", frontDoorNextCatalog)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Primary != frontDoorNextCatalog || plan.Transition != "" || plan.RemoveUUID != "transition" || !plan.Changed {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestPlanManagedFrontDoorCatalogTransitionAuthenticatesMaskedRuntimeValues(t *testing.T) {
	t.Parallel()
	primary := managedCatalogEntry("primary", frontDoorExpectedCatalogKey, frontDoorNextCatalog)
	transition := managedCatalogEntry("transition", frontDoorTransitionCatalogKey, frontDoorTestCatalog)
	primary.Value = ""
	transition.Value = ""

	plan, err := planManagedFrontDoorCatalogTransition([]coolifyEnvironmentVariable{
		primary,
		transition,
	}, "coolify-token", frontDoorNextCatalog)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Primary != frontDoorNextCatalog || plan.Transition != "" || plan.RemoveUUID != "transition" || !plan.Changed {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestPlanManagedFrontDoorCatalogTransitionRejectsThirdOrUnmanagedCatalog(t *testing.T) {
	t.Parallel()
	maskedTampered := managedCatalogEntry("primary", frontDoorExpectedCatalogKey, frontDoorTestCatalog)
	maskedTampered.Value = ""
	maskedTampered.Comment += "tampered"
	for _, entries := range [][]coolifyEnvironmentVariable{
		{
			managedCatalogEntry("primary", frontDoorExpectedCatalogKey, frontDoorTestCatalog),
			managedCatalogEntry("transition", frontDoorTransitionCatalogKey, "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"),
		},
		{
			{UUID: "primary", Key: frontDoorExpectedCatalogKey, Value: frontDoorTestCatalog, IsLiteral: true, IsRuntime: true},
		},
		{
			managedCatalogEntry("one", frontDoorExpectedCatalogKey, frontDoorTestCatalog),
			managedCatalogEntry("two", frontDoorExpectedCatalogKey, frontDoorTestCatalog),
		},
		{maskedTampered},
	} {
		if _, err := planManagedFrontDoorCatalogTransition(entries, "coolify-token", frontDoorNextCatalog); err == nil {
			t.Fatalf("unsafe managed catalog state accepted: %#v", entries)
		}
	}
}
