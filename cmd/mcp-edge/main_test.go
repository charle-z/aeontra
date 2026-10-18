package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpAndPairingCodeInputDoNotAcceptSecretFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"help"}, strings.NewReader(""), &stdout, &stderr); code != 0 || !strings.Contains(strings.ToLower(stdout.String()), "pairing code is read from stdin") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"pair", "--code", "ep_secret"}, strings.NewReader(""), &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestReadPairingCodeIsBoundedAndRequired(t *testing.T) {
	if value, err := readPairingCode(strings.NewReader("ep_opaque-value\n")); err != nil || value != "ep_opaque-value" {
		t.Fatalf("value=%q err=%v", value, err)
	}
	if _, err := readPairingCode(strings.NewReader(strings.Repeat("x", 300))); err == nil {
		t.Fatal("oversized invalid pairing input accepted")
	}
}
