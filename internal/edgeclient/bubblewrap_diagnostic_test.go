//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestClassifyBubblewrapFailure(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		stage  int
		err    error
		stderr string
		code   string
	}{
		{name: "user namespace", stage: bubblewrapStageUserNamespace, err: errors.New("exit"), stderr: "bwrap: No permissions to create new namespace", code: "bubblewrap_user_namespace_denied"},
		{name: "uid map", stage: bubblewrapStageUIDMap, err: errors.New("exit"), stderr: "bwrap: setting up uid map: Permission denied", code: "bubblewrap_uid_map_denied"},
		{name: "gid map", stage: bubblewrapStageGIDMap, err: errors.New("exit"), stderr: "bwrap: setting up gid map: Permission denied", code: "bubblewrap_gid_map_denied"},
		{name: "mount propagation", stage: bubblewrapStageEmptyFilesystem, err: errors.New("exit"), stderr: "bwrap: Failed to make / slave: Permission denied", code: "bubblewrap_mount_propagation_denied"},
		{name: "bind mount", stage: bubblewrapStageWorkspaceBind, err: errors.New("exit"), stderr: "bwrap: bind: Operation not permitted", code: "bubblewrap_bind_mount_denied"},
		{name: "proc mount", stage: bubblewrapStageProcMount, err: errors.New("exit"), stderr: "bwrap: mount proc: Operation not permitted", code: "bubblewrap_proc_mount_denied"},
		{name: "dev mount", stage: bubblewrapStageDevMount, err: errors.New("exit"), stderr: "bwrap: mount dev: Permission denied", code: "bubblewrap_dev_mount_denied"},
		{name: "tmpfs mount", stage: bubblewrapStageTmpfsMount, err: errors.New("exit"), stderr: "bwrap: mount tmpfs: Permission denied", code: "bubblewrap_tmpfs_mount_denied"},
		{name: "exec denied", stage: bubblewrapStageHelperExec, err: errors.New("exit"), stderr: "bwrap: execvp helper: Permission denied", code: "bubblewrap_exec_denied"},
		{name: "path missing", stage: bubblewrapStageHelperExec, err: errors.New("exit"), stderr: "bwrap: execvp helper: No such file or directory", code: "bubblewrap_path_missing"},
		{name: "generic permission", stage: bubblewrapStageEmptyFilesystem, err: errors.New("exit"), stderr: "bwrap: Operation not permitted", code: "bubblewrap_permission_denied"},
		{name: "process exit", stage: bubblewrapStageHelperExec, err: errors.New("exit"), stderr: "bwrap: child failed", code: "bubblewrap_process_exit"},
		{name: "timeout", stage: bubblewrapStageHelperExec, err: context.DeadlineExceeded, code: "bubblewrap_timeout"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			diagnostic := classifyBubblewrapFailure(test.stage, test.err, test.stderr, 5*time.Millisecond)
			if diagnostic.Code != test.code {
				t.Fatalf("code=%q want=%q", diagnostic.Code, test.code)
			}
			if diagnostic.Stage != test.stage || diagnostic.DurationNanos <= 0 {
				t.Fatalf("diagnostic=%+v", diagnostic)
			}
			if test.code == "bubblewrap_timeout" && !diagnostic.TimedOut {
				t.Fatalf("timeout diagnostic=%+v", diagnostic)
			}
		})
	}
}

func TestClassifyBubblewrapSuccess(t *testing.T) {
	diagnostic := classifyBubblewrapFailure(bubblewrapStageVersion, nil, "ignored", time.Millisecond)
	if diagnostic.Code != "none" || diagnostic.ExitCode != 0 || diagnostic.TimedOut {
		t.Fatalf("diagnostic=%+v", diagnostic)
	}
}
