//go:build !windows

package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func TestStep4DoctorReportsJournalReconciliationAsDegraded(t *testing.T) {
	restoreDoctorHooks(t)
	t.Setenv("HOME", t.TempDir())
	stubHealthyDoctor(t)
	doctorInspectJournal = func(string) (edgeclient.JournalHealth, error) {
		return edgeclient.JournalHealth{State: edgeclient.JournalHealthReconciliation, Entries: 1, Started: 1}, nil
	}
	var stdout bytes.Buffer
	err := doctorCommand(nil, &stdout, &bytes.Buffer{})
	if err == nil || err.Error() != "edge doctor found a degraded installation" {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(stdout.String(), "status=degraded") || !strings.Contains(stdout.String(), "journal=reconciliation") || strings.Contains(stdout.String(), "et_") {
		t.Fatalf("output=%q", stdout.String())
	}
}

func TestStep4DoctorBlocksUnsafeJournalWithoutRepairingIt(t *testing.T) {
	restoreDoctorHooks(t)
	t.Setenv("HOME", t.TempDir())
	stubHealthyDoctor(t)
	doctorInspectJournal = func(string) (edgeclient.JournalHealth, error) {
		return edgeclient.JournalHealth{}, errors.New("unsafe")
	}
	var stdout bytes.Buffer
	err := doctorCommand(nil, &stdout, &bytes.Buffer{})
	if err == nil || err.Error() != "edge doctor found an unsafe journal" {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(stdout.String(), "status=blocked") || !strings.Contains(stdout.String(), "journal=blocked") {
		t.Fatalf("output=%q", stdout.String())
	}
}
