//go:build !windows

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

type edgeServiceObservation struct {
	State   string
	Active  bool
	MainPID int
}

type edgeRuntimeObservation struct {
	ServiceState   string
	ServiceActive  bool
	ServicePID     int
	ProcessState   string
	LockState      string
	Coherence      string
	ProcessRelease string
	ProcessCommit  string
	Healthy        bool
	Blockers       []string
}

var edgeServiceUserPattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
var inspectEdgeSystemdService = systemdEdgeServiceObservation

func inspectEdgeRuntime(stateRoot, service string) edgeRuntimeObservation {
	observation := edgeRuntimeObservation{
		ServiceState: "inactive",
		ProcessState: "inactive",
		LockState:    "missing",
		Coherence:    "stopped",
	}
	serviceObservation := inspectEdgeSystemdService(service)
	observation.ServiceState = serviceObservation.State
	observation.ServiceActive = serviceObservation.Active
	observation.ServicePID = serviceObservation.MainPID

	lockReport, err := edgeclient.InspectEdgeInstanceLock(stateRoot)
	if err != nil {
		observation.LockState = "blocked"
		observation.ProcessState = "incoherent"
		observation.Coherence = "incoherent"
		observation.Blockers = append(observation.Blockers, "edge_lock_unavailable")
		return observation
	}
	observation.LockState = lockReport.State
	if lockReport.MetadataValid && lockReport.ProcessActive {
		observation.ProcessRelease = lockReport.Metadata.Release
		observation.ProcessCommit = lockReport.Metadata.Commit
	}

	if !observation.ServiceActive && lockReport.State == "held" && lockReport.ProcessActive && lockReport.Metadata.InvocationID != "" {
		observation.ServiceState = "active"
		observation.ServiceActive = true
		observation.ServicePID = lockReport.Metadata.PID
	}

	switch lockReport.State {
	case "held":
		if !lockReport.ProcessActive {
			observation.ProcessState = "incoherent"
			observation.Coherence = "incoherent"
			observation.Blockers = append(observation.Blockers, "edge_lock_owner_inactive")
			return observation
		}
		observation.ProcessState = "single"
		if observation.ServiceActive {
			if observation.ServicePID != 0 && observation.ServicePID != lockReport.Metadata.PID {
				observation.ProcessState = "duplicate"
				observation.Coherence = "duplicate"
				observation.Blockers = append(observation.Blockers, "edge_process_duplicate")
				return observation
			}
			observation.Coherence = "managed"
		} else {
			if observation.ServiceState != "inactive" {
				observation.ProcessState = "incoherent"
				observation.Coherence = "incoherent"
				observation.Blockers = append(observation.Blockers, "edge_service_process_incoherent")
				return observation
			}
			observation.Coherence = "manual"
		}
		observation.Healthy = true
		return observation
	case "missing", "stale_recoverable":
		if observation.ServiceActive {
			observation.ProcessState = "incoherent"
			observation.Coherence = "incoherent"
			observation.Blockers = append(observation.Blockers, "edge_service_without_lock")
			return observation
		}
		observation.ProcessState = "inactive"
		observation.Coherence = "stopped"
		observation.Blockers = append(observation.Blockers, "edge_process_inactive", "edge_service_inactive")
		return observation
	case "held_unverified", "held_incoherent", "unlocked_invalid", "unlocked_live_owner":
		observation.ProcessState = "incoherent"
		observation.Coherence = "incoherent"
		observation.Blockers = append(observation.Blockers, "edge_lock_incoherent")
		return observation
	default:
		observation.ProcessState = "incoherent"
		observation.Coherence = "incoherent"
		observation.Blockers = append(observation.Blockers, "edge_lock_incoherent")
		return observation
	}
}

func installedEdgeServiceName() string {
	if content, err := os.ReadFile("/etc/mcp-devbox/edge-user"); err == nil {
		username := strings.TrimSpace(string(content))
		if edgeServiceUserPattern.MatchString(username) {
			return "mcp-devbox-opencode-edge@" + username + ".service"
		}
	}
	current, err := user.Current()
	if err != nil {
		return ""
	}
	username := strings.TrimSpace(current.Username)
	if !edgeServiceUserPattern.MatchString(username) {
		return ""
	}
	return "mcp-devbox-opencode-edge@" + username + ".service"
}

func systemdEdgeServiceObservation(service string) edgeServiceObservation {
	if strings.TrimSpace(service) == "" {
		return edgeServiceObservation{State: "inactive"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/bin/systemctl", "show", service, "--property=ActiveState", "--property=MainPID")
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	command.Stdin = nil
	command.Stderr = io.Discard
	output, err := command.Output()
	if err != nil {
		return edgeServiceObservation{State: "inactive"}
	}
	values := map[string]string{}
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		key, value, found := bytes.Cut(line, []byte{'='})
		if found {
			values[string(key)] = strings.TrimSpace(string(value))
		}
	}
	state := values["ActiveState"]
	if state == "" {
		state = "inactive"
	}
	pid, _ := strconv.Atoi(values["MainPID"])
	if pid < 0 {
		pid = 0
	}
	return edgeServiceObservation{State: state, Active: state == "active", MainPID: pid}
}

func appendUniqueBlockers(target []string, blockers ...string) []string {
	for _, blocker := range blockers {
		if blocker == "" {
			continue
		}
		found := false
		for _, existing := range target {
			if existing == blocker {
				found = true
				break
			}
		}
		if !found {
			target = append(target, blocker)
		}
	}
	return target
}
