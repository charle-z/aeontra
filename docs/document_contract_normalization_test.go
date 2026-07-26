package docs_test

import "testing"

func TestContainsNormalizedProseAcceptsWrappedPhrase(t *testing.T) {
	document := "The public landing remains\n\tpresentation-only and never exposes control-plane authority."
	phrase := "The public landing remains presentation-only and never exposes control-plane authority."

	if !containsNormalizedProse(document, phrase) {
		t.Fatal("wrapped prose should satisfy the same semantic phrase")
	}
}

func TestContainsNormalizedProseRejectsMissingWord(t *testing.T) {
	document := "Canonical tool lookup comes before temporary helpers."
	phrase := "Canonical tool lookup always comes before temporary helpers."

	if containsNormalizedProse(document, phrase) {
		t.Fatal("normalization must not ignore a genuinely missing word")
	}
}
