package profiles

import _ "embed"

// HTBLinuxV1 is the versioned operational contract rendered locally for an
// authorized HTB Linux workspace. It is embedded so the Edge binary does not
// depend on a mutable host-side template file at runtime.
//
//go:embed htb-linux-v1.md
var HTBLinuxV1 string
