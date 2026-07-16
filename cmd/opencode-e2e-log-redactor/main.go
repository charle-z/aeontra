package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

const (
	maxLines     = 2000
	maxBytes     = 64 << 10
	maxLineBytes = 512
)

var (
	testLinePattern    = regexp.MustCompile(`^(=== RUN|--- PASS:|--- FAIL:|PASS$|FAIL$|ok\s|\?\s)`)
	safeCodePattern    = regexp.MustCompile(`(?:driver_code=|model turn driver operation failed: |internal_)([a-z_]+)`)
	safeFailurePattern = regexp.MustCompile(`(?:slice_code=|edge_failure=)([a-z_]+)`)
	buildStepPattern   = regexp.MustCompile(`^#([0-9]+)(?:\s+\[[^\]]+\])?`)
	quotedPattern      = regexp.MustCompile(`"[^"\\]*(?:\\.[^"\\]*)*"`)
	pathPattern        = regexp.MustCompile(`/(?:[^\s:]+/)+[^\s:]+`)
)

func normalize(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	if match := safeCodePattern.FindStringSubmatch(line); len(match) == 2 {
		return "E2E safe_driver_code=" + match[1]
	}
	if match := safeFailurePattern.FindStringSubmatch(line); len(match) == 2 {
		return "E2E safe_failure_code=" + match[1]
	}
	if testLinePattern.MatchString(line) {
		line = quotedPattern.ReplaceAllString(line, `"<redacted>"`)
		line = pathPattern.ReplaceAllString(line, "<path>")
		return line
	}
	if match := buildStepPattern.FindStringSubmatch(line); len(match) == 2 {
		state := "progress"
		if strings.Contains(line, "ERROR") {
			state = "error"
		} else if strings.Contains(line, "DONE") {
			state = "done"
		}
		return fmt.Sprintf("BUILD step=%s state=%s", match[1], state)
	}
	lowered := strings.ToLower(line)
	categories := []struct {
		token string
		code  string
	}{
		{"timed out", "timeout"},
		{"timeout", "timeout"},
		{"permission denied", "permission"},
		{"not found", "not_found"},
		{"no such file", "not_found"},
		{"exit status", "process_exit"},
		{"docker", "container"},
		{"panic", "panic"},
		{"compile", "compile"},
		{"failed", "failure"},
		{"error", "failure"},
	}
	for _, category := range categories {
		if strings.Contains(lowered, category.token) {
			return "E2E category=" + category.code
		}
	}
	return ""
}

func redact(input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	lines := 0
	written := 0
	for scanner.Scan() {
		line := normalize(scanner.Text())
		if line == "" {
			continue
		}
		encoded := []byte(line)
		if len(encoded) > maxLineBytes {
			encoded = encoded[:maxLineBytes]
		}
		if lines >= maxLines || written+len(encoded)+1 > maxBytes {
			break
		}
		if _, err := output.Write(append(encoded, '\n')); err != nil {
			return err
		}
		lines++
		written += len(encoded) + 1
	}
	return scanner.Err()
}

func main() {
	if err := redact(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "E2E category=redactor_failure")
		os.Exit(1)
	}
}
