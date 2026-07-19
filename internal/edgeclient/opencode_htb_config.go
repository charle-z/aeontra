//go:build !windows

package edgeclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/charle-z/mcp-devbox/internal/htbaction"
)

const openCodeSandboxHTBSocket = openCodeSandboxRuntime + "/" + HTBLabBrokerSocketName

func augmentOpenCodeConfigForHTB(configJSON string, workspace Workspace) (string, error) {
	if workspace.Profile != WorkspaceProfileLinuxWorkcell || workspace.Mode != WorkspaceModeHTBLinux {
		return configJSON, nil
	}
	root, options, err := openCodeProviderOptions(configJSON)
	if err != nil {
		return "", err
	}
	for _, key := range []string{"htbSocketPath", "htbWorkspaceID", "htbTools"} {
		if _, exists := options[key]; exists {
			return "", errors.New("OpenCode HTB provider options are duplicated")
		}
	}
	options["htbSocketPath"] = openCodeSandboxHTBSocket
	options["htbWorkspaceID"] = workspace.ID
	options["htbTools"] = htbaction.Definitions()
	encoded, err := json.Marshal(root)
	if err != nil {
		return "", errors.New("OpenCode HTB provider configuration failed")
	}
	return string(encoded), nil
}

func validateOpenCodeHTBConfig(configJSON string, workspace Workspace, lease ModelRuntimeLease) error {
	_, options, err := openCodeProviderOptions(configJSON)
	if err != nil {
		return err
	}
	_, hasSocket := options["htbSocketPath"]
	_, hasWorkspace := options["htbWorkspaceID"]
	_, hasTools := options["htbTools"]
	hasAny := hasSocket || hasWorkspace || hasTools
	if workspace.Mode != WorkspaceModeHTBLinux {
		if hasAny {
			return errors.New("OpenCode HTB provider options appeared outside htb-linux mode")
		}
		return nil
	}
	if !hasSocket || !hasWorkspace || !hasTools || options["htbSocketPath"] != openCodeSandboxHTBSocket || options["htbWorkspaceID"] != workspace.ID || workspace.ID != lease.WorkspaceID {
		return errors.New("OpenCode HTB provider identity is invalid")
	}
	encoded, err := json.Marshal(options["htbTools"])
	if err != nil {
		return errors.New("OpenCode HTB tool definitions are invalid")
	}
	var definitions []htbaction.Definition
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&definitions); err != nil || len(definitions) != len(htbaction.Definitions()) {
		return errors.New("OpenCode HTB tool definitions are invalid")
	}
	wantBytes, err := json.Marshal(htbaction.Definitions())
	if err != nil {
		return errors.New("OpenCode HTB tool definitions changed")
	}
	var want any
	if err := json.Unmarshal(wantBytes, &want); err != nil || !reflect.DeepEqual(options["htbTools"], want) {
		return errors.New("OpenCode HTB tool definitions changed")
	}
	return nil
}

func openCodeProviderOptions(configJSON string) (map[string]any, map[string]any, error) {
	var root map[string]any
	if err := json.Unmarshal([]byte(configJSON), &root); err != nil {
		return nil, nil, errors.New("OpenCode provider configuration is invalid")
	}
	provider, ok := root["provider"].(map[string]any)
	if !ok {
		return nil, nil, errors.New("OpenCode provider configuration is invalid")
	}
	bridge, ok := provider["bridge"].(map[string]any)
	if !ok {
		return nil, nil, errors.New("OpenCode provider configuration is invalid")
	}
	options, ok := bridge["options"].(map[string]any)
	if !ok {
		return nil, nil, errors.New("OpenCode provider configuration is invalid")
	}
	return root, options, nil
}

func openCodeSetEnvValue(args []string, key string) (string, bool) {
	for index := 0; index+2 < len(args); index++ {
		if args[index] == "--" {
			break
		}
		if args[index] == "--setenv" && args[index+1] == key {
			return args[index+2], true
		}
	}
	return "", false
}
