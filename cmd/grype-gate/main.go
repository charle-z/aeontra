// Command grype-gate turns a Grype JSON report into bounded GitHub Actions
// annotations and fails when findings meet the configured severity threshold.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/grypegate"
)

func run(args []string, stdout, stderr io.Writer) int {
	return runAt(args, stdout, stderr, time.Now().UTC())
}

func runAt(args []string, stdout, stderr io.Writer, now time.Time) int {
	flags := flag.NewFlagSet("grype-gate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	reportPath := flags.String("report", "", "Grype JSON report path")
	minimumText := flags.String("minimum", "high", "minimum severity: negligible, low, medium, high, or critical")
	annotationFile := flags.String("annotation-file", "Dockerfile", "repository file attached to GitHub annotations")
	acceptedRiskPath := flags.String("accepted-risk", "", "reviewed, expiring workcell risk policy; reports remain unchanged")
	imageID := flags.String("image-id", "", "verified OCI config digest for the scanned image; required for accepted risk")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*reportPath) == "" {
		fmt.Fprintln(stderr, "grype-gate: --report is required")
		return 2
	}
	minimum, err := grypegate.ParseSeverity(*minimumText)
	if err != nil {
		fmt.Fprintf(stderr, "grype-gate: %v\n", err)
		return 2
	}
	file, err := os.Open(*reportPath)
	if err != nil {
		fmt.Fprintf(stderr, "grype-gate: open report: %v\n", err)
		return 1
	}
	defer file.Close()

	var findings, accepted []grypegate.Finding
	var evaluationErr error
	var risk grypegate.AcceptedRisk
	if *acceptedRiskPath != "" {
		policy, openErr := os.Open(*acceptedRiskPath)
		if openErr != nil {
			fmt.Fprintf(stderr, "grype-gate: open risk policy: %v\n", openErr)
			return 1
		}
		defer policy.Close()
		definition, readErr := os.ReadFile(*annotationFile)
		if readErr != nil {
			fmt.Fprintf(stderr, "grype-gate: read image definition: %v\n", readErr)
			return 1
		}
		findings, accepted, risk, evaluationErr = grypegate.EvaluateAcceptedRisk(file, minimum, policy, *annotationFile, definition, *imageID, now)
	} else {
		if *imageID != "" {
			fmt.Fprintln(stderr, "grype-gate: --image-id requires --accepted-risk")
			return 2
		}
		findings, evaluationErr = grypegate.Evaluate(file, minimum)
	}
	for _, finding := range accepted {
		annotation := strings.Replace(finding.GitHubAnnotation(*annotationFile), "::error ", "::warning ", 1)
		fmt.Fprintf(stdout, "%s accepted risk; NOT FIXED; owner=%s expires=%s\n", annotation, risk.Owner, risk.ExpiresAt)
	}
	for _, finding := range findings {
		fmt.Fprintln(stdout, finding.GitHubAnnotation(*annotationFile))
	}
	if evaluationErr != nil {
		fmt.Fprintf(stderr, "grype-gate: %v\n", evaluationErr)
		return 1
	}
	if len(accepted) > 0 {
		fmt.Fprintf(stdout, "PASS no unaccepted vulnerabilities at or above %s; accepted_risks=%d\n", minimum, len(accepted))
	} else {
		fmt.Fprintf(stdout, "PASS no vulnerabilities at or above %s\n", minimum)
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
