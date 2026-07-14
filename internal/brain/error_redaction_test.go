package brain

import (
	"strings"
	"testing"
)

func TestValidationErrorsDoNotEchoUntrustedValues(t *testing.T) {
	secret := "github_pat_0123456789abcdefghijklmnopQRSTUV"
	if err := ValidateSlug("../" + secret); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("slug error leaked input: %v", err)
	}
	if _, err := ExtractLinks("[[../" + secret + "]]"); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("link error leaked input: %v", err)
	}
	if _, err := ParseNote([]byte(strings.Replace(validSource("safe-note", TrustCurated, AuthorOwner), "type: fact", "type: "+secret, 1)), "safe-note", TrustCurated, fixedNow); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("type error leaked input: %v", err)
	}
}
