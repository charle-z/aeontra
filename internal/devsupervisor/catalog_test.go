package devsupervisor

import (
	"context"
	"errors"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/development"
)

type staticAttestationProvider struct {
	items []development.EnvironmentAttestation
	err   error
	scope development.ObjectiveScope
}

func (provider *staticAttestationProvider) Attest(_ context.Context, scope development.ObjectiveScope) ([]development.EnvironmentAttestation, error) {
	provider.scope = scope
	if provider.err != nil {
		return nil, provider.err
	}
	return append([]development.EnvironmentAttestation(nil), provider.items...), nil
}

func TestCompositeCatalogSourceCombinesProvidersCanonically(t *testing.T) {
	scope, err := development.NewObjectiveScope("project", "parrot")
	if err != nil {
		t.Fatal(err)
	}
	objective := supervisorObjectiveWithScope(t, "objective-catalog-composite", scope)
	l3 := supervisorEnvironment(t, "l3", development.ClassL3Sandbox, 1, "toolchain.go")
	workcell := supervisorEnvironment(t, "workcell", development.ClassWorkcell, 2, "toolchain.go", "network.host-shared")
	left := &staticAttestationProvider{items: []development.EnvironmentAttestation{workcell}}
	right := &staticAttestationProvider{items: []development.EnvironmentAttestation{l3}}
	source, err := NewCompositeCatalogSource(left, right)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := source.Catalog(context.Background(), objective)
	if err != nil {
		t.Fatal(err)
	}
	got := catalog.Environments()
	if len(got) != 2 || got[0].EnvironmentID != "l3" || got[1].EnvironmentID != "workcell" {
		t.Fatalf("catalog=%+v", got)
	}
	if left.scope != scope || right.scope != scope {
		t.Fatalf("scope was not propagated: left=%+v right=%+v", left.scope, right.scope)
	}
}

func TestCompositeCatalogSourceFailsClosedOnProviderErrorOrDuplicateIdentity(t *testing.T) {
	scope, _ := development.NewObjectiveScope("project", "parrot")
	objective := supervisorObjectiveWithScope(t, "objective-catalog-failure", scope)
	workcellV1 := supervisorEnvironment(t, "workcell", development.ClassWorkcell, 1, "toolchain.go")
	workcellV2 := supervisorEnvironment(t, "workcell", development.ClassWorkcell, 2, "toolchain.go", "build.make")

	source, err := NewCompositeCatalogSource(
		&staticAttestationProvider{items: []development.EnvironmentAttestation{workcellV1}},
		&staticAttestationProvider{err: errors.New("offline")},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Catalog(context.Background(), objective); !errors.Is(err, ErrCatalogUnavailable) {
		t.Fatalf("provider failure err=%v", err)
	}

	source, err = NewCompositeCatalogSource(
		&staticAttestationProvider{items: []development.EnvironmentAttestation{workcellV1}},
		&staticAttestationProvider{items: []development.EnvironmentAttestation{workcellV2}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Catalog(context.Background(), objective); !errors.Is(err, ErrCatalogUnavailable) {
		t.Fatalf("duplicate identity err=%v", err)
	}
}

func TestCompositeCatalogSourceRejectsUnscopedObjectiveAndInvalidProviderSet(t *testing.T) {
	if _, err := NewCompositeCatalogSource(); err == nil {
		t.Fatal("empty provider set accepted")
	}
	if _, err := NewCompositeCatalogSource(nil); err == nil {
		t.Fatal("nil provider accepted")
	}
	provider := &staticAttestationProvider{}
	source, err := NewCompositeCatalogSource(provider)
	if err != nil {
		t.Fatal(err)
	}
	policy, _ := development.NewResolutionPolicy(development.TierWorkcell, development.ClassWorkcell)
	objective, _ := development.NewObjective("legacy-unscoped", policy, []development.StepSpec{{
		StepID: "validate", Requirements: supervisorRequirements(t, "toolchain.go"),
	}})
	if _, err := source.Catalog(context.Background(), objective); !errors.Is(err, ErrCatalogUnavailable) {
		t.Fatalf("unscoped objective err=%v", err)
	}
}

func supervisorObjectiveWithScope(t *testing.T, id string, scope development.ObjectiveScope) development.Objective {
	t.Helper()
	policy, err := development.NewResolutionPolicy(development.TierWorkcell, development.ClassL3Sandbox, development.ClassWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	objective, err := development.NewScopedObjective(id, scope, policy, []development.StepSpec{{
		StepID: "validate", Requirements: supervisorRequirements(t, "toolchain.go"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	return objective
}
