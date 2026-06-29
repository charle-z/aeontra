package policy

import "regexp"

// redactPlaceholder replaces detected secret material in returned content.
const redactPlaceholder = "***REDACTED-SECRET***"

// secretRule detects a secret in returned content. If group == 0 the whole match
// is redacted; otherwise only that capture group (so we keep the key but drop the
// value, e.g. `api_key = ***REDACTED-SECRET***`).
type secretRule struct {
	name  string
	re    *regexp.Regexp
	group int
}

// secretRules is the content-scan rule set. It is intentionally conservative
// toward safety: for a security tool, an occasional false-positive redaction is
// preferable to leaking a credential. Order does not matter; all rules are applied.
var secretRules = []secretRule{
	// Whole-block private keys (PEM/OpenSSH/PGP). Multiline.
	{"private-key-block", regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`), 0},
	// Cloud / provider tokens (whole match).
	{"aws-access-key-id", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`), 0},
	{"github-token", regexp.MustCompile(`\bgh[pousr]_[0-9A-Za-z]{36,}\b`), 0},
	{"github-pat", regexp.MustCompile(`\bgithub_pat_[0-9A-Za-z_]{20,}\b`), 0},
	{"slack-token", regexp.MustCompile(`\bxox[baprs]-[0-9A-Za-z-]{10,}\b`), 0},
	{"google-api-key", regexp.MustCompile(`\bAIza[0-9A-Za-z\-_]{35}\b`), 0},
	{"stripe-key", regexp.MustCompile(`\b(?:sk|rk)_live_[0-9A-Za-z]{16,}\b`), 0},
	{"slack-webhook", regexp.MustCompile(`https://hooks\.slack\.com/services/[A-Za-z0-9/]+`), 0},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`), 0},
	// Bearer / Authorization header values (redact the token group).
	{"bearer", regexp.MustCompile(`(?i)\b(?:authorization\s*[:=]\s*)?bearer\s+([0-9A-Za-z._\-]{12,})`), 1},
	// Generic "key = value" credential assignments (redact the value group).
	// The leading class consumes any separator (incl. '_' / '-') so compound
	// names like DB_TOKEN or X-API-KEY are caught; group 2 is the value.
	{"generic-secret-assign", regexp.MustCompile(`(?i)(?:^|[\s"',{}\[\]()_-])(api[_-]?key|apikey|secret[_-]?key|secret|access[_-]?key|auth[_-]?token|client[_-]?secret|password|passwd|pwd|token)\s*[:=]\s*["']?([^\s"',;]{8,})["']?`), 2},
}

// Redact scans content for secret patterns and replaces matches with a placeholder.
// It returns the redacted content and whether anything was redacted. Redact is
// applied to EVERY piece of content returned to the agent — file reads, search
// results, command stdout, diffs — so a secret cannot be exfiltrated even through
// a permitted command (constitution Article I.2; security.md content scanning).
func Redact(content string) (string, bool) {
	redacted := false
	out := content
	for _, rule := range secretRules {
		if rule.group == 0 {
			if rule.re.MatchString(out) {
				redacted = true
				out = rule.re.ReplaceAllString(out, redactPlaceholder)
			}
			continue
		}
		// Redact only the capture group, preserving surrounding text.
		out = rule.re.ReplaceAllStringFunc(out, func(m string) string {
			sub := rule.re.FindStringSubmatchIndex(m)
			if sub == nil || len(sub) < 2*(rule.group+1) {
				return m
			}
			start, end := sub[2*rule.group], sub[2*rule.group+1]
			if start < 0 || end < 0 {
				return m
			}
			redacted = true
			return m[:start] + redactPlaceholder + m[end:]
		})
	}
	return out, redacted
}
