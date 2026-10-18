//go:build windows

package edgeclient

import "errors"

func inspectHTBArtifact(string, string) (HTBArtifactStatus, error) {
	return HTBArtifactStatus{}, errors.New("HTB artifact metadata requires Linux")
}
