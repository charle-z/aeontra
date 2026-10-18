package app

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/observability"
)

func parseObservabilityConfig(modeFlag, pathFlag, maxBytesFlag string) (observability.Config, error) {
	cfg := observability.DefaultConfig()
	if value := strings.TrimSpace(envFallback(modeFlag, observabilityModeEnv)); value != "" {
		cfg.Mode = observability.Mode(strings.ToLower(value))
	}
	if value := strings.TrimSpace(envFallback(pathFlag, observabilityPathEnv)); value != "" {
		cfg.Path = value
	}
	if value := strings.TrimSpace(envFallback(maxBytesFlag, observabilityMaxBytesEnv)); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return observability.Config{}, fmt.Errorf("%s must be an integer byte count", observabilityMaxBytesEnv)
		}
		cfg.MaxBytes = parsed
	}
	return observability.ValidateConfig(cfg)
}

func resolveObservabilityConfig(cfg observability.Config, stateRoot string) (observability.Config, error) {
	validated, err := observability.ValidateConfig(cfg)
	if err != nil {
		return observability.Config{}, err
	}
	if (validated.Mode == observability.ModeFile || validated.Mode == observability.ModeBoth) && validated.Path == "" {
		validated.Path = filepath.Join(stateRoot, "logs", "observability.jsonl")
	}
	return observability.ValidateConfig(validated)
}
