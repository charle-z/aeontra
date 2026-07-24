//go:build windows

package main

import (
	"errors"
	"io"
)

func doctorCommand([]string, io.Writer, io.Writer) error {
	return errors.New("Edge doctor is supported only on the packaged Linux Edge")
}
