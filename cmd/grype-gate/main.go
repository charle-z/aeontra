// Command grype-gate turns a Grype JSON report into bounded GitHub Actions
// annotations and fails when findings meet the configured severity threshold.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/grypegate"
)

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("grype-gate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	reportPath := flags.String("report", "", "Grype JSON report path")
	minimumText := flags.String("minimum", "high", "minimum severity: negligible, low, medium, high, or critical")
	annotationFile := flags.String("annotation-file", "Dockerfile", "repository file attached to GitHub annotations")
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

	findings, evaluationErr := grypegate.Evaluate(file, minimum)
	for _, finding := range findings {
		fmt.Fprintln(stdout, finding.GitHubAnnotation(*annotationFile))
	}
	if evaluationErr != nil {
		fmt.Fprintf(stderr, "grype-gate: %v\n", evaluationErr)
		return 1
	}
	fmt.Fprintf(stdout, "PASS no vulnerabilities at or above %s\n", minimum)
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
