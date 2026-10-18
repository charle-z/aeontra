package main

import "testing"

func TestLoadConfigRejectsAlternateStateRoot(t *testing.T) {
	_, err := loadConfig(validCoordinatorEnvironment(map[string]string{stateRootEnv: "/tmp/coordinator-state"}))
	if err == nil {
		t.Fatal("alternate coordinator state root was accepted")
	}
}

func TestLoadConfigRejectsMalformedListenAddress(t *testing.T) {
	_, err := loadConfig(validCoordinatorEnvironment(map[string]string{listenAddrEnv: "localhost"}))
	if err == nil {
		t.Fatal("malformed coordinator listen address was accepted")
	}
}
