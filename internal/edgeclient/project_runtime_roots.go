package edgeclient

import "path/filepath"

// ProjectRuntimeRoots names the private per-workspace mutable roots used by
// development workcells on every supported host.
type ProjectRuntimeRoots struct {
	Runtime   string
	Cache     string
	Artifacts string
}

// projectRuntimeControlRoot is the private compatibility mount for workcell
// instructions, checkpoints, and tool inventory. It is deliberately nested
// below the per-workspace runtime root and is never created in the source
// checkout.
func projectRuntimeControlRoot(roots ProjectRuntimeRoots) string {
	return filepath.Join(filepath.Clean(roots.Runtime), "control")
}

func projectRuntimeStateRoot(roots ProjectRuntimeRoots) string {
	return filepath.Dir(filepath.Dir(filepath.Clean(roots.Runtime)))
}
