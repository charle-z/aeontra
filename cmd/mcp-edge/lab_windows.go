//go:build windows

package main

import (
	"errors"
	"io"
)

func labCommand([]string, io.Writer, io.Writer) error {
	return errors.New("HTB lab management requires a Linux Edge")
}
