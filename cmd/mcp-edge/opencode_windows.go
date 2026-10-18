//go:build windows

package main

import (
	"errors"
	"io"
)

func runOpenCodeRelay(_ []string, _ io.Writer) error {
	return errors.New("OpenCode relay requires a Unix-like Edge host")
}
