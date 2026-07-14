package brain

import "path/filepath"

const managedVolumeRoot = "/brain"

func ensureConfiguredBrainRoot(path string) error {
	if isManagedVolumeRoot(path) {
		return ensurePrivateRootDirectory(path)
	}
	return ensurePrivateDirectory(path)
}

func isManagedVolumeRoot(path string) bool {
	return filepath.Clean(path) == filepath.Clean(managedVolumeRoot)
}
