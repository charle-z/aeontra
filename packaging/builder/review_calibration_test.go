//go:build !windows

package builder

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const calibrationSummaryHeader = "commit\tquota_percent\tmode\tduration_ms\texit_status\tcpu_usage_usec\tcpu_throttled_usec\tnr_throttled\tmemory_peak_bytes\tmemory_high_events\toom_kills\tcpu_pressure_total\tio_pressure_total\tpids_peak\thealth_samples\thealth_failures\thttp_502\thealth_max_seconds\thealth_avg_seconds\tcache_bytes\tartifact_bytes\tartifact_sha256\n"

func TestCalibrationReviewSelectsLowestQuotaWithinReviewedDurationPolicy(t *testing.T) {
	directory := writeCalibrationEvidence(t, []string{
		calibrationRow(50, "no-cache", 150000, 0, 0, 0),
		calibrationRow(50, "cached", 60000, 0, 0, 0),
		calibrationRow(65, "no-cache", 130000, 0, 0, 0),
		calibrationRow(65, "cached", 48000, 0, 0, 0),
		calibrationRow(80, "no-cache", 100000, 0, 0, 0),
		calibrationRow(80, "cached", 40000, 0, 0, 0),
	})

	output, err := runCalibrationReview(directory)
	if err != nil {
		t.Fatalf("review failed: %v: %s", err, output)
	}
	if !strings.Contains(output, "selected quota: 65%") {
		t.Fatalf("unexpected review output %q", output)
	}
	selected, err := os.ReadFile(filepath.Join(directory, "selected-quota-percent"))
	if err != nil {
		t.Fatal(err)
	}
	if string(selected) != "65\n" {
		t.Fatalf("selected quota=%q want 65", selected)
	}
	selection, err := os.ReadFile(filepath.Join(directory, "selection.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(selection), "\t50\tyes\tno\t") || !strings.Contains(string(selection), "\t65\tyes\tyes\t") || !strings.Contains(string(selection), "\t80\tyes\tyes\t") {
		t.Fatalf("selection matrix is incomplete:\n%s", selection)
	}
}

func TestCalibrationReviewRequiresCachedBuildEvidence(t *testing.T) {
	directory := writeCalibrationEvidence(t, []string{
		calibrationRow(50, "no-cache", 150000, 0, 0, 0),
		calibrationRow(50, "cached", 60000, 0, 0, 0),
		calibrationRow(65, "no-cache", 110000, 0, 0, 0),
		calibrationRow(65, "cached", 45000, 0, 0, 0),
		calibrationRow(80, "no-cache", 100000, 0, 0, 0),
		calibrationRow(80, "cached", 40000, 0, 0, 0),
	})
	if err := os.WriteFile(filepath.Join(directory, "q65-cached", "build.log"), []byte("#1 DONE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := runCalibrationReview(directory)
	if err != nil {
		t.Fatalf("review failed: %v: %s", err, output)
	}
	selected, err := os.ReadFile(filepath.Join(directory, "selected-quota-percent"))
	if err != nil {
		t.Fatal(err)
	}
	if string(selected) != "80\n" {
		t.Fatalf("selected quota=%q want 80 when q65 has no cache reuse", selected)
	}
}

func TestCalibrationReviewRejectsIncompleteDuplicateAndHardFailureEvidence(t *testing.T) {
	validRows := []string{
		calibrationRow(50, "no-cache", 100000, 0, 0, 0),
		calibrationRow(50, "cached", 40000, 0, 0, 0),
		calibrationRow(65, "no-cache", 90000, 0, 0, 0),
		calibrationRow(65, "cached", 35000, 0, 0, 0),
		calibrationRow(80, "no-cache", 80000, 0, 0, 0),
		calibrationRow(80, "cached", 30000, 0, 0, 0),
	}

	cases := [][]string{
		validRows[:5],
		append(append([]string{}, validRows[:5]...), validRows[0]),
		{
			calibrationRow(50, "no-cache", 100000, 0, 1, 0),
			calibrationRow(50, "cached", 40000, 0, 0, 0),
			calibrationRow(65, "no-cache", 90000, 0, 1, 0),
			calibrationRow(65, "cached", 35000, 0, 1, 0),
			calibrationRow(80, "no-cache", 80000, 1, 0, 0),
			calibrationRow(80, "cached", 30000, 1, 0, 0),
		},
	}

	for index, rows := range cases {
		directory := writeCalibrationEvidence(t, rows)
		output, err := runCalibrationReview(directory)
		if err == nil {
			t.Fatalf("invalid case %d accepted: %s", index, output)
		}
	}
}

func TestCalibrationReviewScriptIsClosedAndExecutable(t *testing.T) {
	script := readFixture(t, "review-vps-calibration.sh")
	for _, required := range []string{
		"set -eu",
		"MAX_CACHE_BYTES=4294967296",
		"NO_CACHE_PERCENT=135",
		"CACHED_PERCENT=125",
		"exactly one evidence directory is required",
		"summary must contain exactly six measurements",
		"no quota satisfies the reviewed policy",
		"selected-quota-percent",
		"selection-policy",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("reviewer missing %q", required)
		}
	}
	for _, forbidden := range []string{"eval ", "sh -c", "bash -c", "curl ", "git ", "systemctl ", "sudo "} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("reviewer contains forbidden %q", forbidden)
		}
	}
	assertExecutable(t, "review-vps-calibration.sh")
}

func writeCalibrationEvidence(t *testing.T, rows []string) string {
	t.Helper()
	directory := t.TempDir()
	commit := strings.Repeat("a", 40)
	if err := os.WriteFile(filepath.Join(directory, "commit"), []byte(commit+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "summary.tsv"), []byte(calibrationSummaryHeader+strings.Join(rows, "")), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, quota := range []string{"50", "65", "80"} {
		directoryPath := filepath.Join(directory, "q"+quota+"-cached")
		if err := os.Mkdir(directoryPath, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directoryPath, "build.log"), []byte("#1 CACHED\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return directory
}

func calibrationRow(quota int, mode string, duration int, exitStatus int, healthFailures int, http502 int) string {
	commit := strings.Repeat("a", 40)
	return fmt.Sprintf("%s\t%d\t%s\t%d\t%d\t100000\t1000\t1\t268435456\t0\t0\t10\t10\t20\t5\t%d\t%d\t0.100000\t0.050000\t1048576\t2097152\tsha256:%s\n", commit, quota, mode, duration, exitStatus, healthFailures, http502, strings.Repeat("b", 64))
}

func runCalibrationReview(directory string) (string, error) {
	command := exec.Command("./review-vps-calibration.sh", directory)
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	output, err := command.CombinedOutput()
	return string(output), err
}
