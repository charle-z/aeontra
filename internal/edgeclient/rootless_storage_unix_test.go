//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type storageDriverRunner struct {
	output   []byte
	err      error
	commands []recordedContainerCommand
}

func (r *storageDriverRunner) Run(_ context.Context, executable string, args, env []string) ([]byte, error) {
	r.commands = append(r.commands, recordedContainerCommand{
		executable: executable,
		args:       append([]string(nil), args...),
		env:        append([]string(nil), env...),
	})
	return append([]byte(nil), r.output...), r.err
}

func TestRootlessContainerStorageDriverUsesClosedEngineSpecificInfo(t *testing.T) {
	for _, test := range []struct {
		name     string
		endpoint *RootlessContainerEndpoint
		output   string
		wantArgs []string
		want     string
	}{
		{
			name:     "podman",
			endpoint: &RootlessContainerEndpoint{Engine: "podman", SocketPath: "/run/user/1000/podman/podman.sock", Executable: "/usr/bin/podman"},
			output:   "overlay\n",
			wantArgs: []string{"--url", "unix:///run/user/1000/podman/podman.sock", "info", "--format", "{{.Store.GraphDriverName}}"},
			want:     "overlay",
		},
		{
			name:     "docker",
			endpoint: &RootlessContainerEndpoint{Engine: "docker", SocketPath: "/run/user/1000/docker.sock", Executable: "/usr/bin/docker"},
			output:   "overlay2\n",
			wantArgs: []string{"--host", "unix:///run/user/1000/docker.sock", "info", "--format", "{{.Driver}}"},
			want:     "overlay2",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &storageDriverRunner{output: []byte(test.output)}
			driver, err := rootlessContainerStorageDriver(context.Background(), test.endpoint, "/usr/bin:/bin", runner, func(*RootlessContainerEndpoint, string) ([]string, error) {
				return []string{"LANG=C"}, nil
			})
			if err != nil || driver != test.want {
				t.Fatalf("driver=%q err=%v", driver, err)
			}
			if len(runner.commands) != 1 || runner.commands[0].executable != test.endpoint.Executable || !reflect.DeepEqual(runner.commands[0].args, test.wantArgs) {
				t.Fatalf("commands=%+v", runner.commands)
			}
		})
	}
}

func TestRootlessContainerStorageDriverRejectsUnsafeOrAmbiguousOutput(t *testing.T) {
	endpoint := &RootlessContainerEndpoint{Engine: "podman", SocketPath: "/run/user/1000/podman/podman.sock", Executable: "/usr/bin/podman"}
	for _, test := range []struct {
		name   string
		output string
		err    error
	}{
		{name: "empty"},
		{name: "multiline", output: "overlay\nsecret\n"},
		{name: "punctuation", output: "overlay;rm"},
		{name: "runner error", output: "overlay", err: errors.New("boom")},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &storageDriverRunner{output: []byte(test.output), err: test.err}
			if _, err := rootlessContainerStorageDriver(context.Background(), endpoint, "/usr/bin:/bin", runner, func(*RootlessContainerEndpoint, string) ([]string, error) {
				return nil, nil
			}); err == nil {
				t.Fatal("unsafe storage driver response accepted")
			}
		})
	}
}

func TestRootlessContainerStoragePostureIsConservative(t *testing.T) {
	for driver, want := range map[string]string{
		"overlay":            "copy_on_write",
		"overlay2":           "copy_on_write",
		"vfs":                "degraded",
		"unknown-new-driver": "unknown",
		"":                   "unknown",
	} {
		if got := RootlessContainerStoragePosture(driver); got != want {
			t.Fatalf("driver=%q posture=%q want=%q", driver, got, want)
		}
	}
}
