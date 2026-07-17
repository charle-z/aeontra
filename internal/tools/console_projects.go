package tools

import "path/filepath"

// ConsoleProjectCount returns only the number of real configured policy roots.
// Paths, names and repository metadata remain inside the policy boundary.
func (c *RepositoryCapability) ConsoleProjectCount() int {
	if c == nil || c.serviceCore == nil || c.pol == nil {
		return 0
	}
	return len(c.pol.Roots())
}

// ConsoleCurrentProjectIndex identifies the actual service root by index only.
// It never returns a path, repository name or other filesystem metadata.
func (c *RepositoryCapability) ConsoleCurrentProjectIndex() int {
	if c == nil || c.serviceCore == nil || c.pol == nil {
		return -1
	}
	current := filepath.Clean(c.root)
	for index, root := range c.pol.Roots() {
		if filepath.Clean(root) == current {
			return index
		}
	}
	return -1
}
