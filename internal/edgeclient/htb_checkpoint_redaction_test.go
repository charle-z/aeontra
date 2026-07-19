package edgeclient

import (
	"strings"
	"testing"
)

func TestSanitizeHTBResumeForModelKeepsHandlesAndRemovesValues(t *testing.T) {
	input := `# Current State

- Credentials: nathan / recovered-lab-password
- Password: recovered-lab-password
- Credential handle: source=loot/capture-0-strings.txt prefix=PASS
- user.txt: 0123456789abcdef0123456789abcdef
- root.txt: pending
- Token: ghp_012345678901234567890123456789012345
- Next action: use local handle
`
	output := sanitizeHTBResumeForModel(input)
	for _, forbidden := range []string{
		"recovered-lab-password",
		"0123456789abcdef0123456789abcdef",
		"ghp_012345678901234567890123456789012345",
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("checkpoint leaked %q: %s", forbidden, output)
		}
	}
	for _, expected := range []string{
		"Credential handle: source=loot/capture-0-strings.txt prefix=PASS",
		"root.txt: pending",
		"Next action: use local handle",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("checkpoint lost %q: %s", expected, output)
		}
	}
}
