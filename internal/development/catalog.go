package development

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
)

const (
	MaxEnvironmentCatalogEntries   = 64
	environmentCatalogDigestDomain = "aeontra-development-environment-catalog-v1\x00"
)

// EnvironmentCatalog is one bounded, immutable snapshot of server-owned
// execution attestations. A source must choose one current attestation per
// environment identity before constructing the catalog.
type EnvironmentCatalog struct {
	environments []EnvironmentAttestation
	digest       string
}

func NewEnvironmentCatalog(environments ...EnvironmentAttestation) (EnvironmentCatalog, error) {
	if len(environments) == 0 || len(environments) > MaxEnvironmentCatalogEntries {
		return EnvironmentCatalog{}, errors.New("development environment catalog is invalid")
	}
	canonical := append([]EnvironmentAttestation(nil), environments...)
	for _, environment := range canonical {
		if !environment.Valid() {
			return EnvironmentCatalog{}, errors.New("development environment catalog contains invalid attestation")
		}
	}
	sort.Slice(canonical, func(i, j int) bool {
		leftTier, _ := canonical[i].Class.Tier()
		rightTier, _ := canonical[j].Class.Tier()
		if leftTier != rightTier {
			return leftTier < rightTier
		}
		if canonical[i].Class != canonical[j].Class {
			return canonical[i].Class < canonical[j].Class
		}
		if canonical[i].EnvironmentID != canonical[j].EnvironmentID {
			return canonical[i].EnvironmentID < canonical[j].EnvironmentID
		}
		return canonical[i].Digest < canonical[j].Digest
	})
	for index := 1; index < len(canonical); index++ {
		if canonical[index-1].EnvironmentID == canonical[index].EnvironmentID {
			return EnvironmentCatalog{}, errors.New("development environment catalog has ambiguous identity")
		}
	}
	catalog := EnvironmentCatalog{environments: canonical}
	catalog.digest = environmentCatalogDigest(canonical)
	return catalog, nil
}

func (catalog EnvironmentCatalog) Environments() []EnvironmentAttestation {
	return append([]EnvironmentAttestation(nil), catalog.environments...)
}

func (catalog EnvironmentCatalog) Digest() string {
	return catalog.digest
}

func (catalog EnvironmentCatalog) Valid() bool {
	if len(catalog.environments) == 0 || len(catalog.environments) > MaxEnvironmentCatalogEntries ||
		catalog.digest == "" {
		return false
	}
	rebuilt, err := NewEnvironmentCatalog(catalog.environments...)
	return err == nil && rebuilt.digest == catalog.digest
}

func (catalog EnvironmentCatalog) FindDigest(digest string) (EnvironmentAttestation, bool) {
	if !catalog.Valid() {
		return EnvironmentAttestation{}, false
	}
	for _, environment := range catalog.environments {
		if environment.Digest == digest {
			return environment, true
		}
	}
	return EnvironmentAttestation{}, false
}

func environmentCatalogDigest(environments []EnvironmentAttestation) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(environmentCatalogDigestDomain))
	for _, environment := range environments {
		_, _ = hash.Write([]byte(environment.EnvironmentID))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(environment.Digest))
		_, _ = hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}
