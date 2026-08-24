//go:build windows

package main

import (
	"errors"
	"io"
)

func runOpenCodeRelay(_ []string, _ io.Writer) error {
	return errors.New("OpenCode relay requires a Unix-like Edge host")
}

func runCodexRelay(_ []string, _ io.Writer) error {
	return errors.New("Codex relay requires a native Windows workcell release")
}
