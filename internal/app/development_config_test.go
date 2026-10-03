package app

import (
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/tools"
)

func TestDevelopmentRunnerConfigDefaultsDisabledAndRequiresExactAdminPin(t *testing.T) {
	clearRuntimeEnv(t)
	config, err := loadDevelopmentRunnerConfig()
	if err != nil || config != nil {
		t.Fatal("runner enabled without explicit administrator configuration")
	}
	t.Setenv(developmentRunnerRepositoryEnv, "aeontra")
	if _, err := loadDevelopmentRunnerConfig(); err == nil {
		t.Fatal("partial configuration was silently ignored")
	}
	t.Setenv(developmentRunnerProfileEnv, tools.DevelopmentRunnerProfile)
	t.Setenv(developmentRunnerWorkflowRefEnv, "main")
	t.Setenv(developmentRunnerWorkflowSHAEnv, strings.Repeat("a", 40))
	t.Setenv(developmentRunnerGenerationEnv, "1")
	config, err = loadDevelopmentRunnerConfig()
	if err != nil || config == nil || config.Generation != 1 || config.CalibrationEffectID != "" {
		t.Fatalf("exact calibration-ready config rejected: %+v %v", config, err)
	}
	for _, value := range []string{"0", "01", "+1", "-1", "18446744073709551615"} {
		t.Setenv(developmentRunnerGenerationEnv, value)
		if _, err := loadDevelopmentRunnerConfig(); err == nil {
			t.Fatalf("accepted generation %q", value)
		}
	}
	t.Setenv(developmentRunnerGenerationEnv, "1")
	t.Setenv(developmentRunnerWorkflowSHAEnv, "main")
	if _, err := loadDevelopmentRunnerConfig(); err == nil {
		t.Fatal("moving workflow head used as immutable pin")
	}
}
