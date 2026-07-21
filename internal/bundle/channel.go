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
	if channel.Version != 1 || !releasePattern.MatchString(channel.Release) || !commitPattern.MatchString(channel.Commit) ||
		channel.ProtocolVersion == "" || !digestPattern.MatchString(channel.CatalogHash) ||
		(channel.Architecture != "amd64" && channel.Architecture != "arm64") || !digestPattern.MatchString(channel.ArchiveHash) {
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
