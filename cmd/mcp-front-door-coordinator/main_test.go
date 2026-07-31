package main

import (
	"testing"

	"github.com/charle-z/mcp-devbox/internal/frontdoorcoordinator"
)

const testCoordinatorRequestID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func envReader(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadConfigRequiresRequestIDForTransition(t *testing.T) {
	_, err := loadConfig(envReader(map[string]string{targetEnv: string(frontdoorcoordinator.TargetCutover)}))
	if err == nil {
		t.Fatal("cutover without request id was accepted")
	}
}

func TestLoadConfigAcceptsReviewedRequestID(t *testing.T) {
	config, err := loadConfig(envReader(map[string]string{
		targetEnv:    string(frontdoorcoordinator.TargetRollback),
		requestIDEnv: testCoordinatorRequestID,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if config.Target != frontdoorcoordinator.TargetRollback || config.RequestID != testCoordinatorRequestID {
		t.Fatalf("config=%+v", config)
	}
}

func TestLoadConfigAcceptsIdleWithoutRequestID(t *testing.T) {
	config, err := loadConfig(envReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	if config.Target != frontdoorcoordinator.TargetIdle || config.RequestID != "" {
		t.Fatalf("config=%+v", config)
	}
}
