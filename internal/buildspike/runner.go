//go:build !windows

package buildspike

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

var environmentNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

type RunResult struct {
	Output    []byte
	Truncated bool
	Duration  time.Duration
}

type boundedCapture struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (capture *boundedCapture) Write(body []byte) (int, error) {
	original := len(body)
	remaining := capture.limit - capture.buffer.Len()
	if remaining > 0 {
		if len(body) > remaining {
			body = body[:remaining]
		}
		_, _ = capture.buffer.Write(body)
	}
	if original > remaining {
		capture.truncated = true
	}
	return original, nil
}

func RunCommand(ctx context.Context, plan BuildCommandPlan, sensitivePaths []string, maximumOutput int) (RunResult, error) {
	if ctx == nil || !filepath.IsAbs(plan.Executable) || filepath.Clean(plan.Executable) != plan.Executable || len(plan.Args) == 0 || len(plan.Args) > 256 || maximumOutput < 64 || maximumOutput > 16<<20 {
		return RunResult{}, errors.New("buildspike: command contract is invalid")
	}
	for _, argument := range plan.Args {
		if argument == "" || len(argument) > 4096 || strings.ContainsRune(argument, '\x00') {
			return RunResult{}, errors.New("buildspike: command argument is invalid")
		}
	}
	environment, err := closedEnvironment(plan.Environment)
	if err != nil {
		return RunResult{}, err
	}
	capture := &boundedCapture{limit: maximumOutput + 1}
	command := exec.CommandContext(ctx, plan.Executable, plan.Args...)
	command.Env = environment
	command.Stdout = capture
	command.Stderr = capture
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		return cancelProcessGroup(command.Process.Pid)
	}
	command.WaitDelay = 5 * time.Second
	started := time.Now()
	runErr := command.Run()
	duration := time.Since(started)
	output, sanitizedTruncated := SanitizeOutput(capture.buffer.Bytes(), sensitivePaths, maximumOutput)
	result := RunResult{Output: output, Truncated: capture.truncated || sanitizedTruncated, Duration: duration}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, ctxErr
	}
	if runErr != nil {
		return result, errors.New("buildspike: builder command failed")
	}
	return result, nil
}

func closedEnvironment(input []string) ([]string, error) {
	if len(input) > 32 {
		return nil, errors.New("buildspike: command environment is too large")
	}
	values := map[string]string{
		"LANG":   "C",
		"LC_ALL": "C",
		"PATH":   "/usr/local/lib/mcp-devbox-builder:/usr/bin:/bin",
	}
	for _, entry := range input {
		if len(entry) > 8192 || strings.ContainsRune(entry, '\x00') {
			return nil, errors.New("buildspike: command environment entry is invalid")
		}
		name, value, found := strings.Cut(entry, "=")
		if !found || !environmentNamePattern.MatchString(name) || strings.ContainsAny(strings.ToUpper(name), "\n\r") {
			return nil, errors.New("buildspike: command environment entry is invalid")
		}
		upper := strings.ToUpper(name)
		if strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") || strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "CREDENTIAL") {
			return nil, errors.New("buildspike: secret-bearing environment is forbidden")
		}
		if _, exists := values[name]; exists {
			return nil, errors.New("buildspike: duplicate command environment entry")
		}
		values[name] = value
	}
	order := []string{"LANG", "LC_ALL", "PATH"}
	for name := range values {
		if name != "LANG" && name != "LC_ALL" && name != "PATH" {
			order = append(order, name)
		}
	}
	// Determinism is useful for tests and audit evidence. Keep the fixed entries first
	// and sort only caller-provided names.
	if len(order) > 3 {
		sortStrings(order[3:])
	}
	result := make([]string, 0, len(order))
	for _, name := range order {
		result = append(result, name+"="+values[name])
	}
	return result, nil
}

func sortStrings(values []string) {
	for index := 1; index < len(values); index++ {
		for current := index; current > 0 && values[current] < values[current-1]; current-- {
			values[current], values[current-1] = values[current-1], values[current]
		}
	}
}
