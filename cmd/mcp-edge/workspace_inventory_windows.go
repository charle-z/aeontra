//go:build windows

package main

import (
	"errors"
	"io"
)

func workspaceInventory([]string, io.Writer, io.Writer) error {
	return errors.New("Linux tool inventory requires a Linux Edge")
}
