//go:build windows

package main

import (
	"errors"
	"io"
)

func lifecycleCommand([]string, io.Writer, io.Writer) error {
	return errors.New("Edge lifecycle is supported only on the packaged Linux Edge")
}
