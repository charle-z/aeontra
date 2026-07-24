//go:build !windows

package buildspike

import (
	"errors"
	"path"
	"strings"
)

type ProcessEvidence struct {
	PID     int
	PPID    int
	UID     int
	Cgroup  string
	Command string
}

func ValidateProcessEvidence(expectedUID int, expectedCgroup string, evidence []ProcessEvidence) error {
	if expectedUID <= 0 || expectedCgroup == "" || !strings.HasPrefix(expectedCgroup, "/") || path.Clean(expectedCgroup) != expectedCgroup || len(evidence) < 4 || len(evidence) > 4096 {
		return errors.New("buildspike: process evidence is invalid")
	}
	seen := make(map[int]ProcessEvidence, len(evidence))
	for _, process := range evidence {
		if process.PID <= 1 || process.PPID < 1 || process.UID != expectedUID || process.Cgroup != expectedCgroup || process.Command == "" || len(process.Command) > 128 {
			return errors.New("buildspike: process escaped builder boundary")
		}
		if _, exists := seen[process.PID]; exists {
			return errors.New("buildspike: duplicate process evidence")
		}
		seen[process.PID] = process
	}
	root := evidence[0]
	if root.PPID != 1 {
		return errors.New("buildspike: builder root process is invalid")
	}
	for _, process := range evidence[1:] {
		if _, exists := seen[process.PPID]; !exists {
			return errors.New("buildspike: process ancestry is incomplete")
		}
	}
	return nil
}

func ParseUnifiedCgroup(raw string) (string, error) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) != 1 {
		return "", errors.New("buildspike: unified cgroup membership is ambiguous")
	}
	parts := strings.Split(lines[0], ":")
	if len(parts) != 3 || parts[0] != "0" || parts[1] != "" || !strings.HasPrefix(parts[2], "/") || path.Clean(parts[2]) != parts[2] || strings.Contains(parts[2], "..") {
		return "", errors.New("buildspike: cgroup v2 membership is invalid")
	}
	return parts[2], nil
}
