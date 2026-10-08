package tools

import (
	"errors"
	"strings"
)

// MaintainerProfileCharleZProduction enables the repository owner's fixed production
// release and deployment contracts. It is deliberately opt-in: generic installations
// must not inherit application identifiers, domains, or release policy from the
// maintainer's infrastructure merely because GitHub or Coolify is configured.
const MaintainerProfileCharleZProduction = "charle-z-production"

var errMaintainerProfileDisabled = errors.New("maintainer operation is disabled; configure an explicit maintainer profile")

func (c *serviceCore) configureMaintainerProfile(profile string) {
	c.maintainerProfile = strings.TrimSpace(profile)
}

func (c *serviceCore) maintainerProfileEnabled() bool {
	return c != nil && c.maintainerProfile == MaintainerProfileCharleZProduction
}

func (c *serviceCore) requireMaintainerProfile() error {
	if !c.maintainerProfileEnabled() {
		return errMaintainerProfileDisabled
	}
	return nil
}
