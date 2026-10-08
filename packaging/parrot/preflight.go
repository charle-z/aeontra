package parrot

import _ "embed"

// OnboardingPreflight is shared by the Debian installer and the signed Edge binary.
// Bundle updates therefore cannot leave the CLI using an older package helper.
//
//go:embed onboarding-preflight.sh
var OnboardingPreflight string
