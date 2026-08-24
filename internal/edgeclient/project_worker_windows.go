//go:build windows

package edgeclient

import "errors"

func RunProjectBrowserLauncher(string, []string) error {
	return errors.New("managed browser launcher requires a Linux Edge")
}
