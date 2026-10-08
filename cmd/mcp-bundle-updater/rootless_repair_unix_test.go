//go:build !windows

package main

import (
	"errors"
	"reflect"
	"testing"
)

func TestRootlessPodmanRepairIsFixedToEdgeUserSocket(t *testing.T) {
	original := systemctlCommand
	t.Cleanup(func() { systemctlCommand = original })
	var calls [][]string
	systemctlCommand = func(args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return nil, nil
	}
	service := &systemdService{name: "mcp-devbox-opencode-edge@mcpedge.service", user: "mcpedge"}
	if err := service.ReconcileRootlessPodmanSocket(); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"--user", "--machine=mcpedge@", "enable", "--now", "podman.socket"},
		{"--user", "--machine=mcpedge@", "restart", "podman.service"},
		{"--user", "--machine=mcpedge@", "restart", "podman.socket"},
		{"--user", "--machine=mcpedge@", "is-active", "--quiet", "podman.service"},
		{"--user", "--machine=mcpedge@", "is-active", "--quiet", "podman.socket"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls=%v want=%v", calls, want)
	}
}

func TestRootlessPodmanRepairFailsClosed(t *testing.T) {
	original := systemctlCommand
	t.Cleanup(func() { systemctlCommand = original })
	service := &systemdService{name: "mcp-devbox-opencode-edge@mcpedge.service", user: "mcpedge"}
	systemctlCommand = func(args ...string) ([]byte, error) { return nil, errors.New("failed") }
	if err := service.ReconcileRootlessPodmanSocket(); err == nil {
		t.Fatal("systemd failure accepted")
	}
	service.user = "bad/user"
	if err := service.ReconcileRootlessPodmanSocket(); err == nil {
		t.Fatal("unsafe edge user accepted")
	}
}
