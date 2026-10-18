package edgeclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const workspaceAuthorizationRevisionFile = "authorization-revision"

const (
	workspaceLabContractFile   = "lab-contract.json"
	workspaceToolInventoryFile = "tool-inventory.json"
)

type workspaceLabContract struct {
	Version               int    `json:"version"`
	Platform              string `json:"platform"`
	Machine               string `json:"machine"`
	Target                string `json:"target"`
	LHost                 string `json:"lhost"`
	VPNInterface          string `json:"vpn_interface"`
	AuthorizationRevision uint64 `json:"authorization_revision"`
}

func writeWorkspaceAuthorizationRevision(workspace string, revision uint64) error {
	if revision == 0 {
		return errors.New("workspace authorization revision is invalid")
	}
	directory := filepath.Join(workspace, ".mcp-devbox")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return errors.New("workspace authorization state unavailable")
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return errors.New("workspace authorization state is unsafe")
	}
	temporary, err := os.CreateTemp(directory, ".authorization-revision-")
	if err != nil {
		return errors.New("workspace authorization state unavailable")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return errors.New("workspace authorization state is unsafe")
	}
	if _, err := fmt.Fprintf(temporary, "%d\n", revision); err != nil || temporary.Sync() != nil || temporary.Close() != nil {
		return errors.New("workspace authorization state unavailable")
	}
	if err := os.Rename(temporaryPath, filepath.Join(directory, workspaceAuthorizationRevisionFile)); err != nil {
		return errors.New("workspace authorization state unavailable")
	}
	return nil
}

func readWorkspaceAuthorizationRevision(workspace string) (uint64, error) {
	path := filepath.Join(workspace, ".mcp-devbox", workspaceAuthorizationRevisionFile)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || info.Size() > 32 {
		return 0, errors.New("workspace authorization state is invalid")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, errors.New("workspace authorization state is unavailable")
	}
	revision, err := strconv.ParseUint(strings.TrimSpace(string(content)), 10, 64)
	if err != nil || revision == 0 {
		return 0, errors.New("workspace authorization state is invalid")
	}
	return revision, nil
}

// WriteLabPreparation persists the complete local-only lab contract and sanitized
// tool inventory. The contract intentionally never crosses the control plane.
func WriteLabPreparation(workspace Workspace, inventory []LinuxToolInventoryEntry, lhost string) error {
	if workspace.Mode != WorkspaceModeHTBLinux || workspace.AuthorizationRevision == 0 {
		return errors.New("workspace is not an authorized HTB lab")
	}
	if parsed := net.ParseIP(strings.TrimSpace(lhost)); parsed == nil || parsed.To4() == nil {
		return errors.New("lab LHOST is invalid")
	} else {
		lhost = parsed.To4().String()
	}
	if err := ValidateLinuxToolInventory(inventory); err != nil {
		return err
	}
	contract := workspaceLabContract{
		Version: 1, Platform: "htb", Machine: workspace.MachineName,
		Target: workspace.TargetIP, LHost: lhost, VPNInterface: workspace.VPNInterface,
		AuthorizationRevision: workspace.AuthorizationRevision,
	}
	contractBody, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		return errors.New("lab contract could not be encoded")
	}
	inventoryBody, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		return errors.New("lab inventory could not be encoded")
	}
	directory := filepath.Join(workspace.Path, ".mcp-devbox")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return errors.New("lab state directory could not be created")
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return errors.New("lab state directory is unsafe")
	}
	if err := atomicWorkspaceFile(filepath.Join(directory, workspaceLabContractFile), append(contractBody, '\n'), 0o600); err != nil {
		return err
	}
	if err := atomicWorkspaceFile(filepath.Join(directory, workspaceToolInventoryFile), append(inventoryBody, '\n'), 0o600); err != nil {
		return err
	}
	return nil
}

// WriteLabRetarget updates only the authorization-bearing contract while
// preserving the existing sanitized inventory and all workspace evidence.
func WriteLabRetarget(workspace Workspace, lhost string) error {
	path := filepath.Join(workspace.Path, ".mcp-devbox", workspaceToolInventoryFile)
	content, err := os.ReadFile(path)
	if err != nil || len(content) == 0 || len(content) > 1<<20 {
		return errors.New("lab tool inventory is unavailable")
	}
	var inventory []LinuxToolInventoryEntry
	if err := json.Unmarshal(content, &inventory); err != nil {
		return errors.New("lab tool inventory is invalid")
	}
	return WriteLabPreparation(workspace, inventory, lhost)
}
