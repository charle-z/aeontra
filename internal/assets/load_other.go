//go:build !linux

package assets

import "os"

// Other backend/Edge platforms are intentionally not granted this new authority.
func privateManifestOwner(os.FileInfo) bool { return false }
