//go:build !windows

package main

import (
	"errors"
	"io"
)

func runWindowsAgent([]string, io.Reader, io.Writer, io.Writer) error {
	return errors.New("windows-agent requires a native Windows Edge host")
}
