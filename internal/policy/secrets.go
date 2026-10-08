package policy

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrSecretDenied is returned when a path is denied because it names secret
// material (by path); content scanning is the second, independent layer.
var ErrSecretDenied = errors.New("policy: access to secret path denied")

// secretSegments are directory/file names that, if they appear as ANY path
// segment, mark the whole path as secret (e.g. ".ssh/known_hosts" is denied).
var secretSegments = map[string]bool{
	".ssh":        true,
	".aws":        true,
	".gnupg":      true,
	".gpg":        true,
	"grant-admin": true,
}

// secretBasenames are exact (case-insensitive) file names that are always secret.
var secretBasenames = map[string]bool{
	".env":             true,
	".npmrc":           true,
	".netrc":           true,
	"_netrc":           true,
	".git-credentials": true,
	".pgpass":          true,
	".htpasswd":        true,
	"credentials":      true,
	"id_rsa":           true,
	"id_dsa":           true,
	"id_ecdsa":         true,
	"id_ed25519":       true,
	"secring.gpg":      true,
}

// secretExts are file extensions that indicate key/credential material.
var secretExts = map[string]bool{
	".pem":      true,
	".key":      true,
	".pfx":      true,
	".p12":      true,
	".keystore": true,
	".jks":      true,
	".asc":      true,
}

// IsSecretPath reports whether a path must never be read or returned, based on its
// name alone (defense layer 1; content scanning is layer 2). Matching is
// case-insensitive and segment-aware so traversal/casing tricks do not bypass it.
func IsSecretPath(path string) bool {
	if path == "" {
		return false
	}
	// Normalize separators and split into segments.
	norm := strings.ReplaceAll(path, "\\", "/")
	segs := strings.Split(norm, "/")
	for _, seg := range segs {
		if seg == "" || seg == "." || seg == ".." {
			continue
		}
		low := strings.ToLower(seg)
		if secretSegments[low] {
			return true
		}
		if secretBasenames[low] {
			return true
		}
		// ".env.local", ".env.production", etc.
		if strings.HasPrefix(low, ".env.") {
			return true
		}
		if secretExts[strings.ToLower(filepath.Ext(seg))] {
			return true
		}
	}
	return false
}
