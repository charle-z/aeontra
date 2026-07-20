//go:build windows

package main

import (
	"errors"
	"io"
)

func onboard([]string, io.Reader, io.Writer, io.Writer) error {
	return errors.New("onboarding is supported only on the packaged Linux Edge")
}
