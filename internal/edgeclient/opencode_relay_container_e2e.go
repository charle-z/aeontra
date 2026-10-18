//go:build opencode_e2e && !windows

package edgeclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

// EnableRelayContainerE2E replaces only the process runner in binaries built
// with the opencode_e2e tag. It validates the distributed relay and real
// OpenCode/provider behavior without claiming Bubblewrap isolation. Production
// binaries never compile this function or this execution path.
func EnableRelayContainerE2E(launcher *OpenCodeLauncher) error {
	if launcher == nil {
		return errors.New("relay container E2E launcher is unavailable")
	}
	launcher.verifySandbox = verifyRelayContainerOpenCode
	launcher.runProcess = runRelayContainerOpenCode
	return nil
}

func verifyRelayContainerOpenCode(ctx context.Context, spec openCodeProcessSpec) error {
	translated, err := translateRelayContainerSpec(spec)
	if err != nil {
		return err
	}
	translated.Args = []string{"--version"}
	output := newBoundedCapture(4096)
	translated.Stdout = output
	translated.Stderr = io.Discard
	result := runOpenCodeProcess(ctx, translated)
	if result.Err != nil || result.ExitCode != 0 || output.Truncated() || strings.TrimSpace(output.String()) != PinnedOpenCodeVersion {
		return errors.New("relay container OpenCode verification failed")
	}
	return nil
}

func runRelayContainerOpenCode(ctx context.Context, spec openCodeProcessSpec) openCodeProcessResult {
	translated, err := translateRelayContainerSpec(spec)
	if err != nil {
		return openCodeProcessResult{ExitCode: -1, Err: err}
	}
	return runOpenCodeProcess(ctx, translated)
}

func translateRelayContainerSpec(spec openCodeProcessSpec) (openCodeProcessSpec, error) {
	if len(spec.Sandbox.Command) == 0 || !spec.Sandbox.ClearEnv {
		return openCodeProcessSpec{}, errors.New("relay container sandbox specification is invalid")
	}
	mounts := append([]openCodeSandboxMount(nil), spec.Sandbox.Mounts...)
	sort.Slice(mounts, func(left, right int) bool { return len(mounts[left].Target) > len(mounts[right].Target) })
	translate := func(value string) string {
		for _, mount := range mounts {
			if value == mount.Target {
				return mount.Source
			}
			prefix := mount.Target + string(filepath.Separator)
			if strings.HasPrefix(value, prefix) {
				return filepath.Join(mount.Source, strings.TrimPrefix(value, prefix))
			}
		}
		return value
	}
	command := make([]string, len(spec.Sandbox.Command))
	for index, value := range spec.Sandbox.Command {
		command[index] = translate(value)
	}
	environment := make([]string, 0, len(spec.Sandbox.Environment))
	for key, value := range spec.Sandbox.Environment {
		translatedValue := translate(value)
		if key == "OPENCODE_CONFIG_CONTENT" {
			var configErr error
			translatedValue, configErr = relayContainerConfig(translatedValue, translate)
			if configErr != nil {
				return openCodeProcessSpec{}, configErr
			}
		}
		environment = append(environment, key+"="+translatedValue)
	}
	sort.Strings(environment)
	return openCodeProcessSpec{
		Executable: command[0],
		Args:       command[1:],
		Dir:        translate(spec.Sandbox.WorkingDirectory),
		Env:        environment,
		Stdout:     spec.Stdout,
		Stderr:     spec.Stderr,
	}, nil
}

func relayContainerConfig(value string, translate func(string) string) (string, error) {
	var config map[string]any
	if err := json.Unmarshal([]byte(value), &config); err != nil {
		return "", errors.New("relay container OpenCode configuration is invalid")
	}
	provider, ok := config["provider"].(map[string]any)
	if !ok {
		return "", errors.New("relay container OpenCode provider configuration is invalid")
	}
	bridge, ok := provider["bridge"].(map[string]any)
	if !ok {
		return "", errors.New("relay container OpenCode bridge configuration is invalid")
	}
	npm, ok := bridge["npm"].(string)
	if !ok || !strings.HasPrefix(npm, "file://") {
		return "", errors.New("relay container OpenCode provider location is invalid")
	}
	bridge["npm"] = "file://" + translate(strings.TrimPrefix(npm, "file://"))
	options, ok := bridge["options"].(map[string]any)
	if !ok {
		return "", errors.New("relay container OpenCode provider options are invalid")
	}
	socketPath, ok := options["socketPath"].(string)
	if !ok {
		return "", errors.New("relay container OpenCode socket location is invalid")
	}
	options["socketPath"] = translate(socketPath)
	permission, ok := config["permission"].(map[string]any)
	if !ok {
		return "", errors.New("relay container OpenCode permissions are invalid")
	}
	// The host test keeps external_directory denied because the provider is
	// mounted inside Bubblewrap. The container-only adapter maps that mount to
	// a host path, while Docker itself remains read-only and network-none.
	permission["external_directory"] = "allow"
	encoded, err := json.Marshal(config)
	if err != nil {
		return "", errors.New("relay container OpenCode configuration failed")
	}
	return string(encoded), nil
}
