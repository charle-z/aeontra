//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
	"golang.org/x/sys/windows/svc"
)

const windowsAgentPairRequestLimit = 4096

// windowsPairRequest is deliberately a one-shot, operator-created handoff.
// It is not a replacement for SCM onboarding: it only permits the first
// pairing when the state root does not yet contain an identity.
type windowsPairRequest struct {
	Server string `json:"server"`
	Name   string `json:"name"`
	Code   string `json:"code"`
}

func runWindowsAgent(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("windows-agent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", defaultStateRoot(), "private Edge state root")
	root := fs.String("root", "", "administrator-owned native Windows workspace root")
	poll := fs.Duration("poll", 2*time.Second, "empty operation polling interval")
	leaseTTL := fs.Duration("lease", 10*time.Minute, "operation lease duration")
	once := fs.Bool("once", false, "process at most one operation lease")
	pairRequest := fs.String("pair-request", "", "one-shot private pairing request JSON")
	serviceIdentity := fs.String("service-identity", `NT SERVICE\AeontraEdge`, "required SCM service identity (name or SID)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || strings.TrimSpace(*root) == "" || *poll <= 0 || *poll > time.Minute || *leaseTTL < 15*time.Second || *leaseTTL > 10*time.Minute {
		return errors.New("windows-agent arguments are invalid")
	}
	if err := ensureWorkcellUser(); err != nil {
		return err
	}
	if err := ensureWindowsServiceIdentity(*serviceIdentity); err != nil {
		return err
	}
	if err := edgeclient.ConfigureWindowsWorkspaceRoot(*root); err != nil {
		return err
	}
	config := windowsAgentConfig{stateRoot: *state, poll: *poll, leaseTTL: *leaseTTL, once: *once, pairRequest: strings.TrimSpace(*pairRequest), stderr: stderr}
	isService, err := svc.IsWindowsService()
	if err != nil {
		return errors.New("Windows service context is unavailable")
	}
	if isService {
		return runWindowsAgentService(config)
	}
	if config.pairRequest != "" {
		if err := consumeWindowsPairRequest(config.stateRoot, config.pairRequest, stdin, stdout); err != nil {
			return err
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return runWindowsAgentLoop(ctx, config)
}

type windowsAgentConfig struct {
	stateRoot   string
	poll        time.Duration
	leaseTTL    time.Duration
	once        bool
	pairRequest string
	stderr      io.Writer
}

func runWindowsAgentService(config windowsAgentConfig) error {
	return svc.Run("AeontraEdge", windowsAgentService{config: config})
}

type windowsAgentService struct {
	config windowsAgentConfig
}

func (service windowsAgentService) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending, WaitHint: 10_000}
	if service.config.pairRequest != "" {
		if err := consumeWindowsPairRequest(service.config.stateRoot, service.config.pairRequest, nil, nil); err != nil {
			return true, 1
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- runWindowsAgentLoop(ctx, service.config) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case request, ok := <-requests:
			if !ok {
				cancel()
				<-result
				status <- svc.Status{State: svc.StopPending, WaitHint: 10_000}
				return false, 0
			}
			switch request.Cmd {
			case svc.Interrogate:
				status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending, WaitHint: 20_000}
				cancel()
				if err := <-result; err != nil && !errors.Is(err, context.Canceled) {
					return true, 1
				}
				return false, 0
			}
		case err := <-result:
			if err != nil && !errors.Is(err, context.Canceled) {
				return true, 1
			}
			return false, 0
		}
	}
}

func runWindowsAgentLoop(ctx context.Context, config windowsAgentConfig) error {
	transport, err := edgeclient.NewTransport(config.stateRoot, nil)
	if err != nil {
		return err
	}
	registry, err := edgeclient.OpenWorkspaceRegistry(config.stateRoot)
	if err != nil {
		return err
	}
	defer registry.Close()
	processes, err := edgeclient.OpenProjectProcessManager(edgeclient.ProjectProcessManagerConfig{StateRoot: config.stateRoot, MaxProcesses: 256, MaxLogBytes: 64 << 20})
	if err != nil {
		return errors.New("Windows project process journal is unavailable")
	}
	defer processes.Close()
	for {
		workspaces, listErr := registry.List()
		if listErr == nil {
			listErr = transport.RegisterWorkspaces(ctx, workspaces)
		}
		if listErr != nil {
			if config.once {
				return errors.New("Windows workspace registration failed")
			}
			fmt.Fprintln(config.stderr, "mcp-edge: Windows workspace registration failed safely")
			if !waitWindowsAgent(ctx, config.poll) {
				return nil
			}
			continue
		}
		lease, leaseErr := transport.LeaseOperation(ctx, config.leaseTTL)
		if leaseErr != nil {
			if config.once {
				return leaseErr
			}
			fmt.Fprintln(config.stderr, "mcp-edge: Windows control operation polling failed safely")
			if !waitWindowsAgent(ctx, config.poll) {
				return nil
			}
			continue
		}
		if lease == nil {
			if config.once {
				return nil
			}
			if !waitWindowsAgent(ctx, config.poll) {
				return nil
			}
			continue
		}
		result, safeCode, cancelRequested, lifecycleErr := executeWindowsControlOperationWithProgress(ctx, config.stateRoot, transport, processes, *lease)
		if lifecycleErr != nil {
			fmt.Fprintln(config.stderr, "mcp-edge: Windows control operation stopped safely")
			if config.once {
				return lifecycleErr
			}
			continue
		}
		if cancelRequested {
			cancelCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			_, cancelErr := transport.CancelOperation(cancelCtx, lease.Operation.ID, lease.LeaseID)
			cancel()
			if cancelErr != nil {
				fmt.Fprintln(config.stderr, "mcp-edge: Windows cancellation acknowledgement failed safely")
			}
		} else {
			completionCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			_, completeErr := transport.CompleteOperation(completionCtx, lease.Operation.ID, lease.LeaseID, result, safeCode)
			cancel()
			if completeErr != nil {
				fmt.Fprintln(config.stderr, "mcp-edge: Windows control operation completion failed safely")
			}
		}
		if config.once {
			return nil
		}
	}
}

func waitWindowsAgent(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func consumeWindowsPairRequest(stateRoot, requestPath string, _ io.Reader, _ io.Writer) error {
	root := filepath.Clean(strings.TrimSpace(stateRoot))
	path := filepath.Clean(strings.TrimSpace(requestPath))
	if !filepath.IsAbs(root) || !filepath.IsAbs(path) || !pathInsideWindows(root, path) || path == root {
		return errors.New("Windows pairing request path is invalid")
	}
	if _, _, err := edgeclient.LoadIdentity(root); err == nil {
		return errors.New("Windows Edge is already paired")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > windowsAgentPairRequestLimit {
		return errors.New("Windows pairing request is unavailable")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return errors.New("Windows pairing request is unavailable")
	}
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	var request windowsPairRequest
	if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("Windows pairing request is invalid")
	}
	server := strings.TrimSpace(request.Server)
	name := strings.TrimSpace(request.Name)
	code := strings.TrimSpace(request.Code)
	parsed, parseErr := url.Parse(server)
	if parseErr != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Host == "" || len(name) == 0 || len(name) > 128 || !strings.HasPrefix(code, "ep_") || len(code) > 128 {
		return errors.New("Windows pairing request is invalid")
	}
	if err := pair([]string{"--server", server, "--state", root, "--name", name}, strings.NewReader(code+"\n"), io.Discard, io.Discard); err != nil {
		return errors.New("Windows Edge pairing failed")
	}
	if err := os.Remove(path); err != nil {
		// The request is single-use even if deletion is temporarily denied: do
		// not leave a readable code behind.
		file, openErr := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
		if openErr != nil {
			return errors.New("Windows pairing request cleanup failed")
		}
		_ = file.Close()
		return errors.New("Windows pairing request cleanup failed")
	}
	return nil
}

func pathInsideWindows(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false
	}
	return true
}
