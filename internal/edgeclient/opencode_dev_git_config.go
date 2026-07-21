//go:build !windows

package edgeclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/charle-z/mcp-devbox/internal/devaction"
)

const openCodeSandboxDevGitSocket = openCodeSandboxRuntime + "/" + DevGitBrokerSocketName

func augmentOpenCodeConfigForDevGit(configJSON string, workspace Workspace) (string, error) {
	if workspace.Profile != WorkspaceProfileLinuxWorkcell || workspace.Mode != WorkspaceModeDev {
		return configJSON, nil
	}
	root, options, err := openCodeProviderOptions(configJSON)
	if err != nil {
		return "", err
	}
	for _, key := range []string{"devGitSocketPath", "devGitWorkspaceID", "devGitTools"} {
		if _, exists := options[key]; exists {
			return "", errors.New("OpenCode development Git provider options are duplicated")
		}
	}
	options["devGitSocketPath"] = openCodeSandboxDevGitSocket
	options["devGitWorkspaceID"] = workspace.ID
	options["devGitTools"] = devaction.Definitions()
	encoded, err := json.Marshal(root)
	if err != nil {
		return "", errors.New("OpenCode development Git provider configuration failed")
	}
	return string(encoded), nil
}

func validateOpenCodeDevGitConfig(configJSON string, workspace Workspace, lease ModelRuntimeLease, configured bool) error {
	_, options, err := openCodeProviderOptions(configJSON)
	if err != nil {
		return err
	}
	_, hasSocket := options["devGitSocketPath"]
	_, hasWorkspace := options["devGitWorkspaceID"]
	_, hasTools := options["devGitTools"]
	hasAny := hasSocket || hasWorkspace || hasTools
	if workspace.Mode != WorkspaceModeDev || !configured {
		if hasAny {
			return errors.New("OpenCode development Git options appeared without local authority")
		}
		return nil
	}
	if !hasSocket || !hasWorkspace || !hasTools || options["devGitSocketPath"] != openCodeSandboxDevGitSocket ||
		options["devGitWorkspaceID"] != workspace.ID || workspace.ID != lease.WorkspaceID {
		return errors.New("OpenCode development Git provider identity is invalid")
	}
	encoded, err := json.Marshal(options["devGitTools"])
	if err != nil {
		return errors.New("OpenCode development Git tool definitions are invalid")
	}
	var definitions []devaction.Definition
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&definitions); err != nil || len(definitions) != len(devaction.Definitions()) {
		return errors.New("OpenCode development Git tool definitions are invalid")
	}
	wantBytes, err := json.Marshal(devaction.Definitions())
	if err != nil {
		return errors.New("OpenCode development Git tool definitions changed")
	}
	var want any
	if err := json.Unmarshal(wantBytes, &want); err != nil || !reflect.DeepEqual(options["devGitTools"], want) {
		return errors.New("OpenCode development Git tool definitions changed")
	}
	return nil
}
