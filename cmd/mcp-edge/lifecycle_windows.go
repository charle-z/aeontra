//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
)

// Windows lifecycle operations keep inventory/status in-process and delegate
// privileged changes to the signed updater binary. No shell, caller-controlled
// executable, or mutable release path is accepted here.
func lifecycleCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("lifecycle requires a closed state operation")
	}
	switch args[0] {
	case "inspect", "status":
		if len(args) != 1 {
			return errors.New("lifecycle inspect/status accepts no arguments")
		}
		snapshot, err := inspectWindowsDoctor()
		if err != nil {
			return errors.New("Windows Edge lifecycle inspection failed")
		}
		fmt.Fprintf(stdout, "edge_lifecycle status=ready operation=%s bundle=valid identity=valid service=%s release=%s commit=%s\n",
			args[0], windowsDoctorServiceState(snapshot.ServiceStatus.State), snapshot.BundleRelease, snapshot.BundleCommit)
		return nil
	case "update":
		if len(args) != 2 || args[1] != "stable" {
			return errors.New("lifecycle update accepts only stable")
		}
		return runWindowsDoctorUpdater([]string{"update", "stable"}, stdout, stderr)
	case "rollback":
		if len(args) != 1 {
			return errors.New("lifecycle rollback accepts no arguments")
		}
		return runWindowsDoctorUpdater([]string{"rollback"}, stdout, stderr)
	default:
		return errors.New("unknown lifecycle command")
	}
}
