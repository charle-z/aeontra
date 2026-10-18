package edgeclient

import (
	"regexp"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/policy"
)

var htbFlagLikeValue = regexp.MustCompile(`(?i)\b[0-9a-f]{32,64}\b`)

func sanitizeHTBResumeForModel(content string) string {
	redacted, _ := policy.Redact(content)
	redacted = htbFlagLikeValue.ReplaceAllString(redacted, "[LOCAL-ONLY-VALUE]")
	lines := strings.Split(redacted, "\n")
	for index, line := range lines {
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(strings.TrimLeft(line[:colon], "- *")))
		value := strings.TrimSpace(line[colon+1:])
		if !htbSensitiveCheckpointKey(key) || htbSafeCheckpointReference(value) {
			continue
		}
		lines[index] = line[:colon+1] + " [LOCAL-ONLY-VALUE]"
	}
	return strings.Join(lines, "\n")
}

func htbSensitiveCheckpointKey(key string) bool {
	for _, fragment := range []string{"credential", "password", "passwd", "pwd", "token", "secret", "user.txt", "root.txt", "flag"} {
		if strings.Contains(key, fragment) {
			return true
		}
	}
	return false
}

func htbSafeCheckpointReference(value string) bool {
	low := strings.ToLower(strings.TrimSpace(value))
	for _, safe := range []string{"", "none", "pending", "obtained", "verified", "not obtained", "not found", "local-only", "[local-only-value]"} {
		if low == safe {
			return true
		}
	}
	return strings.Contains(low, "source=") || strings.Contains(low, "handle=") || strings.Contains(low, "saved=")
}
