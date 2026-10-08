//go:build windows

package edgeclient

func projectDevelopmentRoot(roots WorkspaceRoots) (string, error) {
	workspace, err := OpenWindowsWorkcell(roots.WindowsDev, roots.WindowsDev)
	if err != nil {
		return "", err
	}
	defer workspace.Close()
	return workspace.FinalPath(), nil
}
