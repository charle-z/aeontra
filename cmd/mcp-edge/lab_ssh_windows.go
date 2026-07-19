//go:build windows

package main

import (
	"errors"
	"io"
)

func labSSHExec([]string, io.Writer, io.Writer) error {
	return errors.New("lab SSH execution requires Linux")
}

func runAskpassIfRequested(io.Writer) (bool, error) {
	return false, nil
}
