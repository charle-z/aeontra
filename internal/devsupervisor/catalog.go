package devsupervisor

import (
	"context"
	"errors"

	"github.com/charle-z/mcp-devbox/internal/development"
)

const MaxAttestationProviders = 16

type AttestationProvider interface {
	Attest(context.Context, development.ObjectiveScope) ([]development.EnvironmentAttestation, error)
}

type CompositeCatalogSource struct {
	providers []AttestationProvider
}

func NewCompositeCatalogSource(providers ...AttestationProvider) (*CompositeCatalogSource, error) {
	if len(providers) == 0 || len(providers) > MaxAttestationProviders {
		return nil, errors.New("development supervisor: attestation provider set is invalid")
	}
	copied := make([]AttestationProvider, len(providers))
	for index, provider := range providers {
		if provider == nil {
			return nil, errors.New("development supervisor: attestation provider set is invalid")
		}
		copied[index] = provider
	}
	return &CompositeCatalogSource{providers: copied}, nil
}

func (source *CompositeCatalogSource) Catalog(ctx context.Context, objective development.Objective) (development.EnvironmentCatalog, error) {
	if source == nil || ctx == nil || ctx.Err() != nil || !objective.Scope.Bound() {
		return development.EnvironmentCatalog{}, ErrCatalogUnavailable
	}
	environments := make([]development.EnvironmentAttestation, 0, len(source.providers)*2)
	for _, provider := range source.providers {
		items, err := provider.Attest(ctx, objective.Scope)
		if err != nil {
			return development.EnvironmentCatalog{}, ErrCatalogUnavailable
		}
		if len(items) > development.MaxEnvironmentCatalogEntries-len(environments) {
			return development.EnvironmentCatalog{}, ErrCatalogUnavailable
		}
		environments = append(environments, items...)
	}
	catalog, err := development.NewEnvironmentCatalog(environments...)
	if err != nil {
		return development.EnvironmentCatalog{}, ErrCatalogUnavailable
	}
	return catalog, nil
}
