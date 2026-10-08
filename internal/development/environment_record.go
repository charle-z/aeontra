package development

import "errors"

// EnvironmentRecord is the bounded, path-free transport of an attestation.
// Parsing recomputes the digest; the record never grants capability by itself.
type EnvironmentRecord struct {
	EnvironmentID string         `json:"environment_id"`
	Class         ExecutionClass `json:"execution_class"`
	Generation    uint64         `json:"generation"`
	Capabilities  []CapabilityID `json:"capabilities"`
	Digest        string         `json:"digest"`
}

func (attestation EnvironmentAttestation) Record() (EnvironmentRecord, error) {
	if !attestation.Valid() {
		return EnvironmentRecord{}, errors.New("development environment attestation is invalid")
	}
	return EnvironmentRecord{EnvironmentID: attestation.EnvironmentID, Class: attestation.Class,
		Generation: attestation.Generation, Capabilities: attestation.Capabilities.IDs(), Digest: attestation.Digest}, nil
}

func (record EnvironmentRecord) Attestation() (EnvironmentAttestation, error) {
	canonical, err := canonicalCapabilityIDs(record.Capabilities)
	if err != nil || !sameCapabilityIDs(canonical, record.Capabilities) {
		return EnvironmentAttestation{}, errors.New("development environment capabilities are not canonical")
	}
	capabilities, err := capabilitySetFromIDs(canonical)
	if err != nil {
		return EnvironmentAttestation{}, err
	}
	attestation, err := NewEnvironmentAttestation(record.EnvironmentID, record.Class, record.Generation, capabilities)
	if err != nil || record.Digest != attestation.Digest {
		return EnvironmentAttestation{}, errors.New("development environment digest is invalid")
	}
	return attestation, nil
}
