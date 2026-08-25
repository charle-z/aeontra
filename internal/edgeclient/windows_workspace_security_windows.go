//go:build windows

package edgeclient

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WindowsWorkspaceIdentity identifies the object held by a workspace handle.
// The volume serial and file index are supplied by Windows, rather than being
// inferred from a caller-controlled path.
type WindowsWorkspaceIdentity struct {
	VolumeSerialNumber uint32
	FileIndexHigh      uint32
	FileIndexLow       uint32
	FileAttributes     uint32
	FinalPath          string
}

// WindowsWorkspace owns an opened workspace directory. Callers that are about
// to execute work should retain this object through validation and call
// Revalidate before starting the process. The handle is deliberately kept
// private so callers cannot replace it with a path-derived identity.
type WindowsWorkspace struct {
	file        *os.File
	path        string
	rootPath    string
	identity    WindowsWorkspaceIdentity
	rootObject  WindowsWorkspaceIdentity
	validateACL bool
}

// OpenWindowsWorkcell retains the path identity and enforces the trusted
// Windows-workcell ACL boundary for the current service identity.
func OpenWindowsWorkcell(root, candidate string) (*WindowsWorkspace, error) {
	workspace, err := OpenWindowsWorkspace(root, candidate)
	if err != nil {
		return nil, err
	}
	if err := validateWindowsWorkspaceACL(root, candidate); err != nil {
		_ = workspace.Close()
		return nil, err
	}
	workspace.validateACL = true
	return workspace, nil
}

const (
	windowsFileReadAttributes        = 0x00000080
	windowsFileShareRead             = 0x00000001
	windowsFileShareWrite            = 0x00000002
	windowsFileShareDelete           = 0x00000004
	windowsOpenExisting              = 3
	windowsFileFlagOpenReparsePoint  = 0x00200000
	windowsFileFlagBackupSemantics   = 0x02000000
	windowsFileAttributeDirectory    = 0x00000010
	windowsFileAttributeReparsePoint = 0x00000400
)

var (
	ErrWindowsWorkspaceUnsafe      = errors.New("windows workspace path is unsafe")
	ErrWindowsWorkspaceUnavailable = errors.New("windows workspace path is unavailable")
	ErrWindowsWorkspaceEscaped     = errors.New("windows workspace path escaped its root")
	ErrWindowsWorkspaceReplaced    = errors.New("windows workspace path was replaced")
	ErrWindowsWorkspaceACLUnsafe   = errors.New("windows workspace ACL is unsafe")
)

// IsWindowsLocalPath reports whether path is an absolute path on a local drive.
// Extended, UNC, device, drive-relative and rooted-without-drive forms are
// intentionally excluded because they have namespace and normalization rules
// that are not part of the trusted Windows workcell contract.
func IsWindowsLocalPath(path string) bool {
	return windowsLexicalPathIsSafe(path)
}

func isWindowsLocalPathShape(path string) bool {
	if path == "" || strings.IndexByte(path, 0) >= 0 || len(path) < 3 {
		return false
	}
	if !isWindowsDriveLetter(path[0]) || path[1] != ':' || !isWindowsSeparator(rune(path[2])) {
		return false
	}
	lower := strings.ToLower(path)
	for _, prefix := range []string{`\\`, `//`, `\\?\`, `//?/`, `\\.\`, `//./`, `\device\`, `/device/`} {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}
	return true
}

// WindowsPathContained performs a case-insensitive, component-aware check.
// It is suitable for paths that have already been returned by
// GetFinalPathNameByHandle; it also rejects paths on different local volumes.
func WindowsPathContained(root, candidate string) bool {
	root, okRoot := normalizeWindowsComparablePath(root)
	candidate, okCandidate := normalizeWindowsComparablePath(candidate)
	if !okRoot || !okCandidate || !sameWindowsVolume(root, candidate) {
		return false
	}
	root = trimWindowsTrailingSeparators(root)
	candidate = trimWindowsTrailingSeparators(candidate)
	if strings.EqualFold(root, candidate) {
		return true
	}
	if !strings.HasSuffix(root, `\`) {
		root += `\`
	}
	return len(candidate) > len(root) && strings.EqualFold(candidate[:len(root)], root)
}

// ValidateWindowsWorkspacePath validates and resolves a registered workspace.
// The returned path is the final handle path, not a caller-provided spelling.
// A subsequent execution should use OpenWindowsWorkspace so the validating
// handle remains available for replacement detection.
func ValidateWindowsWorkspacePath(root, candidate string) (string, error) {
	workspace, err := OpenWindowsWorkspace(root, candidate)
	if err != nil {
		return "", err
	}
	defer workspace.Close()
	return workspace.FinalPath(), nil
}

// OpenWindowsWorkspace opens candidate with reparse points treated as objects,
// walks every component under the configured local root, and retains the final
// directory handle. It fails closed if the final handle resolves to another
// volume, outside the root, or a different namespace.
func OpenWindowsWorkspace(root, candidate string) (*WindowsWorkspace, error) {
	if !IsWindowsLocalPath(root) || !IsWindowsLocalPath(candidate) || !windowsLexicalPathIsSafe(root) || !windowsLexicalPathIsSafe(candidate) {
		return nil, ErrWindowsWorkspaceUnsafe
	}
	if !sameWindowsVolume(root, candidate) {
		return nil, ErrWindowsWorkspaceEscaped
	}

	rootHandle, rootIdentity, err := openAndInspectWindowsDirectory(root)
	if err != nil {
		return nil, err
	}
	rootFile := os.NewFile(uintptr(rootHandle), root)
	if rootFile == nil {
		_ = windows.CloseHandle(rootHandle)
		return nil, ErrWindowsWorkspaceUnavailable
	}
	// openAndInspectWindowsDirectory returns a handle with reparse-aware flags;
	// rootFile remains open only while the candidate is opened and compared.

	workspaceHandle, workspaceIdentity, err := openAndInspectWindowsPathUnderRoot(root, candidate)
	if err != nil {
		_ = rootFile.Close()
		return nil, err
	}
	workspaceFile := os.NewFile(uintptr(workspaceHandle), candidate)
	if workspaceFile == nil {
		_ = windows.CloseHandle(workspaceHandle)
		_ = rootFile.Close()
		return nil, ErrWindowsWorkspaceUnavailable
	}
	if rootIdentity.VolumeSerialNumber != workspaceIdentity.VolumeSerialNumber || !WindowsPathContained(rootIdentity.FinalPath, workspaceIdentity.FinalPath) {
		_ = workspaceFile.Close()
		_ = rootFile.Close()
		return nil, ErrWindowsWorkspaceEscaped
	}
	if err := verifyWindowsHandleIdentity(rootFile, rootIdentity); err != nil {
		_ = workspaceFile.Close()
		_ = rootFile.Close()
		return nil, ErrWindowsWorkspaceReplaced
	}
	if err := verifyWindowsHandleIdentity(workspaceFile, workspaceIdentity); err != nil {
		_ = workspaceFile.Close()
		_ = rootFile.Close()
		return nil, ErrWindowsWorkspaceReplaced
	}
	_ = rootFile.Close()
	return &WindowsWorkspace{file: workspaceFile, path: candidate, rootPath: root, identity: workspaceIdentity, rootObject: rootIdentity}, nil
}

// createWindowsDirectoryTree creates missing directory components while a
// no-delete handle keeps each parent from being renamed or replaced. Existing
// components are opened as reparse-point objects and rejected before use.
func createWindowsDirectoryTree(root, candidate string) error {
	normalizedRoot, rootOK := normalizeWindowsComparablePath(root)
	normalizedCandidate, candidateOK := normalizeWindowsComparablePath(candidate)
	if !rootOK || !candidateOK || !WindowsPathContained(normalizedRoot, normalizedCandidate) {
		return ErrWindowsWorkspaceEscaped
	}
	parent, _, err := openAndInspectWindowsDirectoryNoDelete(normalizedRoot)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(parent) }()
	current := trimWindowsTrailingSeparators(normalizedRoot)
	remainder := strings.TrimLeft(normalizedCandidate[len(normalizedRoot):], `\`)
	for _, component := range splitWindowsPath(remainder) {
		next := current + `\` + component
		if err := os.Mkdir(next, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		nextHandle, _, err := openAndInspectWindowsDirectoryNoDelete(next)
		if err != nil {
			return err
		}
		_ = windows.CloseHandle(parent)
		parent = nextHandle
		current = next
	}
	return nil
}

func validateWindowsWorkspaceACL(root, candidate string) error {
	root, rootOK := normalizeWindowsComparablePath(root)
	candidate, candidateOK := normalizeWindowsComparablePath(candidate)
	if !rootOK || !candidateOK || !WindowsPathContained(root, candidate) {
		return fmt.Errorf("%w: containment", ErrWindowsWorkspaceACLUnsafe)
	}
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return fmt.Errorf("%w: token", ErrWindowsWorkspaceACLUnsafe)
	}
	defer token.Close()
	serviceSID, err := windowsWorkspaceWriterSID(token)
	if err != nil {
		return fmt.Errorf("%w: token service SID", ErrWindowsWorkspaceACLUnsafe)
	}
	for index, current := range windowsPathPrefixesFromRoot(root, candidate) {
		handle, _, err := openAndInspectWindowsDirectory(current)
		if err != nil {
			return fmt.Errorf("%w: open ancestor", ErrWindowsWorkspaceACLUnsafe)
		}
		err = validateWindowsDirectoryDescriptor(handle, serviceSID, true)
		_ = windows.CloseHandle(handle)
		if err != nil {
			return fmt.Errorf("%w at ACL component %d", err, index)
		}
	}
	// Prove that the actual service identity can mutate workspace contents. The
	// handle is not retained; OpenWindowsWorkspace retains the identity handle.
	pathPtr, err := windows.UTF16PtrFromString(candidate)
	if err != nil {
		return fmt.Errorf("%w: candidate path", ErrWindowsWorkspaceACLUnsafe)
	}
	const directoryModify = windows.FILE_LIST_DIRECTORY | 0x00000002 | 0x00000004 | windows.FILE_TRAVERSE | 0x00000040 | windowsFileReadAttributes | windows.FILE_WRITE_ATTRIBUTES | windows.READ_CONTROL
	handle, err := windows.CreateFile(pathPtr, directoryModify, windowsFileShareRead|windowsFileShareWrite|windowsFileShareDelete, nil, windowsOpenExisting, windowsFileFlagOpenReparsePoint|windowsFileFlagBackupSemantics, 0)
	if err != nil {
		return fmt.Errorf("%w: service modify access", ErrWindowsWorkspaceACLUnsafe)
	}
	_ = windows.CloseHandle(handle)
	return nil
}

func windowsWorkspaceWriterSID(token windows.Token) (*windows.SID, error) {
	user, err := token.GetTokenUser()
	if err != nil || user == nil || user.User.Sid == nil {
		return nil, errors.New("Windows token user is unavailable")
	}
	groups, err := token.GetTokenGroups()
	if err != nil || groups == nil {
		return nil, errors.New("Windows token groups are unavailable")
	}
	return selectWindowsWorkspaceWriterSID(user.User.Sid, groups.AllGroups())
}

func selectWindowsWorkspaceWriterSID(userSID *windows.SID, groups []windows.SIDAndAttributes) (*windows.SID, error) {
	if userSID == nil {
		return nil, errors.New("Windows token user is unavailable")
	}
	var selected *windows.SID
	sawServiceSID := false
	for _, group := range groups {
		if group.Sid == nil {
			continue
		}
		value := group.Sid.String()
		if value == "S-1-5-80-0" || !strings.HasPrefix(value, "S-1-5-80-") {
			continue
		}
		sawServiceSID = true
		const required = windows.SE_GROUP_ENABLED | windows.SE_GROUP_OWNER
		if group.Attributes&required != required {
			continue
		}
		if selected != nil {
			return nil, errors.New("Windows service SID is ambiguous")
		}
		copySID, err := group.Sid.Copy()
		if err != nil {
			return nil, errors.New("Windows service SID is unavailable")
		}
		selected = copySID
	}
	if selected != nil {
		return selected, nil
	}
	if sawServiceSID {
		return nil, errors.New("Windows service SID is disabled")
	}
	return userSID.Copy()
}

func validateWindowsDirectoryDescriptor(handle windows.Handle, serviceSID *windows.SID, requireOwner bool) error {
	descriptor, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil || descriptor == nil {
		return fmt.Errorf("%w: descriptor", ErrWindowsWorkspaceACLUnsafe)
	}
	return validateWindowsSecurityDescriptor(descriptor, serviceSID, requireOwner)
}

func validateWindowsSecurityDescriptor(descriptor *windows.SECURITY_DESCRIPTOR, serviceSID *windows.SID, requireOwner bool) error {
	if descriptor == nil || serviceSID == nil {
		return fmt.Errorf("%w: owner", ErrWindowsWorkspaceACLUnsafe)
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil {
		return fmt.Errorf("%w: untrusted owner", ErrWindowsWorkspaceACLUnsafe)
	}
	if requireOwner && !owner.Equals(serviceSID) && !owner.IsWellKnown(windows.WinLocalSystemSid) && !owner.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
		return fmt.Errorf("%w: dacl", ErrWindowsWorkspaceACLUnsafe)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		return ErrWindowsWorkspaceACLUnsafe
	}
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil || ace == nil {
			return fmt.Errorf("%w: ace", ErrWindowsWorkspaceACLUnsafe)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
				continue
			}
			return fmt.Errorf("%w: unsupported ace", ErrWindowsWorkspaceACLUnsafe)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if trustedWindowsWriter(sid, serviceSID) {
			continue
		}
		mask := uint32(ace.Mask)
		const untrustedWrite = uint32(windows.GENERIC_ALL | windows.GENERIC_WRITE | windows.DELETE | windows.WRITE_DAC | windows.WRITE_OWNER |
			windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA | windows.FILE_WRITE_EA | windows.FILE_WRITE_ATTRIBUTES | 0x00000004 | 0x00000040)
		if mask&untrustedWrite != 0 {
			return fmt.Errorf("%w: untrusted writer", ErrWindowsWorkspaceACLUnsafe)
		}
	}
	return nil
}

func trustedWindowsWriter(sid, serviceSID *windows.SID) bool {
	return sid != nil && serviceSID != nil && (sid.Equals(serviceSID) || sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid))
}

func windowsPathPrefixesFromRoot(root, candidate string) []string {
	root, rootOK := normalizeWindowsComparablePath(root)
	candidate, candidateOK := normalizeWindowsComparablePath(candidate)
	if !rootOK || !candidateOK || !WindowsPathContained(root, candidate) {
		return nil
	}
	prefixes := []string{root}
	current := trimWindowsTrailingSeparators(root)
	remainder := strings.TrimLeft(candidate[len(root):], `\`)
	for _, part := range splitWindowsPath(remainder) {
		current += `\` + part
		prefixes = append(prefixes, current)
	}
	return prefixes
}

// FinalPath returns the final path captured while opening the workspace.
func (w *WindowsWorkspace) FinalPath() string {
	if w == nil {
		return ""
	}
	return w.identity.FinalPath
}

// Identity returns the immutable identity captured by OpenWindowsWorkspace.
func (w *WindowsWorkspace) Identity() WindowsWorkspaceIdentity {
	if w == nil {
		return WindowsWorkspaceIdentity{}
	}
	return w.identity
}

// Revalidate checks both the retained handle and a fresh handle opened through
// the registered spelling. This catches a directory being renamed, replaced,
// or redirected between registration and execution.
func (w *WindowsWorkspace) Revalidate() error {
	if w == nil || w.file == nil || w.rootPath == "" || w.path == "" {
		return ErrWindowsWorkspaceUnavailable
	}
	current, err := inspectWindowsHandle(w.file)
	if err != nil || !sameWindowsWorkspaceIdentity(current, w.identity) {
		return ErrWindowsWorkspaceReplaced
	}
	if !WindowsPathContained(w.rootObject.FinalPath, current.FinalPath) {
		return ErrWindowsWorkspaceEscaped
	}
	fresh, err := OpenWindowsWorkspace(w.rootPath, w.path)
	if err != nil {
		return err
	}
	defer fresh.Close()
	if !sameWindowsWorkspaceIdentity(fresh.identity, w.identity) {
		return ErrWindowsWorkspaceReplaced
	}
	if w.validateACL {
		if err := validateWindowsWorkspaceACL(w.rootPath, w.path); err != nil {
			return err
		}
	}
	return nil
}

// File returns the retained directory handle for APIs that accept a Windows
// handle. The returned pointer must not be closed independently; close the
// WindowsWorkspace instead.
func (w *WindowsWorkspace) File() *os.File {
	if w == nil {
		return nil
	}
	return w.file
}

// Close releases the retained workspace handle.
func (w *WindowsWorkspace) Close() error {
	if w == nil || w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func openAndInspectWindowsPathUnderRoot(root, candidate string) (windows.Handle, WindowsWorkspaceIdentity, error) {
	normalizedRoot, rootOK := normalizeWindowsComparablePath(root)
	normalizedCandidate, candidateOK := normalizeWindowsComparablePath(candidate)
	if !rootOK || !candidateOK || !WindowsPathContained(normalizedRoot, normalizedCandidate) {
		return 0, WindowsWorkspaceIdentity{}, ErrWindowsWorkspaceEscaped
	}
	remainder := ""
	if len(normalizedCandidate) > len(normalizedRoot) {
		remainder = strings.TrimLeft(normalizedCandidate[len(normalizedRoot):], `\`)
	}
	parts := splitWindowsPath(remainder)
	prefix := trimWindowsTrailingSeparators(normalizedRoot)
	if len(parts) == 0 {
		return openAndInspectWindowsDirectory(prefix)
	}
	var current windows.Handle
	for index, component := range parts {
		prefix += `\` + component
		handle, identity, openErr := openAndInspectWindowsDirectory(prefix)
		if openErr != nil {
			if current != 0 {
				_ = windows.CloseHandle(current)
			}
			return 0, WindowsWorkspaceIdentity{}, openErr
		}
		if current != 0 {
			_ = windows.CloseHandle(current)
		}
		current = handle
		if index != len(parts)-1 && identity.FileAttributes&windowsFileAttributeDirectory == 0 {
			_ = windows.CloseHandle(current)
			return 0, WindowsWorkspaceIdentity{}, ErrWindowsWorkspaceUnavailable
		}
		if index == len(parts)-1 {
			if identity.FileAttributes&windowsFileAttributeDirectory == 0 {
				_ = windows.CloseHandle(current)
				return 0, WindowsWorkspaceIdentity{}, ErrWindowsWorkspaceUnavailable
			}
			return current, identity, nil
		}
	}
	return 0, WindowsWorkspaceIdentity{}, ErrWindowsWorkspaceUnavailable
}

func openAndInspectWindowsDirectory(path string) (windows.Handle, WindowsWorkspaceIdentity, error) {
	return openAndInspectWindowsDirectoryWithShare(path, windowsFileShareRead|windowsFileShareWrite|windowsFileShareDelete)
}

func openAndInspectWindowsDirectoryNoDelete(path string) (windows.Handle, WindowsWorkspaceIdentity, error) {
	return openAndInspectWindowsDirectoryWithShare(path, windowsFileShareRead|windowsFileShareWrite)
}

func openAndInspectWindowsDirectoryWithShare(path string, shareMode uint32) (windows.Handle, WindowsWorkspaceIdentity, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, WindowsWorkspaceIdentity{}, ErrWindowsWorkspaceUnsafe
	}
	handle, err := windows.CreateFile(pathPtr, windowsFileReadAttributes|windows.READ_CONTROL,
		shareMode, nil,
		windowsOpenExisting, windowsFileFlagOpenReparsePoint|windowsFileFlagBackupSemantics, 0)
	if err != nil {
		return 0, WindowsWorkspaceIdentity{}, fmt.Errorf("%w: %v", ErrWindowsWorkspaceUnavailable, err)
	}
	identity, err := inspectWindowsHandleByHandle(handle)
	if err != nil {
		_ = windows.CloseHandle(handle)
		return 0, WindowsWorkspaceIdentity{}, ErrWindowsWorkspaceUnavailable
	}
	if identity.FileAttributes&windowsFileAttributeReparsePoint != 0 {
		_ = windows.CloseHandle(handle)
		return 0, WindowsWorkspaceIdentity{}, ErrWindowsWorkspaceUnsafe
	}
	return handle, identity, nil
}

func inspectWindowsHandle(file *os.File) (WindowsWorkspaceIdentity, error) {
	if file == nil {
		return WindowsWorkspaceIdentity{}, ErrWindowsWorkspaceUnavailable
	}
	return inspectWindowsHandleByHandle(windows.Handle(file.Fd()))
}

func inspectWindowsHandleByHandle(handle windows.Handle) (WindowsWorkspaceIdentity, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return WindowsWorkspaceIdentity{}, err
	}
	finalPath, err := windowsFinalPathByHandle(handle)
	if err != nil {
		return WindowsWorkspaceIdentity{}, err
	}
	finalPath, ok := normalizeWindowsComparablePath(finalPath)
	if !ok {
		return WindowsWorkspaceIdentity{}, ErrWindowsWorkspaceUnsafe
	}
	return WindowsWorkspaceIdentity{VolumeSerialNumber: info.VolumeSerialNumber, FileIndexHigh: info.FileIndexHigh, FileIndexLow: info.FileIndexLow, FileAttributes: info.FileAttributes, FinalPath: finalPath}, nil
}

func windowsFinalPathByHandle(handle windows.Handle) (string, error) {
	buffer := make([]uint16, 256)
	for {
		count, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
		if err != nil {
			return "", err
		}
		if count < uint32(len(buffer)) {
			return windows.UTF16ToString(buffer[:count]), nil
		}
		// The API returns the required character count when the buffer is too
		// small. Leave room for the terminating NUL on the next attempt.
		buffer = make([]uint16, count+1)
	}
}

func verifyWindowsHandleIdentity(file *os.File, expected WindowsWorkspaceIdentity) error {
	actual, err := inspectWindowsHandle(file)
	if err != nil || !sameWindowsWorkspaceIdentity(actual, expected) {
		return ErrWindowsWorkspaceReplaced
	}
	return nil
}

func sameWindowsWorkspaceIdentity(a, b WindowsWorkspaceIdentity) bool {
	return a.VolumeSerialNumber == b.VolumeSerialNumber && a.FileIndexHigh == b.FileIndexHigh && a.FileIndexLow == b.FileIndexLow && a.FileAttributes == b.FileAttributes && strings.EqualFold(a.FinalPath, b.FinalPath)
}

func windowsLexicalPathIsSafe(path string) bool {
	if !isWindowsLocalPathShape(path) || strings.ContainsRune(path, '\x00') {
		return false
	}
	for index, r := range path {
		if r == ':' && index != 1 {
			return false // alternate data stream or another namespace syntax
		}
	}
	for _, component := range splitWindowsPath(path[2:]) {
		if component == "" || component == "." || component == ".." || strings.HasSuffix(component, " ") || strings.HasSuffix(component, ".") {
			return false
		}
		for _, r := range component {
			if unicode.IsControl(r) || strings.ContainsRune(`<>"|?*`, r) {
				return false
			}
		}
	}
	return true
}

func splitWindowsPath(path string) []string {
	return strings.FieldsFunc(path, isWindowsSeparator)
}

func isWindowsSeparator(r rune) bool {
	return r == '\\' || r == '/'
}

func isWindowsDriveLetter(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func normalizeWindowsComparablePath(path string) (string, bool) {
	if strings.HasPrefix(path, `\\?\`) {
		path = path[4:]
	}
	if !IsWindowsLocalPath(path) || !windowsLexicalPathIsSafe(path) {
		return "", false
	}
	path = filepath.Clean(strings.ReplaceAll(path, "/", `\`))
	if len(path) > 3 && strings.HasSuffix(path, `\`) {
		path = strings.TrimRight(path, `\`)
	}
	return path, true
}

func trimWindowsTrailingSeparators(path string) string {
	if len(path) > 3 {
		return strings.TrimRight(path, `\`)
	}
	return path
}

func sameWindowsVolume(a, b string) bool {
	return len(a) >= 2 && len(b) >= 2 && strings.EqualFold(a[:2], b[:2])
}
