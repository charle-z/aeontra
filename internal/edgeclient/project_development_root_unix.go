//go:build !windows

package edgeclient

func projectDevelopmentRoot(roots WorkspaceRoots) (string, error) {
	return ValidateRegisteredWorkspace(roots.Dev)
}
