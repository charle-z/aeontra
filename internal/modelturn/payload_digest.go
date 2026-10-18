package modelturn

import "encoding/json"

// ExactPayloadDigest validates one JSON value and hashes its exact bytes. The
// external provider already canonicalizes its request, so the relay must not
// silently reformat it before comparing the signed digest.
func ExactPayloadDigest(payload json.RawMessage) (string, error) {
	body, err := exactJSON(payload)
	if err != nil {
		return "", ErrInvalidRequest
	}
	return digestBytes(body), nil
}
