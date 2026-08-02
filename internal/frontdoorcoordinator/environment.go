package frontdoorcoordinator

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

const managedEnvironmentCommentPrefix = "mcp-devbox-managed-env:v1:"

// ManagedEnvironmentComment returns authenticated metadata for one Coolify
// environment value. Only the fixed non-sensitive compatibility and topology
// keys are embedded in clear text; all other values remain sealed behind HMAC.
func ManagedEnvironmentComment(token, key, value string) string {
	visibility := "sealed"
	encoded := ""
	if managedEnvironmentPublicKey(key) {
		visibility = "public"
		encoded = base64.RawURLEncoding.EncodeToString([]byte(value))
	}
	return managedEnvironmentCommentPrefix + visibility + ":" + encoded + ":" + managedEnvironmentMAC(token, key, value, visibility)
}

// ManagedEnvironmentValue authenticates one managed comment. Public comments
// carry a non-sensitive value; sealed comments require an exact bounded candidate
// supplied by the caller. Candidates also restrict public values to a closed set.
func ManagedEnvironmentValue(comment, token, key string, candidates ...string) (string, error) {
	if !strings.HasPrefix(comment, managedEnvironmentCommentPrefix) {
		return "", errors.New("managed environment comment is absent or invalid")
	}
	parts := strings.Split(strings.TrimPrefix(comment, managedEnvironmentCommentPrefix), ":")
	if len(parts) != 3 {
		return "", errors.New("managed environment comment is malformed")
	}
	visibility, encoded, signature := parts[0], parts[1], parts[2]
	if len(signature) != sha256.Size*2 {
		return "", errors.New("managed environment signature is malformed")
	}
	if _, err := hex.DecodeString(signature); err != nil {
		return "", errors.New("managed environment signature is malformed")
	}

	switch visibility {
	case "public":
		decoded, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil || len(decoded) > 4096 {
			return "", errors.New("managed environment public value is invalid")
		}
		value := string(decoded)
		if !hmac.Equal([]byte(signature), []byte(managedEnvironmentMAC(token, key, value, visibility))) {
			return "", errors.New("managed environment signature does not match")
		}
		if len(candidates) > 0 && !containsManagedEnvironmentCandidate(value, candidates) {
			return "", errors.New("managed environment value is outside the fixed contract")
		}
		return value, nil
	case "sealed":
		if encoded != "" || len(candidates) == 0 {
			return "", errors.New("managed environment sealed value cannot be resolved")
		}
		matched := ""
		for _, candidate := range candidates {
			if hmac.Equal([]byte(signature), []byte(managedEnvironmentMAC(token, key, candidate, visibility))) {
				if matched != "" && matched != candidate {
					return "", errors.New("managed environment value is ambiguous")
				}
				matched = candidate
			}
		}
		if matched == "" {
			return "", errors.New("managed environment signature does not match")
		}
		return matched, nil
	default:
		return "", errors.New("managed environment visibility is invalid")
	}
}

func managedEnvironmentMAC(token, key, value, visibility string) string {
	mac := hmac.New(sha256.New, []byte(strings.TrimSpace(token)))
	_, _ = mac.Write([]byte(strings.TrimSpace(key)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(visibility))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

func managedEnvironmentPublicKey(key string) bool {
	switch strings.TrimSpace(key) {
	case "MCP_FRONT_DOOR_BACKEND_URL",
		"MCP_FRONT_DOOR_EXPECTED_PROTOCOL",
		"MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH",
		"MCP_FRONT_DOOR_TRANSITION_CATALOG_HASH",
		"MCP_FRONT_DOOR_COORDINATOR_TARGET":
		return true
	default:
		return false
	}
}

func containsManagedEnvironmentCandidate(value string, candidates []string) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}
	return false
}
