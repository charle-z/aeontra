package app

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/tools"
)

// The administrator selects an exact reviewed runner template. Repository
// manifests and MCP callers never select a credential or a workflow URL.
func loadDevelopmentRunnerConfig() (*tools.DevelopmentRunnerConfig, error) {
	values := make([]string, 6)
	for i, key := range []string{developmentRunnerProfileEnv, developmentRunnerRepositoryEnv, developmentRunnerWorkflowRefEnv,
		developmentRunnerWorkflowSHAEnv, developmentRunnerGenerationEnv, developmentRunnerCalibrationEnv} {
		values[i] = strings.TrimSpace(os.Getenv(key))
	}
	if strings.Join(values, "") == "" {
		return nil, nil
	}
	if values[0] == "" || values[4] == "" || strings.HasPrefix(values[4], "+") || strings.HasPrefix(values[4], "0") {
		return nil, errors.New("development runner configuration is incomplete or noncanonical")
	}
	generation, err := strconv.ParseUint(values[4], 10, 63)
	if err != nil {
		return nil, errors.New("development runner generation is invalid")
	}
	config := tools.DevelopmentRunnerConfig{Profile: values[0], Repository: values[1], WorkflowRef: values[2], WorkflowSHA: values[3],
		Generation: generation, CalibrationEffectID: values[5]}
	if err := tools.ValidateDevelopmentRunnerConfig(config); err != nil {
		return nil, err
	}
	return &config, nil
}
