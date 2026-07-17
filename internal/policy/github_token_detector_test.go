package policy

import (
	"strings"
	"testing"
	"time"
)

func githubToken(prefix, fill string, length int) string {
	return prefix + "_" + strings.Repeat(fill, length)
}

func TestGitHubTokenSearchPatternFindsEmbeddedSecrets(t *testing.T) {
	token := githubToken("ghp", "A", 36)
	cases := map[string]string{
		"isolated":       token,
		"middle-of-line": "before " + token + " after",
		"json":           `{"authorization":"Bearer ` + token + `"}`,
		"log":            "level=error credential=" + token + " retry=false",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if !githubTokenSearchPattern.MatchString(input) {
				t.Fatalf("embedded GitHub token was not detected in %q", name)
			}
			output, redacted := Redact(input)
			if !redacted || strings.Contains(output, token) {
				t.Fatalf("GitHub token was not redacted in %q", name)
			}
		})
	}
}

func TestGitHubTokenSearchPatternRejectsInvalidCandidates(t *testing.T) {
	cases := map[string]string{
		"invalid-prefix":    githubToken("ghx", "A", 40),
		"short":             githubToken("ghp", "A", 35),
		"invalid-character": "ghp_" + strings.Repeat("A", 18) + "/" + strings.Repeat("A", 18),
		"split-token":       "ghp_" + strings.Repeat("A", 20) + "\n" + strings.Repeat("A", 20),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if githubTokenSearchPattern.MatchString(input) {
				t.Fatalf("invalid GitHub token candidate matched in %q", name)
			}
			if output, redacted := Redact(input); redacted || output != input {
				t.Fatalf("invalid candidate was changed in %q", name)
			}
		})
	}
}

func TestGitHubTokenSearchPatternRedactsMultipleTokens(t *testing.T) {
	first := githubToken("ghp", "A", 36)
	second := githubToken("gho", "B", 40)
	input := "first=" + first + " second=" + second
	output, redacted := Redact(input)
	if !redacted || strings.Contains(output, first) || strings.Contains(output, second) {
		t.Fatal("multiple GitHub tokens were not fully redacted")
	}
	if strings.Count(output, redactPlaceholder) != 2 {
		t.Fatalf("redaction count=%d want=2", strings.Count(output, redactPlaceholder))
	}
}

func TestGitHubTokenSearchPatternLargeNonMatchIsLinear(t *testing.T) {
	input := strings.Repeat("ordinary-log-line-without-credentials\n", 1<<15)
	started := time.Now()
	if githubTokenSearchPattern.MatchString(input) {
		t.Fatal("large clean input unexpectedly matched")
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("large clean input scan took %s", elapsed)
	}
}

func BenchmarkGitHubTokenSearchPatternLargeNonMatch(b *testing.B) {
	input := strings.Repeat("ordinary-log-line-without-credentials\n", 1<<15)
	b.SetBytes(int64(len(input)))
	for i := 0; i < b.N; i++ {
		if githubTokenSearchPattern.MatchString(input) {
			b.Fatal("clean input unexpectedly matched")
		}
	}
}
