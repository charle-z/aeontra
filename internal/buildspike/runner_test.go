//go:build !windows

package buildspike

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunCommandCancelsWholeProcessGroup(t *testing.T) {
	pidFile := t.TempDir() + "/child.pid"
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	plan := BuildCommandPlan{
		Executable: executable,
		Args:       []string{"-test.run=TestBuildSpikeHelperProcess"},
		Environment: []string{
			"BUILD_SPIKE_HELPER=parent",
			"BUILD_SPIKE_PID_FILE=" + pidFile,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err = RunCommand(ctx, plan, nil, 4096)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
	var childPID int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		body, readErr := os.ReadFile(pidFile)
		if readErr == nil {
			childPID, _ = strconv.Atoi(string(body))
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childPID < 2 {
		t.Fatalf("child pid unavailable: %d", childPID)
	}
	for time.Now().Before(deadline) {
		if !processRunning(childPID) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("child process %d survived process-group cancellation", childPID)
}

func TestRunCommandBoundsAndSanitizesOutput(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	plan := BuildCommandPlan{
		Executable: executable,
		Args:       []string{"-test.run=TestBuildSpikeHelperProcess"},
		Environment: []string{
			"BUILD_SPIKE_HELPER=output",
		},
	}
	result, err := RunCommand(context.Background(), plan, []string{"/secret/project"}, 128)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Output) > 128 || !result.Truncated {
		t.Fatalf("len=%d truncated=%v", len(result.Output), result.Truncated)
	}
	for _, forbidden := range []string{"/secret/project", "ghp_", "\x1b", "\x00"} {
		if containsBytes(result.Output, []byte(forbidden)) {
			t.Fatalf("output leaked %q: %q", forbidden, result.Output)
		}
	}
}

func TestBuildSpikeHelperProcess(t *testing.T) {
	mode := os.Getenv("BUILD_SPIKE_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "parent":
		child := exec.Command(os.Args[0], "-test.run=TestBuildSpikeHelperProcess")
		child.Env = append(os.Environ(), "BUILD_SPIKE_HELPER=child")
		if err := child.Start(); err != nil {
			os.Exit(11)
		}
		if err := os.WriteFile(os.Getenv("BUILD_SPIKE_PID_FILE"), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
			os.Exit(12)
		}
		_ = child.Wait()
		os.Exit(0)
	case "child":
		for {
			time.Sleep(time.Second)
		}
	case "output":
		_, _ = fmt.Fprint(os.Stdout, "\x1b[31m/secret/project ghp_abcdefghijklmnopqrstuvwxyz0123456789 \x00")
		for index := 0; index < 1024; index++ {
			_, _ = fmt.Fprint(os.Stdout, "x")
		}
		os.Exit(0)
	default:
		os.Exit(13)
	}
}

func processRunning(pid int) bool {
	body, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err == nil {
		fields := strings.Fields(string(body))
		if len(fields) > 2 && fields[2] == "Z" {
			return false
		}
	}
	err = syscall.Kill(pid, 0)
	return !errors.Is(err, syscall.ESRCH)
}

func containsBytes(body, fragment []byte) bool {
	if len(fragment) == 0 || len(fragment) > len(body) {
		return false
	}
	for index := 0; index+len(fragment) <= len(body); index++ {
		match := true
		for offset := range fragment {
			if body[index+offset] != fragment[offset] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
