//go:build !windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/autopilot"
	"github.com/charle-z/mcp-devbox/internal/bundle"
	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
	"github.com/charle-z/mcp-devbox/internal/edgeupdate"
)

func runControlOperationLoop(ctx context.Context, stateRoot string, transport *edgeclient.Transport, maxProcesses int, maxLogBytes int64, stderr io.Writer) {
	processes, err := edgeclient.OpenProjectProcessManager(edgeclient.ProjectProcessManagerConfig{StateRoot: stateRoot, MaxProcesses: maxProcesses, MaxLogBytes: maxLogBytes})
	if err != nil {
		fmt.Fprintln(stderr, "mcp-edge: project process journal failed safely")
		return
	}
	defer processes.Close()
	browsers, err := edgeclient.OpenProjectBrowserManager(edgeclient.ProjectBrowserManagerConfig{Root: filepath.Join(stateRoot, "project-browser"), Runner: edgeclient.NewProjectBrowserRunner()})
	if err != nil {
		fmt.Fprintln(stderr, "mcp-edge: project browser journal failed safely")
		return
	}
	defer browsers.Close()
	for {
		if ctx.Err() != nil {
			return
		}
		lease, err := transport.LeaseOperation(ctx, time.Minute)
		if err != nil {
			fmt.Fprintln(stderr, "mcp-edge: control operation polling failed safely")
			if !waitControlOperation(ctx, 5*time.Second) {
				return
			}
			continue
		}
		if lease == nil {
			if !waitControlOperation(ctx, 2*time.Second) {
				return
			}
			continue
		}
		result, code, cancelRequested, lifecycleErr := executeControlOperationWithProgress(ctx, stateRoot, transport, processes, browsers, *lease)
		if lifecycleErr != nil {
			fmt.Fprintln(stderr, "mcp-edge: control operation progress failed safely")
			continue
		}
		if cancelRequested {
			if err = acknowledgeControlOperationCancellation(ctx, stateRoot, transport, *lease); err != nil {
				fmt.Fprintln(stderr, "mcp-edge: control operation cancellation failed safely")
			}
			continue
		}
		if code == "" {
			registry, openErr := edgeclient.OpenWorkspaceRegistry(stateRoot)
			if openErr == nil {
				workspaces, listErr := registry.List()
				_ = registry.Close()
				if listErr != nil || transport.RegisterWorkspaces(ctx, workspaces) != nil {
					code = "workspace_registration_failed"
					result = edge.OperationResult{}
				}
			} else {
				code = "workspace_registration_failed"
				result = edge.OperationResult{}
			}
		}
		completionCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		_, err = transport.CompleteOperation(completionCtx, lease.Operation.ID, lease.LeaseID, result, code)
		if err == nil && isBundleOperation(lease.Operation.Kind) {
			clearBundleReceipt(stateRoot, lease.Operation.ID)
		}
		if err == nil && result.JobID != "" {
			err = transport.ReportAutopilot(completionCtx, result)
		}
		cancel()
		if err != nil {
			fmt.Fprintln(stderr, "mcp-edge: control operation completion failed safely")
		}
	}
}

func executeControlOperation(ctx context.Context, stateRoot string, processes *edgeclient.ProjectProcessManager, browsers *edgeclient.ProjectBrowserManager, operation edge.Operation) (edge.OperationResult, string) {
	var output strings.Builder
	switch operation.Kind {
	case edge.OperationLabPrepare:
		request := operation.Request
		err := labInit([]string{"--state", stateRoot, "--platform", request.Platform, "--machine", request.Machine, "--target", request.Target, "--difficulty", request.Difficulty, "--os", request.OperatingSystem}, &output, io.Discard)
		if err != nil {
			return edge.OperationResult{}, safeControlFailure(err)
		}
		return resolvePreparedWorkspace(stateRoot, request.Machine)
	case edge.OperationLabRetarget:
		err := labRetarget([]string{"--state", stateRoot, "--workspace-id", operation.Request.WorkspaceID, "--target", operation.Request.Target}, &output, io.Discard)
		if err != nil {
			return edge.OperationResult{}, safeControlFailure(err)
		}
		return resolveOperationWorkspace(stateRoot, operation.Request.WorkspaceID)
	case edge.OperationAutopilotStart, edge.OperationAutopilotPause, edge.OperationAutopilotResume, edge.OperationAutopilotCancel:
		return executeAutopilotControl(stateRoot, operation)
	case edge.OperationProjectPrepare:
		return executeProjectPrepare(ctx, stateRoot, operation.Request)
	case edge.OperationProjectStatus:
		return executeProjectStatus(ctx, stateRoot, operation.Request)
	case edge.OperationProjectSnapshot:
		return executeProjectSnapshot(ctx, stateRoot, operation.Request)
	case edge.OperationProjectExec:
		return executeProjectExec(ctx, stateRoot, operation)
	case edge.OperationProjectNetworkRoute, edge.OperationProjectNetworkProbe:
		return executeProjectNetwork(ctx, stateRoot, operation)
	case edge.OperationProjectProcessStart, edge.OperationProjectProcessStatus, edge.OperationProjectProcessStop, edge.OperationProjectProcessSignal, edge.OperationProjectProcessList, edge.OperationProjectProcessCleanup:
		return executeProjectProcess(ctx, stateRoot, processes, operation)
	case edge.OperationProjectBrowserCreate, edge.OperationProjectBrowserStatus, edge.OperationProjectBrowserList, edge.OperationProjectBrowserRun, edge.OperationProjectBrowserArtifactRead, edge.OperationProjectBrowserClose, edge.OperationProjectBrowserCleanup:
		return executeProjectBrowser(ctx, stateRoot, browsers, operation)
	case edge.OperationProjectGitStatus, edge.OperationProjectGitFetch, edge.OperationProjectGitFastForwardPreview, edge.OperationProjectGitFastForward:
		return executeProjectGitSync(ctx, stateRoot, operation)
	case edge.OperationProjectGitHubStatus:
		return executeProjectGitHubStatus(ctx, stateRoot, operation)
	case edge.OperationProjectToolboxCreate, edge.OperationProjectToolboxStatus, edge.OperationProjectToolboxExec, edge.OperationProjectToolboxInstall, edge.OperationProjectToolboxCleanup,
		edge.OperationProjectToolboxRepair, edge.OperationProjectToolboxServiceStart, edge.OperationProjectToolboxServiceStatus, edge.OperationProjectToolboxServiceStop,
		edge.OperationProjectBrowserHarnessStart, edge.OperationProjectBrowserHarnessStatus, edge.OperationProjectBrowserHarnessList, edge.OperationProjectBrowserHarnessStop, edge.OperationProjectBrowserHarnessCleanup, edge.OperationProjectBrowserHarnessArtifactList, edge.OperationProjectBrowserHarnessArtifactRead:
		return executeProjectToolbox(ctx, stateRoot, operation)
	case edge.OperationBundleStatus, edge.OperationOnboardingStatus:
		return collectEdgeDiagnostic(stateRoot, true)
	case edge.OperationBundleUpdate, edge.OperationBundleRollback, edge.OperationEdgeRepair:
		return executeBundleControl(stateRoot, operation)
	default:
		return edge.OperationResult{}, "operation_invalid"
	}
}

func executeProjectGitHubStatus(ctx context.Context, stateRoot string, operation edge.Operation) (edge.OperationResult, string) {
	credential, workspaces, projects, _, code := openProjectControlState(stateRoot)
	if code != "" {
		return edge.OperationResult{}, code
	}
	defer workspaces.Close()
	defer projects.Close()
	resolved, err := projects.Resolve(ctx, operation.Request.Alias, operation.Request.TargetAlias)
	if err != nil {
		return edge.OperationResult{}, "project_github_status_failed"
	}
	result, err := collectProjectGitHubStatus(ctx, resolved, credential, edgeclient.NewGitHubCommandRunner(stateRoot, "/usr/local/bin:/usr/bin:/bin"))
	if err != nil {
		return edge.OperationResult{}, "project_github_status_failed"
	}
	return result, ""
}

func executeProjectGitSync(ctx context.Context, stateRoot string, operation edge.Operation) (edge.OperationResult, string) {
	credential, workspaces, projects, _, code := openProjectControlState(stateRoot)
	if code != "" {
		return edge.OperationResult{}, code
	}
	defer workspaces.Close()
	defer projects.Close()
	resolved, err := projects.Resolve(ctx, operation.Request.Alias, operation.Request.TargetAlias)
	if err != nil {
		return edge.OperationResult{}, safeProjectControlFailure(err)
	}
	runner := edgeclient.NewDevGitCommandRunner(stateRoot, "/usr/local/bin:/usr/bin:/bin")
	var result edge.OperationResult
	switch operation.Kind {
	case edge.OperationProjectGitStatus:
		result, err = inspectProjectGitCheckout(ctx, resolved, runner, credential)
	case edge.OperationProjectGitFetch:
		result, err = fetchProjectGitCheckout(ctx, resolved, runner, credential)
	case edge.OperationProjectGitFastForwardPreview:
		result, err = previewProjectGitFastForward(ctx, stateRoot, resolved, runner, credential, time.Now().UTC())
	case edge.OperationProjectGitFastForward:
		result, err = executeProjectGitFastForward(ctx, stateRoot, resolved, operation.Request.GitPlanID, runner, credential, time.Now().UTC())
	default:
		return edge.OperationResult{}, "operation_invalid"
	}
	if err != nil {
		return edge.OperationResult{}, "project_git_sync_failed"
	}
	return result, ""
}

func executeProjectPrepare(ctx context.Context, stateRoot string, request edge.OperationRequest) (edge.OperationResult, string) {
	credential, workspaces, projects, roots, code := openProjectControlState(stateRoot)
	if code != "" {
		return edge.OperationResult{}, code
	}
	defer workspaces.Close()
	defer projects.Close()
	config := edgeclient.ProjectPreparationConfig{
		StateRoot: stateRoot, Projects: projects, Workspaces: workspaces, Roots: roots,
		Credential: credential, Runner: edgeclient.NewDevGitCommandRunner(stateRoot, "/usr/local/bin:/usr/bin:/bin"),
	}
	plan, err := edgeclient.PlanProjectPreparation(ctx, config, edgeclient.ProjectPreparationRequest{
		Alias: request.Alias, Repository: request.Repository, TargetAlias: request.TargetAlias,
		Profile: edgeclient.WorkspaceProfile(request.Profile),
	})
	if err != nil {
		return edge.OperationResult{}, safeProjectControlFailure(err)
	}
	if _, err := edgeclient.ApplyProjectPreparation(ctx, config, plan); err != nil {
		return edge.OperationResult{}, safeProjectControlFailure(err)
	}
	return projectControlResult(ctx, projects, request.Alias, request.TargetAlias)
}

func executeProjectStatus(ctx context.Context, stateRoot string, request edge.OperationRequest) (edge.OperationResult, string) {
	_, workspaces, projects, _, code := openProjectControlState(stateRoot)
	if code != "" {
		return edge.OperationResult{}, code
	}
	defer workspaces.Close()
	defer projects.Close()
	return projectControlResult(ctx, projects, request.Alias, request.TargetAlias)
}

func executeProjectSnapshot(ctx context.Context, stateRoot string, request edge.OperationRequest) (edge.OperationResult, string) {
	credential, workspaces, projects, _, code := openProjectControlState(stateRoot)
	if code != "" {
		return edge.OperationResult{}, code
	}
	defer workspaces.Close()
	defer projects.Close()
	resolved, err := projects.Resolve(ctx, request.Alias, request.TargetAlias)
	if err != nil {
		return edge.OperationResult{}, safeProjectControlFailure(err)
	}
	return collectProjectSnapshot(ctx, resolved, edgeclient.NewDevGitCommandRunner(stateRoot, "/usr/local/bin:/usr/bin:/bin"), credential)
}

var projectSnapshotHeadPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var projectSnapshotBranchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)

func collectProjectSnapshot(ctx context.Context, resolved edgeclient.ProjectResolution, runner edgeclient.DevGitCommandRunner, credential edgeclient.GitHubCredential) (edge.OperationResult, string) {
	if runner == nil || resolved.Workspace.Profile != edgeclient.WorkspaceProfileLinuxWorkcell || resolved.Workspace.Mode != edgeclient.WorkspaceModeDev {
		return edge.OperationResult{}, "project_snapshot_invalid"
	}
	headOutput, err := runner.Run(ctx, resolved.Workspace.Path, []string{"rev-parse", "--verify", "HEAD"}, credential)
	head := strings.TrimSpace(headOutput)
	if err != nil || !projectSnapshotHeadPattern.MatchString(head) {
		return edge.OperationResult{}, "project_snapshot_failed"
	}
	branchOutput, err := runner.Run(ctx, resolved.Workspace.Path, []string{"branch", "--show-current"}, credential)
	branch := strings.TrimSpace(branchOutput)
	if err != nil || !validProjectSnapshotBranch(branch) {
		return edge.OperationResult{}, "project_snapshot_failed"
	}
	statusOutput, err := runner.Run(ctx, resolved.Workspace.Path, []string{"status", "--porcelain=v1", "--untracked-files=all"}, credential)
	if err != nil {
		return edge.OperationResult{}, "project_snapshot_failed"
	}
	clean := edgeclient.ProjectCheckoutStatusClean(statusOutput)
	projectState := string(edgeclient.ProjectCheckoutReady)
	if !clean {
		projectState = string(edgeclient.ProjectCheckoutDirty)
	}
	return edge.OperationResult{
		WorkspaceID:  resolved.Workspace.ID,
		ProjectAlias: resolved.Project.Alias, ProjectOwner: resolved.Project.Owner,
		ProjectRepository: resolved.Project.Repository, ProjectTarget: resolved.TargetAlias,
		ProjectState: projectState, ProjectProfile: string(resolved.Workspace.Profile), ProjectMode: string(resolved.Workspace.Mode),
		SnapshotBranch: branch, SnapshotHead: head, SnapshotClean: clean,
	}, ""
}

func validProjectSnapshotBranch(branch string) bool {
	return projectSnapshotBranchPattern.MatchString(branch) &&
		!strings.HasPrefix(branch, "-") && !strings.Contains(branch, "..") && !strings.Contains(branch, "//") &&
		!strings.Contains(branch, "@{") && !strings.HasSuffix(branch, "/") && !strings.HasSuffix(branch, ".") &&
		!strings.HasSuffix(branch, ".lock")
}

func openProjectControlState(stateRoot string) (edgeclient.GitHubCredential, *edgeclient.WorkspaceRegistry, *edgeclient.ProjectRegistry, edgeclient.WorkspaceRoots, string) {
	credential, err := edgeclient.LoadGitHubCredential(stateRoot)
	if err != nil {
		return edgeclient.GitHubCredential{}, nil, nil, edgeclient.WorkspaceRoots{}, "project_credential_unavailable"
	}
	roots, err := edgeclient.DefaultWorkspaceRoots()
	if err != nil {
		return edgeclient.GitHubCredential{}, nil, nil, edgeclient.WorkspaceRoots{}, "project_roots_unavailable"
	}
	workspaces, err := edgeclient.OpenWorkspaceRegistryWithRoots(stateRoot, roots)
	if err != nil {
		return edgeclient.GitHubCredential{}, nil, nil, edgeclient.WorkspaceRoots{}, "project_registry_unavailable"
	}
	projects, err := edgeclient.OpenProjectRegistry(edgeclient.ProjectRegistryConfig{
		StateRoot: stateRoot, AllowedOwner: credential.Owner, Workspaces: workspaces,
	})
	if err != nil {
		_ = workspaces.Close()
		return edgeclient.GitHubCredential{}, nil, nil, edgeclient.WorkspaceRoots{}, "project_registry_unavailable"
	}
	return credential, workspaces, projects, roots, ""
}

func projectControlResult(ctx context.Context, projects *edgeclient.ProjectRegistry, alias, target string) (edge.OperationResult, string) {
	resolved, err := projects.Resolve(ctx, alias, target)
	if err != nil {
		return edge.OperationResult{}, safeProjectControlFailure(err)
	}
	return edge.OperationResult{
		WorkspaceID:       resolved.Workspace.ID,
		ProjectAlias:      resolved.Project.Alias,
		ProjectOwner:      resolved.Project.Owner,
		ProjectRepository: resolved.Project.Repository,
		ProjectTarget:     resolved.TargetAlias,
		ProjectState:      resolved.SafeState(),
		ProjectProfile:    string(resolved.Workspace.Profile),
		ProjectMode:       string(resolved.Workspace.Mode),
	}, ""
}

func safeProjectControlFailure(err error) string {
	var projectFailure *edgeclient.ProjectError
	if errors.As(err, &projectFailure) {
		code := string(projectFailure.Code)
		if regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`).MatchString(code) {
			return "project_" + code
		}
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return "project_operation_failed"
}

type bundleOperationReceipt struct {
	OperationID string             `json:"operation_id"`
	Kind        edge.OperationKind `json:"kind"`
}

const bundleReceiptFile = "bundle-operation-receipt.json"

func executeBundleControl(stateRoot string, operation edge.Operation) (edge.OperationResult, string) {
	unit := bundleOperationUnit(operation.Kind)
	if unit == "" {
		return edge.OperationResult{}, "operation_invalid"
	}
	receipt, receiptErr := readBundleReceipt(stateRoot)
	switch {
	case receiptErr == nil:
		if receipt.OperationID != operation.ID || receipt.Kind != operation.Kind {
			return edge.OperationResult{}, "updater_busy"
		}
		if !waitBundleUnitInactive(unit, 15*time.Minute) {
			return edge.OperationResult{}, "updater_timeout"
		}
		return collectEdgeDiagnostic(stateRoot, false)
	case !errors.Is(receiptErr, os.ErrNotExist):
		return edge.OperationResult{}, "updater_receipt_invalid"
	}
	if err := writeBundleReceipt(stateRoot, bundleOperationReceipt{OperationID: operation.ID, Kind: operation.Kind}); err != nil {
		return edge.OperationResult{}, "updater_receipt_unavailable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 16*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/bin/systemctl", "start", unit)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if command.Run() != nil {
		clearBundleReceipt(stateRoot, operation.ID)
		return edge.OperationResult{}, "updater_failed"
	}
	return collectEdgeDiagnostic(stateRoot, false)
}

func bundleOperationUnit(kind edge.OperationKind) string {
	switch kind {
	case edge.OperationBundleUpdate:
		return "mcp-devbox-bundle-updater.service"
	case edge.OperationBundleRollback:
		return "mcp-devbox-bundle-rollback.service"
	case edge.OperationEdgeRepair:
		return "mcp-devbox-edge-repair.service"
	}
	return ""
}

func waitBundleUnitInactive(unit string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		command := exec.CommandContext(ctx, "/usr/bin/systemctl", "is-active", "--quiet", unit)
		command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
		command.Stdout = io.Discard
		command.Stderr = io.Discard
		err := command.Run()
		cancel()
		if err != nil {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Second)
	}
}

func writeBundleReceipt(stateRoot string, receipt bundleOperationReceipt) error {
	if !edgeOperationID(receipt.OperationID) || !isBundleOperation(receipt.Kind) {
		return errors.New("bundle operation receipt is invalid")
	}
	content, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	path := bundleReceiptPath(stateRoot)
	temporary := path + ".tmp"
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(content, '\n'))
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(temporary)
		return errors.New("bundle operation receipt write failed")
	}
	if err := os.Link(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return os.Remove(temporary)
}

func readBundleReceipt(stateRoot string) (bundleOperationReceipt, error) {
	path := bundleReceiptPath(stateRoot)
	info, err := os.Lstat(path)
	if err != nil {
		return bundleOperationReceipt{}, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() > 1024 {
		return bundleOperationReceipt{}, errors.New("bundle operation receipt is unsafe")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return bundleOperationReceipt{}, err
	}
	var receipt bundleOperationReceipt
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&receipt) != nil || decoder.Decode(&struct{}{}) != io.EOF || !edgeOperationID(receipt.OperationID) || !isBundleOperation(receipt.Kind) {
		return bundleOperationReceipt{}, errors.New("bundle operation receipt is invalid")
	}
	return receipt, nil
}

func clearBundleReceipt(stateRoot, operationID string) {
	receipt, err := readBundleReceipt(stateRoot)
	if err == nil && receipt.OperationID == operationID {
		_ = os.Remove(bundleReceiptPath(stateRoot))
	}
}

func bundleReceiptPath(stateRoot string) string {
	return stateRoot + string(os.PathSeparator) + bundleReceiptFile
}

func edgeOperationID(value string) bool {
	return len(value) == len("eo_")+32 && strings.HasPrefix(value, "eo_") && strings.IndexFunc(value[3:], func(r rune) bool {
		return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f')
	}) == -1
}

func isBundleOperation(kind edge.OperationKind) bool {
	return bundleOperationUnit(kind) != ""
}
func collectEdgeDiagnostic(stateRoot string, allowInvalid bool) (edge.OperationResult, string) {
	verified, verifyErr := verifyInstalledEdgeBundleAt(installedBundleRoot)
	manifestStatus := "valid"
	componentsCompatible := verifyErr == nil
	providerValid := componentsCompatible
	driverValid := componentsCompatible
	if verifyErr != nil {
		manifestStatus = openCodeFailureCode(verifyErr)
		var verification *bundle.VerificationError
		if errors.As(verifyErr, &verification) && verification.Code == bundle.ProviderOutdated {
			driverValid = true
		}
		if !allowInvalid {
			return edge.OperationResult{}, manifestStatus
		}
	}
	identity, _, identityErr := edgeclient.LoadIdentity(stateRoot)
	registry, registryErr := edgeclient.OpenWorkspaceRegistry(stateRoot)
	count := 0
	if registryErr == nil {
		items, listErr := registry.List()
		_ = registry.Close()
		if listErr == nil {
			count = len(items)
		} else {
			registryErr = listErr
		}
	}
	bubble := false
	if info, statErr := os.Stat("/usr/bin/bwrap"); statErr == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
		bubble = true
	}
	rootless := false
	if endpoint, discoverErr := edgeclient.DiscoverRootlessContainerEndpoint(os.Geteuid(), ""); discoverErr == nil && endpoint != nil {
		rootless = true
	}
	if identityErr != nil || registryErr != nil {
		return edge.OperationResult{}, "diagnostic_unavailable"
	}
	blockers := []string{}
	if !componentsCompatible {
		blockers = append(blockers, manifestStatus)
	}
	paired := identity.DeviceID != ""
	if !paired {
		blockers = append(blockers, "edge_unpaired")
	}
	runtime := inspectEdgeRuntime(stateRoot, installedEdgeServiceName())
	blockers = appendUniqueBlockers(blockers, runtime.Blockers...)
	serviceActive := runtime.ServiceActive
	if !bubble {
		blockers = append(blockers, "bubblewrap_unavailable")
	}
	if !rootless {
		blockers = append(blockers, "rootless_unavailable")
	}
	if !installedModelProviderValid("/etc/mcp-devbox/autopilot-model.json") {
		blockers = append(blockers, "local_model_unconfigured")
	}
	available := false
	if key, keyErr := installedBundlePublicKey(); keyErr == nil {
		channelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		var channelErr error
		available, channelErr = (edgeupdate.OfficialResolver{PublicKey: key}).StableAvailable(channelCtx, verified.Release)
		cancel()
		if channelErr != nil {
			blockers = append(blockers, "release_channel_unavailable")
		}
	}
	result := edge.OperationResult{Release: verified.Release, Commit: verified.Commit, ManifestStatus: manifestStatus, ComponentsCompatible: componentsCompatible, ServiceActive: serviceActive, UpdateAvailable: available, Paired: paired, BubblewrapValid: bubble, RootlessValid: rootless, WorkspaceCount: count, ProviderValid: providerValid, DriverValid: driverValid, Blockers: blockers}
	result.ServiceState = runtime.ServiceState
	result.ServiceRestarts = runtime.ServiceRestarts
	result.ServiceRestartsKnown = runtime.ServiceRestartsKnown
	result.ProcessState = runtime.ProcessState
	result.LockState = runtime.LockState
	result.Coherence = runtime.Coherence
	result.ProcessRelease = runtime.ProcessRelease
	result.ProcessCommit = runtime.ProcessCommit
	return result, ""
}

func installedModelProviderValid(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 || info.Size() <= 0 || info.Size() > 4096 {
		return false
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var config struct {
		Version  int    `json:"version"`
		Provider string `json:"provider"`
		Endpoint string `json:"endpoint"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil || decoder.Decode(&struct{}{}) != io.EOF || config.Version != 1 || (config.Provider != "local-http" && config.Provider != "opencode-local") {
		return false
	}
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Scheme != "http" || endpoint.User != nil || endpoint.Port() == "" || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Path != "/v1/next-action" {
		return false
	}
	host, _, err := net.SplitHostPort(endpoint.Host)
	return err == nil && net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func executeAutopilotControl(stateRoot string, operation edge.Operation) (edge.OperationResult, string) {
	registry, err := edgeclient.OpenWorkspaceRegistry(stateRoot)
	if err != nil {
		return edge.OperationResult{}, "workspace_unavailable"
	}
	workspace, err := registry.Get(operation.Request.WorkspaceID)
	_ = registry.Close()
	if err != nil || workspace.Mode != edgeclient.WorkspaceModeHTBLinux {
		return edge.OperationResult{}, "workspace_unavailable"
	}
	store := autopilot.Store{Workspace: workspace.Path}
	var job autopilot.State
	switch operation.Kind {
	case edge.OperationAutopilotStart:
		job, _, err = store.Start(workspace.ID, operation.Request.RunUntil)
	case edge.OperationAutopilotPause:
		job, err = store.Pause()
	case edge.OperationAutopilotResume:
		job, err = store.Resume()
	case edge.OperationAutopilotCancel:
		job, err = store.Cancel()
	}
	if err != nil {
		return edge.OperationResult{}, "autopilot_control_failed"
	}
	return edge.OperationResult{WorkspaceID: workspace.ID, JobID: job.JobID, JobState: string(job.State), ProgressRevision: job.ProgressRevision, CycleCount: job.CycleCount, JobSafeCode: job.SafeCode}, ""
}

func resolvePreparedWorkspace(stateRoot, machine string) (edge.OperationResult, string) {
	registry, err := edgeclient.OpenWorkspaceRegistry(stateRoot)
	if err != nil {
		return edge.OperationResult{}, "workspace_unavailable"
	}
	defer registry.Close()
	workspaces, err := registry.List()
	if err != nil {
		return edge.OperationResult{}, "workspace_unavailable"
	}
	for _, workspace := range workspaces {
		if workspace.Mode == edgeclient.WorkspaceModeHTBLinux && workspace.MachineName == machine {
			return edge.OperationResult{WorkspaceID: workspace.ID, AuthorizationRevision: workspace.AuthorizationRevision}, ""
		}
	}
	return edge.OperationResult{}, "workspace_unavailable"
}

func resolveOperationWorkspace(stateRoot, id string) (edge.OperationResult, string) {
	registry, err := edgeclient.OpenWorkspaceRegistry(stateRoot)
	if err != nil {
		return edge.OperationResult{}, "workspace_unavailable"
	}
	defer registry.Close()
	workspace, err := registry.Get(id)
	if err != nil {
		return edge.OperationResult{}, "workspace_unavailable"
	}
	return edge.OperationResult{WorkspaceID: workspace.ID, AuthorizationRevision: workspace.AuthorizationRevision}, ""
}

func safeControlFailure(err error) string {
	if err == nil {
		return "none"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "vpn") || strings.Contains(message, "route"):
		return "vpn_unavailable"
	case strings.Contains(message, "target"):
		return "target_invalid"
	case strings.Contains(message, "inventory") || strings.Contains(message, "tool"):
		return "tools_unavailable"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "operation_failed"
	}
}

func waitControlOperation(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
