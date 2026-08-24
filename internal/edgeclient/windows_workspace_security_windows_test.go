//go:build windows

package edgeclient

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestIsWindowsLocalPathRejectsOtherNamespacesAndStreams(t *testing.T) {
	for _, path := range []string{
		`relative\workspace`, `C:workspace`, `\workspace`, `/workspace`,
		`\\server\share\workspace`, `//server/share/workspace`,
		`\\?\C:\workspace`, `\\?\UNC\server\share\workspace`,
		`\\.\PhysicalDrive0`, `\Device\HarddiskVolume1\workspace`,
		`C:\workspace:secret`, `C:\workspace\file.txt:secret`,
		`C:\workspace `, `C:\workspace\trailing.`,
	} {
		if IsWindowsLocalPath(path) {
			t.Errorf("namespace or stream path accepted: %q", path)
		}
	}
	for _, path := range []string{`C:\workspace`, `c:/workspace`, `D:\work`} {
		if !IsWindowsLocalPath(path) {
			t.Errorf("local drive path rejected: %q", path)
		}
	}
}

func TestWindowsWorkspaceACLRejectsBroadWriteAndInvalidOwner(t *testing.T) {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	service := user.User.Sid
	for name, fixture := range map[string]struct {
		sddl string
		ok   bool
	}{
		"service owner and broad read": {"O:" + service.String() + "D:P(A;;FA;;;" + service.String() + ")(A;;FR;;;BU)", true},
		"world write":                  {"O:" + service.String() + "D:P(A;;FA;;;" + service.String() + ")(A;;GW;;;WD)", false},
		"domain group write":           {"O:" + service.String() + "D:P(A;;FA;;;" + service.String() + ")(A;;GW;;;S-1-5-21-1-2-3-513)", false},
		"system administration":        {"O:SYD:P(A;;FA;;;" + service.String() + ")(A;;FA;;;SY)(A;;FA;;;BA)", true},
		"untrusted owner":              {"O:WDD:P(A;;FA;;;" + service.String() + ")", false},
		"missing dacl":                 {"O:" + service.String(), false},
	} {
		t.Run(name, func(t *testing.T) {
			descriptor, err := windows.SecurityDescriptorFromString(fixture.sddl)
			if err != nil {
				t.Fatal(err)
			}
			err = validateWindowsSecurityDescriptor(descriptor, service, true)
			if fixture.ok && err != nil {
				t.Fatalf("safe ACL rejected: %v", err)
			}
			if !fixture.ok && err == nil {
				t.Fatal("unsafe ACL accepted")
			}
		})
	}
}

func TestWindowsPathContainedIsCaseInsensitiveAndComponentAware(t *testing.T) {
	for _, test := range []struct {
		root, candidate string
		want            bool
	}{
		{`C:\Workspaces\Project`, `c:\workspaces\project`, true},
		{`C:\Workspaces\Project`, `c:\WORKSPACES\PROJECT\src`, true},
		{`C:\Workspaces\Project`, `C:\Workspaces\Project-old`, false},
		{`C:\Workspaces\Project`, `D:\Workspaces\Project\src`, false},
		{`C:\Workspaces\Project`, `\\server\share\Project`, false},
	} {
		if got := WindowsPathContained(test.root, test.candidate); got != test.want {
			t.Errorf("WindowsPathContained(%q, %q)=%t, want %t", test.root, test.candidate, got, test.want)
		}
	}
}

func TestWindowsWorkspaceAndStateRootsMustRemainDisjoint(t *testing.T) {
	for _, test := range []struct {
		state, workspace string
		wantErr          bool
	}{
		{`C:\ProgramData\Aeontra\state`, `C:\Aeontra\workspaces`, false},
		{`C:\Aeontra\workspaces\state`, `C:\Aeontra\workspaces`, true},
		{`C:\Aeontra`, `C:\Aeontra\workspaces`, true},
		{`C:\Aeontra\workspaces`, `C:\Aeontra\workspaces`, true},
	} {
		err := validateWorkspaceStateSeparation(test.state, WorkspaceRoots{WindowsDev: test.workspace})
		if (err != nil) != test.wantErr {
			t.Errorf("state=%q workspace=%q err=%v wantErr=%t", test.state, test.workspace, err, test.wantErr)
		}
	}
}

func TestOpenWindowsWorkspaceUsesFinalHandleAndRevalidatesIdentity(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "Workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}

	opened, err := OpenWindowsWorkspace(root, strings.ToLower(workspace))
	if err != nil {
		t.Fatal(err)
	}
	if opened.File() == nil || opened.FinalPath() == "" || opened.Identity().VolumeSerialNumber == 0 {
		_ = opened.Close()
		t.Fatalf("workspace did not retain a usable final handle: path=%q identity=%+v", opened.FinalPath(), opened.Identity())
	}
	if err := opened.Revalidate(); err != nil {
		_ = opened.Close()
		t.Fatalf("initial revalidation failed: %v", err)
	}
	if got, err := ValidateWindowsWorkspacePath(root, workspace); err != nil || !strings.EqualFold(got, opened.FinalPath()) {
		_ = opened.Close()
		t.Fatalf("final path=%q err=%v retained=%q", got, err, opened.FinalPath())
	}

	moved := filepath.Join(root, "Workspace-old")
	if err := os.Rename(workspace, moved); err != nil {
		_ = opened.Close()
		t.Fatal(err)
	}
	if err := os.Mkdir(workspace, 0o700); err != nil {
		_ = opened.Close()
		t.Fatal(err)
	}
	if err := opened.Revalidate(); !errors.Is(err, ErrWindowsWorkspaceReplaced) {
		t.Errorf("path replacement was not rejected: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenWindowsWorkspaceRejectsReparsePointComponents(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "link")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("creating a Windows directory symlink requires the test host privilege: %v", err)
	}
	if _, err := OpenWindowsWorkspace(root, link); err == nil {
		t.Fatal("reparse-point workspace accepted")
	}
}

func TestCreateWindowsDirectoryTreeDoesNotWriteThroughReparsePoint(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("creating a Windows directory symlink requires the test host privilege: %v", err)
	}
	if err := createWindowsDirectoryTree(root, filepath.Join(link, "escaped")); err == nil {
		t.Fatal("directory creation accepted a reparse-point parent")
	}
	if _, err := os.Stat(filepath.Join(outside, "escaped")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("directory was created outside the workspace: %v", err)
	}
}
