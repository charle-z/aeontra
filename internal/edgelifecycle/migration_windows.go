//go:build windows

package edgelifecycle

import "errors"

func currentEUID() int { return -1 }

func currentEGID() int { return -1 }

func requireOwnedPath(string, int, int) error {
	return errors.New("Edge state migration is supported only on Linux")
}

func renameNoReplace(string, string) error {
	return errors.New("Edge state migration is supported only on Linux")
}
