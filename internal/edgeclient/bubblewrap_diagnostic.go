//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

const (
	bubblewrapStageVersion = iota + 1
	bubblewrapStageUserNamespace
	bubblewrapStageUIDMap
	bubblewrapStageGIDMap
	bubblewrapStageEmptyFilesystem
	bubblewrapStageProcMount
	bubblewrapStageDevMount
	bubblewrapStageTmpfsMount
	bubblewrapStageSystemBind
	bubblewrapStageWorkspaceBind
	bubblewrapStageRuntimeBind
	bubblewrapStageProviderBind
	bubblewrapStageUnixSocket
	bubblewrapStageUnshareAll
	bubblewrapStageHelperExec
	bubblewrapStageMax = bubblewrapStageHelperExec
)

type bubblewrapDiagnostic struct {
	Code          string
	Stage         int
	ExitCode      int
	TimedOut      bool
	DurationNanos int64
}

func classifyBubblewrapFailure(stage int, err error, stderr string, duration time.Duration) bubblewrapDiagnostic {
	if stage < bubblewrapStageVersion || stage > bubblewrapStageMax {
		stage = 0
	}
	diagnostic := bubblewrapDiagnostic{
		Code:          "bubblewrap_process_exit",
		Stage:         stage,
		ExitCode:      -1,
		DurationNanos: duration.Nanoseconds(),
	}
	if err == nil {
		diagnostic.Code = "none"
		diagnostic.ExitCode = 0
		return diagnostic
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		diagnostic.Code = "bubblewrap_timeout"
		diagnostic.TimedOut = true
		return diagnostic
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		diagnostic.ExitCode = exitErr.ExitCode()
	}

	lowered := strings.ToLower(stderr)
	permissionDenied := strings.Contains(lowered, "permission denied") ||
		strings.Contains(lowered, "operation not permitted") ||
		strings.Contains(lowered, "eperm") || strings.Contains(lowered, "eacces")

	switch {
	case strings.Contains(lowered, "netlink_route") &&
		(strings.Contains(lowered, "address family not supported") ||
			strings.Contains(lowered, "failed to create")):
		diagnostic.Code = "bubblewrap_netlink_route_denied"
	case strings.Contains(lowered, "no permissions to create new namespace"),
		strings.Contains(lowered, "creating new namespace") && permissionDenied,
		strings.Contains(lowered, "unshare") && strings.Contains(lowered, "user") && permissionDenied:
		diagnostic.Code = "bubblewrap_user_namespace_denied"
	case strings.Contains(lowered, "setting up uid map"), strings.Contains(lowered, "uid map"), strings.Contains(lowered, "newuidmap"):
		diagnostic.Code = "bubblewrap_uid_map_denied"
	case strings.Contains(lowered, "setting up gid map"), strings.Contains(lowered, "gid map"), strings.Contains(lowered, "newgidmap"):
		diagnostic.Code = "bubblewrap_gid_map_denied"
	case strings.Contains(lowered, "failed to make / slave"), strings.Contains(lowered, "mount propagation"):
		diagnostic.Code = "bubblewrap_mount_propagation_denied"
	case strings.Contains(lowered, "no such file"), strings.Contains(lowered, "not found"):
		diagnostic.Code = "bubblewrap_path_missing"
	case strings.Contains(lowered, "tmpfs") && permissionDenied:
		diagnostic.Code = "bubblewrap_tmpfs_mount_denied"
	case strings.Contains(lowered, "proc") && strings.Contains(lowered, "mount") && permissionDenied:
		diagnostic.Code = "bubblewrap_proc_mount_denied"
	case strings.Contains(lowered, "dev") && strings.Contains(lowered, "mount") && permissionDenied:
		diagnostic.Code = "bubblewrap_dev_mount_denied"
	case strings.Contains(lowered, "bind") && permissionDenied:
		diagnostic.Code = "bubblewrap_bind_mount_denied"
	case strings.Contains(lowered, "execvp") && permissionDenied:
		diagnostic.Code = "bubblewrap_exec_denied"
	case permissionDenied:
		diagnostic.Code = bubblewrapPermissionCodeForStage(stage)
	default:
		diagnostic.Code = "bubblewrap_process_exit"
	}
	return diagnostic
}

func bubblewrapPermissionCodeForStage(stage int) string {
	switch stage {
	case bubblewrapStageUserNamespace, bubblewrapStageUnshareAll:
		return "bubblewrap_user_namespace_denied"
	case bubblewrapStageUIDMap:
		return "bubblewrap_uid_map_denied"
	case bubblewrapStageGIDMap:
		return "bubblewrap_gid_map_denied"
	case bubblewrapStageProcMount:
		return "bubblewrap_proc_mount_denied"
	case bubblewrapStageDevMount:
		return "bubblewrap_dev_mount_denied"
	case bubblewrapStageTmpfsMount:
		return "bubblewrap_tmpfs_mount_denied"
	case bubblewrapStageSystemBind, bubblewrapStageWorkspaceBind, bubblewrapStageRuntimeBind, bubblewrapStageProviderBind, bubblewrapStageUnixSocket:
		return "bubblewrap_bind_mount_denied"
	case bubblewrapStageHelperExec:
		return "bubblewrap_exec_denied"
	default:
		return "bubblewrap_permission_denied"
	}
}
