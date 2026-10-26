//go:build windows

package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsWSLAvailabilityPlan(t *testing.T) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", filepath.Join("..", "..", "scripts", "test-edge-wsl-availability.ps1"))
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "WSL_AVAILABILITY_PLAN=PASS") {
		t.Fatalf("availability plan: %v %s", err, output)
	}
}
