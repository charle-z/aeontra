//go:build opencode_e2e && !windows

package edgeclient

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func bubblewrapFileReadOnly(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return true
	}
	testMode := info.Mode().Perm() ^ 0o200
	if err := os.Chmod(path, testMode); err != nil {
		return true
	}
	_ = os.Chmod(path, info.Mode().Perm())
	return false
}

func bubblewrapTCPListenerFound() bool {
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(file)
		first := true
		for scanner.Scan() {
			if first {
				first = false
				continue
			}
			fields := strings.Fields(scanner.Text())
			if len(fields) > 3 && fields[3] == "0A" {
				_ = file.Close()
				return true
			}
		}
		_ = file.Close()
	}
	return false
}

func readBubblewrapPreflightReport(t *testing.T) bubblewrapPreflightReport {
	t.Helper()
	path := filepath.Join(repoRootForBubblewrapSmoke(t), "artifacts", "opencode-bubblewrap-preflight-report.json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("Bubblewrap preflight report is required before isolation")
	}
	var report bubblewrapPreflightReport
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatal("Bubblewrap preflight report is invalid")
	}
	if report.SchemaVersion != 1 || len(report.Stages) != bubblewrapStageMax-1 || report.BubblewrapVersion == "" {
		t.Fatal("Bubblewrap preflight report is incomplete")
	}
	for _, passed := range report.Stages {
		if !passed {
			t.Fatal("Bubblewrap preflight stage did not pass")
		}
	}
	return report
}
