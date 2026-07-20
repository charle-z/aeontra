package bundle

import (
	"crypto/ed25519"
	"encoding/json"
)

type Channel struct {
	Version         int    `json:"version"`
	Release         string `json:"release"`
	Commit          string `json:"commit"`
	ProtocolVersion string `json:"protocol_version"`
	CatalogHash     string `json:"catalog_hash"`
	Architecture    string `json:"architecture"`
	ArchiveHash     string `json:"archive_hash"`
}

func CanonicalChannel(channel Channel) ([]byte, error) {
	manifest := Manifest{
		Version: channel.Version, Release: channel.Release, Commit: channel.Commit,
		ProtocolVersion: channel.ProtocolVersion, CatalogHash: channel.CatalogHash,
		Architecture: channel.Architecture, Components: map[string]string{},
	}
	for _, component := range RequiredComponents() {
		manifest.Components[component] = channel.ArchiveHash
	}
	if _, err := canonicalManifest(manifest); err != nil || !digestPattern.MatchString(channel.ArchiveHash) {
		return nil, &VerificationError{Code: ManifestInvalid}
	}
	return json.Marshal(channel)
}

func SignChannel(channel Channel, privateKey ed25519.PrivateKey) ([]byte, []byte, error) {
	canonical, err := CanonicalChannel(channel)
	if err != nil {
		return nil, nil, err
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, nil, &VerificationError{Code: ManifestInvalid}
	}
	return canonical, ed25519.Sign(privateKey, canonical), nil
}
