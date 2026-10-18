//go:build !windows

package main

import "testing"

func TestUpdaterAcceptsOnlyClosedOperations(t *testing.T) {
	for _, args := range [][]string{{"status"}, {"update", "stable"}, {"rollback"}, {"repair"}} {
		if _, err := parseUpdaterOperation(args); err != nil {
			t.Fatalf("closed operation %v rejected: %v", args, err)
		}
	}
	for _, args := range [][]string{
		{"update", "https://attacker.invalid/bundle"},
		{"update", "/tmp/bundle"},
		{"update", "stable", "--hash", "sha256:any"},
		{"sh", "-c", "id"},
		{"install", "script.sh"},
	} {
		if _, err := parseUpdaterOperation(args); err == nil {
			t.Fatalf("open-ended updater operation accepted: %v", args)
		}
	}
}
