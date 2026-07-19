//go:build !windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

var labRuntimeIDPattern = regexp.MustCompile(`^mr_[a-f0-9]{32}$`)

func labSSHExec(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("lab ssh-exec", flag.ContinueOnError)
	fs.SetOutput(stderr)
	request := edgeclient.HTBLabSSHRequest{}
	fs.StringVar(&request.Username, "username", "", "remote lab username")
	fs.StringVar(&request.Source, "source", "", "workspace-relative artifact containing the recovered password")
	fs.StringVar(&request.ExtractAfter, "extract-after", "", "literal prefix immediately before the password")
	fs.StringVar(&request.Command, "command", "", "bounded command to execute on the authorized target")
	fs.StringVar(&request.SaveOutput, "save-output", "", "optional workspace-relative file for local-only stdout")
	fs.BoolVar(&request.PasswordStdin, "password-stdin", false, "also send the recovered password to remote stdin")
	fs.BoolVar(&request.PTY, "pty", false, "request a pseudo-terminal")
	fs.IntVar(&request.Port, "port", 22, "SSH port on the registered target")
	timeout := fs.Duration("timeout", 2*time.Minute, "SSH timeout from 5s to 10m")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("lab ssh-exec accepts no positional arguments")
	}
	if os.Getenv("MCP_DEVBOX_PROFILE") != "linux-workcell" || os.Getenv("MCP_DEVBOX_MODE") != "htb-linux" {
		return errors.New("lab SSH execution requires an active htb-linux runtime")
	}
	workspace, err := os.Getwd()
	if err != nil || filepath.Clean(workspace) != "/workspace" {
		return errors.New("lab SSH execution requires the selected workspace")
	}
	if *timeout < 5*time.Second || *timeout > 10*time.Minute {
		return errors.New("lab SSH timeout must be between 5s and 10m")
	}
	request.TimeoutSeconds = int(*timeout / time.Second)
	payload, err := json.Marshal(request)
	if err != nil {
		return errors.New("lab SSH request could not be encoded")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout+15*time.Second)
	defer cancel()
	socketPath := filepath.Join("/runtime", edgeclient.HTBLabBrokerSocketName)
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", socketPath)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: *timeout + 10*time.Second}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, edgeclient.HTBLabBrokerSandboxURL, bytes.NewReader(payload))
	if err != nil {
		return errors.New("lab SSH request could not be created")
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := client.Do(httpRequest)
	if err != nil {
		return errors.New("lab SSH broker is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("lab SSH broker rejected the request")
	}
	result, err := edgeclient.ReadHTBLabBrokerResponse(response.Body)
	if err != nil {
		return err
	}
	if result.SavedPath != "" {
		fmt.Fprintf(stdout, "lab-ssh-ok target=%s user=%s bytes=%d sha256=%s saved=%s\n", result.Target, result.Username, result.Bytes, result.SHA256, result.SavedPath)
		return nil
	}
	if _, err := io.WriteString(stdout, result.Stdout); err != nil {
		return err
	}
	_, err = io.WriteString(stderr, result.Stderr)
	return err
}

func runAskpassIfRequested(stdout io.Writer) (bool, error) {
	path := strings.TrimSpace(os.Getenv("MCP_DEVBOX_ASKPASS_FILE"))
	runtimeID := strings.TrimSpace(os.Getenv("MCP_DEVBOX_ASKPASS_RUNTIME"))
	if path == "" && runtimeID == "" {
		return false, nil
	}
	if path == "" || !labRuntimeIDPattern.MatchString(runtimeID) || !filepath.IsAbs(path) || !strings.HasPrefix(filepath.Base(path), ".ssh-askpass-") {
		return true, errors.New("lab askpass request is invalid")
	}
	for _, forbidden := range []string{"/workspace", "/runtime", "/tmp", "/mnt/c", "/mnt/d"} {
		if path == forbidden || pathInsideLocal(forbidden, path) {
			return true, errors.New("lab askpass path is invalid")
		}
	}
	parentDir := filepath.Dir(path)
	if filepath.Base(parentDir) != "lab-secrets" {
		return true, errors.New("lab askpass root is invalid")
	}
	parentInfo, err := os.Lstat(parentDir)
	if err != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 || parentInfo.Mode().Perm() != 0o700 || !ownedByCurrentUser(parentInfo) {
		return true, errors.New("lab askpass root is unsafe")
	}
	parent, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", os.Getppid()))
	if err != nil || filepath.Base(parent) != "ssh" {
		return true, errors.New("lab askpass parent is invalid")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || info.Size() <= 0 || info.Size() > 1024 || !ownedByCurrentUser(info) {
		return true, errors.New("lab askpass file is unsafe")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return true, errors.New("lab askpass file is unavailable")
	}
	_ = os.Remove(path)
	defer zeroBytes(body)
	_, err = stdout.Write(append(body, '\n'))
	return true, err
}

func pathInsideLocal(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Geteuid()
}

func zeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
