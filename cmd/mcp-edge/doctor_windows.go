//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/bundle"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	windowsDoctorServiceName     = "AeontraEdge"
	windowsDoctorServiceIdentity = `NT SERVICE\AeontraEdge`
	windowsDoctorConfigLimit     = 16 << 10
)

type windowsDoctorServiceConfig struct {
	Version         int    `json:"version"`
	Service         string `json:"service"`
	ServiceIdentity string `json:"service_identity"`
	StateRoot       string `json:"state_root"`
	WorkspaceRoot   string `json:"workspace_root"`
}

type windowsDoctorActiveMarker struct {
	Version int    `json:"version"`
	Release string `json:"release"`
	Commit  string `json:"commit"`
	Path    string `json:"path"`
}

type windowsDoctorSnapshot struct {
	BundleRelease string
	BundleCommit  string
	Identity      edgeclient.Identity
	ServiceState  svc.State
}

var windowsDoctorLoadConfig = loadWindowsDoctorServiceConfig
var windowsDoctorInspectService = inspectWindowsDoctorService

func doctorCommand(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repair := fs.Bool("repair", false, "delegate repair to the signed updater")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("doctor accepts only --repair")
	}
	snapshot, err := inspectWindowsDoctor()
	if err != nil {
		fmt.Fprintln(stdout, "edge_doctor status=blocked bundle=invalid layout=blocked identity=unknown service=unknown")
		if *repair {
			return errors.New("Windows doctor is read-only; use the signed mcp-bundle-updater.exe")
		}
		return errors.New("Windows Edge doctor found a degraded installation")
	}
	if *repair {
		fmt.Fprintln(stdout, "edge_doctor repair=delegated updater=mcp-bundle-updater.exe mutation=none")
		return nil
	}
	fmt.Fprintf(stdout, "edge_doctor status=ready bundle=valid layout=valid identity=valid service=%s release=%s commit=%s\n",
		windowsDoctorServiceState(snapshot.ServiceState), snapshot.BundleRelease, snapshot.BundleCommit)
	return nil
}

func inspectWindowsDoctor() (windowsDoctorSnapshot, error) {
	installRoot, err := windowsDoctorInstallRoot()
	if err != nil {
		return windowsDoctorSnapshot{}, err
	}
	stateRoot, err := windowsDoctorManagedStateRoot()
	if err != nil {
		return windowsDoctorSnapshot{}, err
	}
	config, err := windowsDoctorLoadConfig(filepath.Join(installRoot, "service-config.json"), stateRoot)
	if err != nil {
		return windowsDoctorSnapshot{}, err
	}
	if err := validateWindowsDoctorRoots(config, installRoot); err != nil {
		return windowsDoctorSnapshot{}, err
	}
	marker, err := loadWindowsDoctorActiveMarker(filepath.Join(installRoot, "active.json"), installRoot)
	if err != nil {
		return windowsDoctorSnapshot{}, err
	}
	expectedReleasePath := filepath.Join(installRoot, "releases", marker.Release)
	if !strings.EqualFold(marker.Path, expectedReleasePath) {
		return windowsDoctorSnapshot{}, errors.New("active Windows Edge release path is not canonical")
	}
	verified, err := verifyInstalledEdgeBundleAt(marker.Path)
	if err != nil || verified.Release != marker.Release || verified.Commit != marker.Commit {
		return windowsDoctorSnapshot{}, errors.New("active Windows Edge bundle is invalid")
	}
	identity, _, err := edgeclient.LoadWindowsServiceIdentity(config.StateRoot, windowsDoctorServiceIdentity)
	if err != nil {
		return windowsDoctorSnapshot{}, errors.New("Windows Edge identity is unavailable")
	}
	state, err := windowsDoctorInspectService(marker.Path, config)
	if err != nil {
		return windowsDoctorSnapshot{}, err
	}
	return windowsDoctorSnapshot{BundleRelease: marker.Release, BundleCommit: marker.Commit, Identity: identity, ServiceState: state}, nil
}

func windowsDoctorInstallRoot() (string, error) {
	root := filepath.Dir(filepath.Dir(filepath.Clean(installedBundleRoot)))
	if root == "" || !filepath.IsAbs(root) || filepath.VolumeName(root) == "" || windowsDoctorIsReparse(root) {
		return "", errors.New("Windows Edge install root is invalid")
	}
	return filepath.Clean(root), nil
}

func windowsDoctorManagedStateRoot() (string, error) {
	programData, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return "", errors.New("managed Windows state root unavailable")
	}
	return filepath.Clean(filepath.Join(programData, "Aeontra", "Edge")), nil
}

func loadWindowsDoctorServiceConfig(filename, expectedStateRoot string) (windowsDoctorServiceConfig, error) {
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > windowsDoctorConfigLimit || windowsDoctorIsReparse(filename) {
		return windowsDoctorServiceConfig{}, errors.New("Windows service configuration is unavailable")
	}
	content, err := os.ReadFile(filename)
	if err != nil {
		return windowsDoctorServiceConfig{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var config windowsDoctorServiceConfig
	if err := decoder.Decode(&config); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return windowsDoctorServiceConfig{}, errors.New("Windows service configuration is invalid")
	}
	if config.Version != 1 || config.Service != windowsDoctorServiceName || config.ServiceIdentity != windowsDoctorServiceIdentity {
		return windowsDoctorServiceConfig{}, errors.New("Windows service configuration identity is invalid")
	}
	state, err := cleanWindowsDoctorPath(config.StateRoot)
	if err != nil || !strings.EqualFold(state, filepath.Clean(expectedStateRoot)) {
		return windowsDoctorServiceConfig{}, errors.New("Windows service state root is not managed")
	}
	workspace, err := cleanWindowsDoctorPath(config.WorkspaceRoot)
	if err != nil || pathsOverlap(state, workspace) {
		return windowsDoctorServiceConfig{}, errors.New("Windows service workspace root is invalid")
	}
	config.StateRoot, config.WorkspaceRoot = state, workspace
	return config, nil
}

func loadWindowsDoctorActiveMarker(filename, installRoot string) (windowsDoctorActiveMarker, error) {
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > windowsDoctorConfigLimit || windowsDoctorIsReparse(filename) {
		return windowsDoctorActiveMarker{}, errors.New("Windows active release marker is unavailable")
	}
	content, err := os.ReadFile(filename)
	if err != nil {
		return windowsDoctorActiveMarker{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var marker windowsDoctorActiveMarker
	if err := decoder.Decode(&marker); err != nil || decoder.Decode(&struct{}{}) != io.EOF || marker.Version != 1 || !bundle.ValidRelease(marker.Release) || !commitValid(marker.Commit) {
		return windowsDoctorActiveMarker{}, errors.New("Windows active release marker is invalid")
	}
	path, err := cleanWindowsDoctorPath(marker.Path)
	if err != nil || !pathInsideWindows(installRoot, path) || windowsDoctorIsReparse(path) {
		return windowsDoctorActiveMarker{}, errors.New("Windows active release path is invalid")
	}
	marker.Path = path
	return marker, nil
}

func validateWindowsDoctorRoots(config windowsDoctorServiceConfig, installRoot string) error {
	if pathsOverlap(installRoot, config.StateRoot) || pathsOverlap(installRoot, config.WorkspaceRoot) {
		return errors.New("Windows Edge roots overlap")
	}
	for _, root := range []string{config.StateRoot, config.WorkspaceRoot} {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() || windowsDoctorIsReparse(root) {
			return errors.New("Windows Edge roots are unavailable or unsafe")
		}
	}
	return nil
}

func inspectWindowsDoctorService(activePath string, config windowsDoctorServiceConfig) (svc.State, error) {
	manager, err := mgr.Connect()
	if err != nil {
		return svc.Stopped, errors.New("Windows Service Control Manager is unavailable")
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(windowsDoctorServiceName)
	if err != nil {
		return svc.Stopped, errors.New("Aeontra Windows Edge service is unavailable")
	}
	defer service.Close()
	serviceConfig, err := service.Config()
	if err != nil || serviceConfig.ServiceStartName != windowsDoctorServiceIdentity {
		return svc.Stopped, errors.New("Aeontra Windows Edge service identity is invalid")
	}
	expectedBinary := filepath.Join(activePath, "bin", "mcp-edge.exe")
	if !strings.EqualFold(serviceConfig.BinaryPathName, windowsDoctorServiceCommand(expectedBinary, config)) {
		return svc.Stopped, errors.New("Aeontra Windows Edge service binary is not the active signed release")
	}
	status, err := service.Query()
	if err != nil {
		return svc.Stopped, errors.New("Aeontra Windows Edge service state is unavailable")
	}
	if status.State != svc.Running {
		return status.State, errors.New("Aeontra Windows Edge service is not running")
	}
	return status.State, nil
}

func windowsDoctorServiceState(state svc.State) string {
	if state == svc.Running {
		return "active"
	}
	return "inactive"
}

func cleanWindowsDoctorPath(value string) (string, error) {
	path := strings.TrimSpace(value)
	if path == "" || !filepath.IsAbs(path) || filepath.VolumeName(path) == "" || strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, `\\?\`) || strings.HasPrefix(path, `\\.\`) {
		return "", errors.New("Windows path is not local absolute")
	}
	clean := filepath.Clean(path)
	volumeRoot := filepath.VolumeName(clean) + string(filepath.Separator)
	if strings.EqualFold(strings.TrimRight(clean, `\`), strings.TrimRight(volumeRoot, `\`)) {
		return "", errors.New("Windows path must not be a volume root")
	}
	return clean, nil
}

func windowsDoctorUpdaterPath() (string, error) {
	installRoot, err := windowsDoctorInstallRoot()
	if err != nil {
		return "", err
	}
	marker, err := loadWindowsDoctorActiveMarker(filepath.Join(installRoot, "active.json"), installRoot)
	if err != nil {
		return "", err
	}
	updater := filepath.Join(marker.Path, "bin", "mcp-bundle-updater.exe")
	info, err := os.Lstat(updater)
	if err != nil || !info.Mode().IsRegular() || windowsDoctorIsReparse(updater) {
		return "", errors.New("signed Windows updater is unavailable")
	}
	return updater, nil
}

var runWindowsDoctorUpdater = func(args []string, stdout, stderr io.Writer) error {
	updater, err := windowsDoctorUpdaterPath()
	if err != nil {
		return err
	}
	command := exec.Command(updater, args...)
	command.Stdout, command.Stderr = stdout, stderr
	return command.Run()
}

func windowsDoctorIsReparse(path string) bool {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return true
	}
	attributes, err := windows.GetFileAttributes(pointer)
	return err == nil && attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func commitValid(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func windowsDoctorServiceCommand(binary string, config windowsDoctorServiceConfig) string {
	return `"` + binary + `" windows-agent --state "` + config.StateRoot + `" --root "` + config.WorkspaceRoot + `" --service-identity "` + windowsDoctorServiceIdentity + `" --pair-request "` + filepath.Join(config.StateRoot, "pair-request.json") + `"`
}
