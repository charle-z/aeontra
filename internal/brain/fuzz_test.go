package brain

import (
	"path/filepath"
	"strings"
	"testing"
)

func FuzzValidateSlugNeverAcceptsPathSyntax(f *testing.F) {
	for _, seed := range []string{"safe-note", "../secret", "/absolute", `working\\note`, "a.md", "café", "two--dashes"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, slug string) {
		err := ValidateSlug(slug)
		if err != nil {
			return
		}
		if slug == "" || filepath.Base(slug) != slug || strings.ContainsAny(slug, `/\\._ %`) || strings.HasPrefix(slug, "-") || strings.HasSuffix(slug, "-") || strings.Contains(slug, "--") {
			t.Fatalf("unsafe slug accepted: %q", slug)
		}
		if len(slug) > MaxSlugBytes {
			t.Fatalf("oversized slug accepted: %q", slug)
		}
	})
}

func FuzzExtractLinksRejectsInvalidTargets(f *testing.F) {
	for _, seed := range []string{"[[safe-note]]", "[[../secret]]", "[[a/b]]", "plain text", "[[two--dashes]]"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, body string) {
		links, err := ExtractLinks(body)
		if err != nil {
			return
		}
		for _, link := range links {
			if err := ValidateSlug(link); err != nil {
				t.Fatalf("invalid extracted link %q from %q", link, body)
			}
		}
	})
}
