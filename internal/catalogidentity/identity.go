package catalogidentity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
)

const ManifestSchemaVersion = 1

var protocolPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
var catalogPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type Identity struct {
	ProtocolVersion string `json:"protocol_version"`
	ToolCount       int    `json:"tool_count"`
	CatalogHash     string `json:"catalog_hash"`
}

func (i Identity) Validate() error {
	if !protocolPattern.MatchString(i.ProtocolVersion) {
		return errors.New("catalog protocol version is invalid")
	}
	if i.ToolCount < 1 || i.ToolCount > 10000 {
		return errors.New("catalog tool count is invalid")
	}
	if !catalogPattern.MatchString(i.CatalogHash) {
		return errors.New("catalog hash is invalid")
	}
	return nil
}

type Manifest struct {
	SchemaVersion int `json:"schema_version"`
	Identity
}

func DecodeManifest(data []byte) (Manifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decoding catalog identity manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Manifest{}, errors.New("catalog identity manifest contains trailing data")
	}
	if manifest.SchemaVersion != ManifestSchemaVersion {
		return Manifest{}, errors.New("catalog identity manifest schema is unsupported")
	}
	if err := manifest.Identity.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m Manifest) Matches(identity Identity) error {
	if m.SchemaVersion != ManifestSchemaVersion {
		return errors.New("catalog identity manifest schema is unsupported")
	}
	if err := m.Identity.Validate(); err != nil {
		return err
	}
	if err := identity.Validate(); err != nil {
		return err
	}
	if m.Identity != identity {
		return errors.New("catalog identity manifest does not match deterministic runtime identity")
	}
	return nil
}

func EncodeManifest(identity Identity) ([]byte, error) {
	if err := identity.Validate(); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(Manifest{SchemaVersion: ManifestSchemaVersion, Identity: identity}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
