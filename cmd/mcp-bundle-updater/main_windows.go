//go:build windows

// The Windows updater is a narrow privileged adapter. Release identity and
// component layout come from the signed channel and manifest; the operator can
// select only status, stable update, or rollback.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
	"github.com/charle-z/mcp-devbox/internal/bundle"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	windowsServiceName     = "AeontraEdge"
	windowsServiceIdentity = `NT SERVICE\AeontraEdge`
	windowsStableURL       = "https://github.com/charle-z/aeontra/releases/download/stable/channel-windows-amd64.json"
	windowsReleaseBaseURL  = "https://github.com/charle-z/aeontra/releases/download"
	maxChannelBytes        = 64 << 10
	maxArchiveBytes        = 512 << 20
	maxArchiveMemberBytes  = 256 << 20
	maxArchiveTotalBytes   = 512 << 20
	windowsTransactionFile = "update-transaction.json"
)

const (
	transactionPrepared       = "prepared"
	transactionMarkersWritten = "markers_written"
	transactionServiceStarted = "service_started"
)

type windowsServiceConfig struct {
	Version         int    `json:"version"`
	Service         string `json:"service"`
	ServiceIdentity string `json:"service_identity"`
	StateRoot       string `json:"state_root"`
	WorkspaceRoot   string `json:"workspace_root"`
}

type windowsActiveMarker struct {
	Version int    `json:"version"`
	Release string `json:"release"`
	Commit  string `json:"commit"`
	Path    string `json:"path"`
}

type windowsUpdateTransaction struct {
	Version        int                 `json:"version"`
	Operation      string              `json:"operation"`
	Phase          string              `json:"phase"`
	OldActive      windowsActiveMarker `json:"old_active"`
	OldPrevious    windowsActiveMarker `json:"old_previous"`
	TargetActive   windowsActiveMarker `json:"target_active"`
	TargetPrevious windowsActiveMarker `json:"target_previous"`
}

type windowsStatus struct {
	Release         string `json:"release"`
	PreviousRelease string `json:"previous_release,omitempty"`
	Commit          string `json:"commit,omitempty"`
	ServiceActive   bool   `json:"service_active"`
}

type windowsServiceController interface {
	SwapAndStart(oldBinary, newBinary string, config windowsServiceConfig) error
	Verify(binary string, config windowsServiceConfig) error
	Active() bool
}

type scmServiceController struct{}

type windowsUpdater struct {
	installRoot string
	publicKey   ed25519.PublicKey
	client      *http.Client
	service     windowsServiceController
}

var windowsUpdaterExecutable = os.Executable

func main() { os.Exit(runWindows(os.Args[1:])) }

func runWindows(args []string) int {
	if !windows.GetCurrentProcessToken().IsElevated() {
		fmt.Fprintln(os.Stderr, "mcp-bundle-updater must run elevated")
		return 1
	}
	operation, err := parseWindowsUpdaterOperation(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	key, err := compiledWindowsPublicKey()
	if err != nil {
		fmt.Fprintln(os.Stderr, bundle.ManifestInvalid)
		return 1
	}
	installRoot, stateRoot, err := managedWindowsRoots()
	if err != nil {
		fmt.Fprintln(os.Stderr, "managed Windows Edge roots are invalid")
		return 1
	}
	config, err := readWindowsServiceConfig(filepath.Join(installRoot, "service-config.json"), stateRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Windows Edge service configuration is invalid")
		return 1
	}
	updater := &windowsUpdater{installRoot: installRoot, publicKey: key, client: &http.Client{Timeout: 2 * time.Minute}, service: scmServiceController{}}
	var releaseLock func()
	if operation != "status" {
		releaseLock, err = acquireWindowsUpdateLock()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer releaseLock()
	}
	var status windowsStatus
	switch operation {
	case "status":
		status, err = updater.status(config)
	case "update":
		status, err = updater.updateStable(context.Background(), config)
	case "rollback":
		status, err = updater.rollback(config)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	encoded, _ := json.Marshal(status)
	fmt.Println(string(encoded))
	return 0
}

// The updater is a single-writer transaction. A named mutex also covers two
// elevated invocations started by SCM recovery or an operator at the same time.
func acquireWindowsUpdateLock() (func(), error) {
	name, err := windows.UTF16PtrFromString(`Global\AeontraEdgeBundleUpdater`)
	if err != nil {
		return nil, errors.New("updater lock name is invalid")
	}
	handle, createErr := windows.CreateMutex(nil, false, name)
	if createErr != nil && !errors.Is(createErr, windows.ERROR_ALREADY_EXISTS) {
		return nil, errors.New("updater lock unavailable")
	}
	result, waitErr := windows.WaitForSingleObject(handle, 0)
	if waitErr != nil || (result != windows.WAIT_OBJECT_0 && result != windows.WAIT_ABANDONED) {
		_ = windows.CloseHandle(handle)
		return nil, errors.New("another updater operation is active")
	}
	return func() {
		_ = windows.ReleaseMutex(handle)
		_ = windows.CloseHandle(handle)
	}, nil
}

func parseWindowsUpdaterOperation(args []string) (string, error) {
	switch {
	case len(args) == 1 && (args[0] == "status" || args[0] == "rollback"):
		return args[0], nil
	case len(args) == 2 && args[0] == "update" && args[1] == "stable":
		return "update", nil
	default:
		return "", errors.New("accepted operations are status, update stable, or rollback")
	}
}

func compiledWindowsPublicKey() (ed25519.PublicKey, error) {
	key, err := hex.DecodeString(buildinfo.EdgeBundlePublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, errors.New("invalid compiled public key")
	}
	return ed25519.PublicKey(key), nil
}

func managedWindowsRoots() (installRoot, stateRoot string, err error) {
	// The immutable updater is inside <install>/releases/<release>/bin. Derive
	// the selected installation from that signed binary instead of trusting a
	// caller-controlled ProgramFiles environment variable.
	executable, err := windowsUpdaterExecutable()
	if err != nil {
		return "", "", errors.New("managed Windows updater path unavailable")
	}
	installRoot, err = windowsInstallRootFromUpdater(executable)
	if err != nil {
		return "", "", err
	}
	programData, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return "", "", errors.New("managed ProgramData root unavailable")
	}
	stateRoot, err = cleanLocalPath(filepath.Join(programData, "Aeontra", "Edge"))
	if err != nil || pathsOverlap(installRoot, stateRoot) {
		return "", "", errors.New("managed Windows roots overlap")
	}
	if err := ensureWindowsPathNoReparse(installRoot); err != nil {
		return "", "", errors.New("managed install root is unsafe")
	}
	if err := ensureWindowsPathNoReparse(stateRoot); err != nil {
		return "", "", errors.New("managed state root is unsafe")
	}
	return installRoot, stateRoot, nil
}

func windowsInstallRootFromUpdater(executable string) (string, error) {
	binary, err := cleanLocalPath(executable)
	if err != nil || !strings.EqualFold(filepath.Base(binary), "mcp-bundle-updater.exe") {
		return "", errors.New("managed Windows updater path is invalid")
	}
	binRoot := filepath.Dir(binary)
	releaseRoot := filepath.Dir(binRoot)
	releasesRoot := filepath.Dir(releaseRoot)
	installRoot := filepath.Dir(releasesRoot)
	if !strings.EqualFold(filepath.Base(binRoot), "bin") ||
		!strings.EqualFold(filepath.Base(releasesRoot), "releases") ||
		!strings.EqualFold(filepath.Base(installRoot), "Edge") ||
		!strings.EqualFold(filepath.Base(filepath.Dir(installRoot)), "Aeontra") {
		return "", errors.New("managed Windows updater layout is invalid")
	}
	return cleanLocalPath(installRoot)
}

func cleanLocalPath(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", errors.New("empty managed path")
	}
	full, err := filepath.Abs(value)
	if err != nil || !filepath.IsAbs(full) || filepath.VolumeName(full) == "" {
		return "", errors.New("managed path is not local absolute")
	}
	if strings.HasPrefix(full, `\\`) || strings.HasPrefix(full, `\\?\`) || strings.HasPrefix(full, `\\.\`) {
		return "", errors.New("managed path is not local")
	}
	root := filepath.VolumeName(full) + string(filepath.Separator)
	if strings.EqualFold(strings.TrimRight(full, `\`), strings.TrimRight(root, `\`)) {
		return "", errors.New("managed path is a volume root")
	}
	return strings.TrimRight(filepath.Clean(full), `\`), nil
}

func pathsOverlap(left, right string) bool {
	l, r := strings.TrimRight(left, `\`), strings.TrimRight(right, `\`)
	lowerL, lowerR := strings.ToLower(l), strings.ToLower(r)
	return strings.EqualFold(l, r) || strings.HasPrefix(lowerL, lowerR+`\`) || strings.HasPrefix(lowerR, lowerL+`\`)
}

func readWindowsServiceConfig(filename, stateRoot string) (windowsServiceConfig, error) {
	if err := ensureWindowsPathNoReparse(filename); err != nil {
		return windowsServiceConfig{}, errors.New("service configuration is unsafe")
	}
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() || isWindowsReparse(filename) || info.Size() > 16<<10 {
		return windowsServiceConfig{}, errors.New("service configuration unavailable")
	}
	content, err := os.ReadFile(filename)
	if err != nil {
		return windowsServiceConfig{}, err
	}
	var config windowsServiceConfig
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil || decoder.Decode(&struct{}{}) != io.EOF || config.Version != 1 || config.Service != windowsServiceName || config.ServiceIdentity != windowsServiceIdentity {
		return windowsServiceConfig{}, errors.New("service configuration identity is invalid")
	}
	if !strings.EqualFold(config.StateRoot, stateRoot) {
		return windowsServiceConfig{}, errors.New("service state root is not managed")
	}
	workspace, err := cleanLocalPath(config.WorkspaceRoot)
	if err != nil || pathsOverlap(config.StateRoot, workspace) {
		return windowsServiceConfig{}, errors.New("service workspace root is invalid")
	}
	config.StateRoot, config.WorkspaceRoot = stateRoot, workspace
	return config, nil
}

func (u *windowsUpdater) status(config windowsServiceConfig) (windowsStatus, error) {
	if _, err := readWindowsTransaction(filepath.Join(u.installRoot, windowsTransactionFile), u.installRoot); err == nil {
		return windowsStatus{}, errors.New("Windows update transaction requires recovery")
	} else if !errors.Is(err, os.ErrNotExist) {
		return windowsStatus{}, err
	}
	active, previous, err := u.readMarkers()
	if err != nil {
		return windowsStatus{}, err
	}
	manifest, err := u.verifyInstalledRelease(active.Release)
	if err != nil || manifest.Commit != active.Commit {
		return windowsStatus{}, errors.New("active signed release identity is invalid")
	}
	activeBinary := filepath.Join(u.installRoot, "releases", active.Release, "bin", "mcp-edge.exe")
	if err := u.service.Verify(activeBinary, config); err != nil {
		return windowsStatus{}, errors.New("active Windows Edge service identity is invalid")
	}
	return windowsStatus{Release: active.Release, PreviousRelease: previous.Release, Commit: active.Commit, ServiceActive: true}, nil
}

func (u *windowsUpdater) updateStable(ctx context.Context, config windowsServiceConfig) (windowsStatus, error) {
	if err := u.recoverPendingTransaction(config); err != nil {
		return windowsStatus{}, err
	}
	channel, archive, err := u.fetchStable(ctx)
	if err != nil {
		return windowsStatus{}, err
	}
	if err := verifyDigest(archive, channel.ArchiveHash); err != nil {
		return windowsStatus{}, err
	}
	// Stage below the protected installation root. The user TEMP directory is
	// not an acceptable trust boundary for a privileged updater.
	staging, err := os.MkdirTemp(u.installRoot, ".aeontra-edge-update-")
	if err != nil {
		return windowsStatus{}, errors.New("update staging unavailable")
	}
	defer os.RemoveAll(staging)
	if err := extractWindowsArchive(archive, staging); err != nil {
		return windowsStatus{}, err
	}
	expected := bundle.Compatibility{Release: channel.Release, Commit: channel.Commit, ProtocolVersion: channel.ProtocolVersion, CatalogHash: channel.CatalogHash, Architecture: channel.Architecture, Platform: channel.Platform}
	manifest, err := bundle.LoadTrustedManifest(staging, u.publicKey)
	if err != nil || manifest.Release != expected.Release || manifest.Commit != expected.Commit {
		return windowsStatus{}, &bundle.VerificationError{Code: bundle.BundleMismatch}
	}
	if err := installWindowsRelease(u.installRoot, staging, expected, u.publicKey); err != nil {
		return windowsStatus{}, err
	}
	active, previous, err := u.readMarkers()
	if err != nil {
		return windowsStatus{}, err
	}
	if active.Release == channel.Release {
		return u.status(config)
	}
	newBinary := filepath.Join(u.installRoot, "releases", channel.Release, "bin", "mcp-edge.exe")
	oldBinary := filepath.Join(u.installRoot, "releases", active.Release, "bin", "mcp-edge.exe")
	newMarker := windowsActiveMarker{Version: 1, Release: channel.Release, Commit: channel.Commit, Path: filepath.Join(u.installRoot, "releases", channel.Release)}
	tx := windowsUpdateTransaction{
		Version: 1, Operation: "update", Phase: transactionPrepared,
		OldActive: active, OldPrevious: previous, TargetActive: newMarker, TargetPrevious: active,
	}
	if err := u.commitWindowsTransaction(tx, oldBinary, newBinary, config); err != nil {
		return windowsStatus{}, err
	}
	return u.status(config)
}

func (u *windowsUpdater) rollback(config windowsServiceConfig) (windowsStatus, error) {
	if err := u.recoverPendingTransaction(config); err != nil {
		return windowsStatus{}, err
	}
	active, previous, err := u.readMarkers()
	if err != nil || previous.Release == "" {
		return windowsStatus{}, errors.New("previous signed release is unavailable")
	}
	manifest, err := u.verifyInstalledRelease(previous.Release)
	if err != nil || manifest.Commit != previous.Commit {
		return windowsStatus{}, errors.New("previous signed release identity is invalid")
	}
	oldBinary := filepath.Join(u.installRoot, "releases", active.Release, "bin", "mcp-edge.exe")
	newBinary := filepath.Join(u.installRoot, "releases", previous.Release, "bin", "mcp-edge.exe")
	tx := windowsUpdateTransaction{
		Version: 1, Operation: "rollback", Phase: transactionPrepared,
		OldActive: active, OldPrevious: previous, TargetActive: previous, TargetPrevious: active,
	}
	if err := u.commitWindowsTransaction(tx, oldBinary, newBinary, config); err != nil {
		return windowsStatus{}, err
	}
	return u.status(config)
}

func (u *windowsUpdater) commitWindowsTransaction(tx windowsUpdateTransaction, oldBinary, newBinary string, config windowsServiceConfig) error {
	if err := validateWindowsTransaction(tx, u.installRoot); err != nil {
		return err
	}
	if err := writeWindowsTransaction(filepath.Join(u.installRoot, windowsTransactionFile), tx, u.installRoot); err != nil {
		return err
	}
	if err := u.writeTransactionMarkers(tx.TargetActive, tx.TargetPrevious); err != nil {
		return err
	}
	tx.Phase = transactionMarkersWritten
	if err := writeWindowsTransaction(filepath.Join(u.installRoot, windowsTransactionFile), tx, u.installRoot); err != nil {
		return errors.New("Windows update transaction phase could not be persisted")
	}
	if err := u.service.SwapAndStart(oldBinary, newBinary, config); err != nil {
		return u.restoreWindowsTransaction(tx, oldBinary, newBinary, config, err)
	}
	if err := u.service.Verify(newBinary, config); err != nil {
		return u.restoreWindowsTransaction(tx, oldBinary, newBinary, config, errors.New("new Windows Edge service failed final verification"))
	}
	tx.Phase = transactionServiceStarted
	if err := writeWindowsTransaction(filepath.Join(u.installRoot, windowsTransactionFile), tx, u.installRoot); err != nil {
		return errors.New("Windows update transaction completion could not be persisted")
	}
	if err := clearWindowsTransaction(filepath.Join(u.installRoot, windowsTransactionFile), u.installRoot); err != nil {
		return errors.New("Windows update transaction cleanup failed")
	}
	return nil
}

func (u *windowsUpdater) restoreWindowsTransaction(tx windowsUpdateTransaction, oldBinary, newBinary string, config windowsServiceConfig, cause error) error {
	if err := u.service.Verify(oldBinary, config); err != nil {
		_ = u.service.SwapAndStart(newBinary, oldBinary, config)
	}
	if err := u.service.Verify(oldBinary, config); err != nil {
		return errors.New("Windows update failed and recovery remains pending")
	}
	if err := u.writeTransactionMarkers(tx.OldActive, tx.OldPrevious); err != nil {
		return errors.New("Windows update failed and marker restoration failed")
	}
	if err := clearWindowsTransaction(filepath.Join(u.installRoot, windowsTransactionFile), u.installRoot); err != nil {
		return errors.New("Windows update failed and transaction cleanup failed")
	}
	return cause
}

func (u *windowsUpdater) recoverPendingTransaction(config windowsServiceConfig) error {
	filename := filepath.Join(u.installRoot, windowsTransactionFile)
	tx, err := readWindowsTransaction(filename, u.installRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("Windows update transaction is invalid")
	}
	oldBinary := filepath.Join(u.installRoot, "releases", tx.OldActive.Release, "bin", "mcp-edge.exe")
	newBinary := filepath.Join(u.installRoot, "releases", tx.TargetActive.Release, "bin", "mcp-edge.exe")
	if err := u.service.Verify(newBinary, config); err == nil {
		if err := u.writeTransactionMarkers(tx.TargetActive, tx.TargetPrevious); err != nil {
			return errors.New("Windows update recovery could not restore target markers")
		}
		if err := clearWindowsTransaction(filename, u.installRoot); err != nil {
			return errors.New("Windows update recovery cleanup failed")
		}
		return nil
	}
	if err := u.service.Verify(oldBinary, config); err == nil {
		if err := u.writeTransactionMarkers(tx.OldActive, tx.OldPrevious); err != nil {
			return errors.New("Windows update recovery could not restore previous markers")
		}
		if err := clearWindowsTransaction(filename, u.installRoot); err != nil {
			return errors.New("Windows update recovery cleanup failed")
		}
		return nil
	}
	return errors.New("Windows update transaction requires service recovery")
}

func (u *windowsUpdater) writeTransactionMarkers(active, previous windowsActiveMarker) error {
	if err := u.writeActive(active); err != nil {
		return err
	}
	if previous.Release == "" {
		if err := u.clearPrevious(); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return u.writePrevious(previous)
}

func (u *windowsUpdater) fetchStable(ctx context.Context) (bundle.Channel, []byte, error) {
	channelBytes, err := boundedGET(ctx, u.client, windowsStableURL, maxChannelBytes)
	if err != nil {
		return bundle.Channel{}, nil, errors.New("stable Windows channel unavailable")
	}
	signature, err := boundedGET(ctx, u.client, windowsStableURL+".sig", ed25519.SignatureSize)
	if err != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(u.publicKey, channelBytes, signature) {
		return bundle.Channel{}, nil, &bundle.VerificationError{Code: bundle.ManifestInvalid}
	}
	var channel bundle.Channel
	decoder := json.NewDecoder(bytes.NewReader(channelBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&channel); err != nil {
		return bundle.Channel{}, nil, &bundle.VerificationError{Code: bundle.ManifestInvalid}
	}
	canonical, err := bundle.CanonicalChannel(channel)
	if err != nil || !bytes.Equal(canonical, channelBytes) || channel.Version != 2 || channel.Platform != "windows" || channel.Architecture != runtime.GOARCH || channel.ProtocolVersion != buildinfo.EdgeBundleProtocolVersion {
		return bundle.Channel{}, nil, &bundle.VerificationError{Code: bundle.ManifestInvalid}
	}
	archiveURL := windowsReleaseBaseURL + "/" + channel.Release + "/mcp-devbox-edge_" + channel.Release + "_windows_amd64.zip"
	archive, err := boundedGET(ctx, u.client, archiveURL, maxArchiveBytes)
	if err != nil {
		return bundle.Channel{}, nil, errors.New("Windows release archive unavailable")
	}
	return channel, archive, nil
}

func boundedGET(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("release request rejected")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("release response exceeds bound")
	}
	return data, nil
}

func verifyDigest(content []byte, expected string) error {
	if !strings.HasPrefix(expected, "sha256:") || len(expected) != len("sha256:")+64 {
		return &bundle.VerificationError{Code: bundle.BundleMismatch}
	}
	sum := sha256.Sum256(content)
	if !strings.EqualFold(expected, "sha256:"+hex.EncodeToString(sum[:])) {
		return &bundle.VerificationError{Code: bundle.BundleMismatch}
	}
	return nil
}

func extractWindowsArchive(content []byte, destination string) error {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return &bundle.VerificationError{Code: bundle.BundleMismatch}
	}
	allowed := map[string]struct{}{bundle.ManifestFile: {}, bundle.SignatureFile: {}}
	for _, relative := range bundle.WindowsLayout() {
		allowed[filepath.ToSlash(relative)] = struct{}{}
	}
	allowedDirectories := map[string]struct{}{"bin": {}}
	seen := map[string]struct{}{}
	var total int64
	for _, entry := range reader.File {
		name := strings.TrimSuffix(entry.Name, "/")
		if name == "" || strings.Contains(name, `\`) || strings.HasPrefix(name, "/") || path.Clean(name) != name || strings.HasPrefix(name, "../") || name == ".." {
			return &bundle.VerificationError{Code: bundle.BundleMismatch}
		}
		if entry.FileInfo().IsDir() {
			if _, ok := allowedDirectories[name]; !ok {
				return &bundle.VerificationError{Code: bundle.BundleMismatch}
			}
			continue
		}
		if entry.FileInfo().Mode()&os.ModeSymlink != 0 {
			return &bundle.VerificationError{Code: bundle.BundleMismatch}
		}
		if _, ok := allowed[name]; !ok || entry.UncompressedSize64 > maxArchiveMemberBytes {
			return &bundle.VerificationError{Code: bundle.BundleMismatch}
		}
		if _, ok := seen[name]; ok {
			return &bundle.VerificationError{Code: bundle.BundleMismatch}
		}
		seen[name] = struct{}{}
		total += int64(entry.UncompressedSize64)
		if total > maxArchiveTotalBytes {
			return &bundle.VerificationError{Code: bundle.BundleMismatch}
		}
		output, err := windowsArchiveDestination(destination, name)
		if err != nil {
			return &bundle.VerificationError{Code: bundle.BundleMismatch}
		}
		if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil || hasReparseComponent(destination, output) {
			return &bundle.VerificationError{Code: bundle.BundleMismatch}
		}
		input, err := entry.Open()
		if err != nil {
			return &bundle.VerificationError{Code: bundle.BundleMismatch}
		}
		file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			input.Close()
			return &bundle.VerificationError{Code: bundle.BundleMismatch}
		}
		_, copyErr := io.CopyN(file, input, int64(entry.UncompressedSize64))
		closeErr := file.Close()
		input.Close()
		if copyErr != nil || closeErr != nil {
			return &bundle.VerificationError{Code: bundle.BundleMismatch}
		}
	}
	if len(seen) != len(allowed) {
		return &bundle.VerificationError{Code: bundle.BundleMismatch}
	}
	return nil
}

func windowsArchiveDestination(root, name string) (string, error) {
	cleanRoot, err := cleanLocalPath(root)
	if err != nil {
		return "", err
	}
	full := filepath.Join(cleanRoot, filepath.FromSlash(name))
	rel, err := filepath.Rel(cleanRoot, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("archive path escapes staging")
	}
	return full, nil
}

func installWindowsRelease(root, source string, expected bundle.Compatibility, publicKey ed25519.PublicKey) error {
	if err := ensureWindowsPathNoReparse(root); err != nil {
		return errors.New("managed install root is unsafe")
	}
	if _, err := bundle.LoadAndVerify(source, publicKey, expected); err != nil {
		return err
	}
	releases := filepath.Join(root, "releases")
	if err := ensureWindowsPathNoReparse(releases); err != nil {
		return errors.New("release directory is unsafe")
	}
	if err := os.MkdirAll(releases, 0o755); err != nil {
		return errors.New("release directory unavailable")
	}
	target := filepath.Join(releases, expected.Release)
	if err := ensureWindowsPathNoReparse(target); err != nil {
		return errors.New("release path is unsafe")
	}
	if info, err := os.Lstat(target); err == nil {
		if !info.IsDir() || isWindowsReparse(target) {
			return errors.New("existing release path is unsafe")
		}
		if _, err := bundle.LoadAndVerify(target, publicKey, expected); err != nil {
			return errors.New("existing release identity differs")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("release path unavailable")
	}
	temporary := filepath.Join(releases, ".staging-"+expected.Release+"-"+fmt.Sprint(os.Getpid()))
	if err := ensureWindowsPathNoReparse(temporary); err != nil {
		return errors.New("release staging path is unsafe")
	}
	_ = os.RemoveAll(temporary)
	if err := copyWindowsBundle(source, temporary); err != nil {
		_ = os.RemoveAll(temporary)
		return err
	}
	if _, err := bundle.LoadAndVerify(temporary, publicKey, expected); err != nil {
		_ = os.RemoveAll(temporary)
		return err
	}
	if err := os.Rename(temporary, target); err != nil {
		_ = os.RemoveAll(temporary)
		return errors.New("release activation failed")
	}
	return nil
}

func copyWindowsBundle(source, destination string) error {
	if err := ensureWindowsPathNoReparse(source); err != nil {
		return errors.New("signed release source is unsafe")
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return errors.New("release staging unavailable")
	}
	if err := ensureWindowsPathNoReparse(destination); err != nil {
		return errors.New("release staging directory is unsafe")
	}
	files := map[string]struct{}{bundle.ManifestFile: {}, bundle.SignatureFile: {}}
	for _, relative := range bundle.WindowsLayout() {
		files[relative] = struct{}{}
	}
	for relative := range files {
		from, to := filepath.Join(source, filepath.FromSlash(relative)), filepath.Join(destination, filepath.FromSlash(relative))
		info, err := os.Lstat(from)
		if err != nil || !info.Mode().IsRegular() || isWindowsReparse(from) {
			return errors.New("signed release contains an unsafe component")
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil || hasReparseComponent(destination, to) {
			return errors.New("release staging directory is unsafe")
		}
		input, err := os.Open(from)
		if err != nil {
			return errors.New("release component unavailable")
		}
		output, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
		if err != nil {
			input.Close()
			return errors.New("release component staging failed")
		}
		_, copyErr := io.Copy(output, input)
		closeErr := output.Close()
		input.Close()
		if copyErr != nil || closeErr != nil {
			return errors.New("release component staging failed")
		}
	}
	return nil
}

func (u *windowsUpdater) verifyInstalledRelease(release string) (bundle.Manifest, error) {
	if !bundle.ValidRelease(release) {
		return bundle.Manifest{}, errors.New("release name is invalid")
	}
	root := filepath.Join(u.installRoot, "releases", release)
	if !withinRoot(u.installRoot, root) {
		return bundle.Manifest{}, errors.New("installed release path is unsafe")
	}
	if err := ensureWindowsPathNoReparse(root); err != nil {
		return bundle.Manifest{}, errors.New("installed release path is unsafe")
	}
	return bundle.LoadTrustedManifest(root, u.publicKey)
}

func withinRoot(root, child string) bool {
	rel, err := filepath.Rel(root, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func hasReparseComponent(root, target string) bool {
	root, target = filepath.Clean(root), filepath.Clean(target)
	return !withinRoot(root, target) || ensureWindowsPathNoReparse(target) != nil
}

func ensureWindowsPathNoReparse(value string) error {
	clean, err := cleanLocalPath(value)
	if err != nil {
		return err
	}
	for current := clean; ; {
		info, statErr := os.Lstat(current)
		if statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return errors.New("managed path contains a symlink")
			}
			pointer, pointerErr := windows.UTF16PtrFromString(current)
			if pointerErr != nil {
				return pointerErr
			}
			attributes, attributeErr := windows.GetFileAttributes(pointer)
			if attributeErr != nil {
				return attributeErr
			}
			if attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
				return errors.New("managed path contains a reparse point")
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return nil
}

func isWindowsReparse(name string) bool {
	pointer, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return true
	}
	attributes, err := windows.GetFileAttributes(pointer)
	return err == nil && attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func (u *windowsUpdater) readMarkers() (windowsActiveMarker, windowsActiveMarker, error) {
	active, err := readWindowsMarker(filepath.Join(u.installRoot, "active.json"), u.installRoot)
	if err != nil {
		return windowsActiveMarker{}, windowsActiveMarker{}, err
	}
	previous, err := readWindowsMarker(filepath.Join(u.installRoot, "previous.json"), u.installRoot)
	if errors.Is(err, os.ErrNotExist) {
		return active, windowsActiveMarker{}, nil
	}
	if err != nil {
		return windowsActiveMarker{}, windowsActiveMarker{}, err
	}
	return active, previous, nil
}

func readWindowsMarker(filename, root string) (windowsActiveMarker, error) {
	if err := ensureWindowsPathNoReparse(filename); err != nil {
		return windowsActiveMarker{}, err
	}
	info, err := os.Lstat(filename)
	if err != nil {
		return windowsActiveMarker{}, err
	}
	if !info.Mode().IsRegular() || isWindowsReparse(filename) || info.Size() > 16<<10 {
		return windowsActiveMarker{}, errors.New("release marker is unsafe")
	}
	content, err := os.ReadFile(filename)
	if err != nil {
		return windowsActiveMarker{}, err
	}
	var marker windowsActiveMarker
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&marker); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return windowsActiveMarker{}, errors.New("release marker is invalid")
	}
	if err := validateWindowsMarker(marker, root, true); err != nil {
		return windowsActiveMarker{}, err
	}
	marker.Path, _ = cleanLocalPath(marker.Path)
	return marker, nil
}

func validateWindowsMarker(marker windowsActiveMarker, root string, requireCanonical bool) error {
	if marker.Version != 1 || !bundle.ValidRelease(marker.Release) || !validWindowsCommit(marker.Commit) {
		return errors.New("release marker is invalid")
	}
	markerPath, err := cleanLocalPath(marker.Path)
	if err != nil || !withinRoot(root, markerPath) || filepath.Base(markerPath) != marker.Release {
		return errors.New("release marker path is invalid")
	}
	if requireCanonical && !strings.EqualFold(markerPath, filepath.Join(root, "releases", marker.Release)) {
		return errors.New("release marker path is not canonical")
	}
	if err := ensureWindowsPathNoReparse(markerPath); err != nil {
		return errors.New("release marker path is unsafe")
	}
	return nil
}

func validWindowsCommit(commit string) bool {
	if len(commit) != 40 {
		return false
	}
	for _, character := range commit {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	return true
}

func (u *windowsUpdater) writeActive(marker windowsActiveMarker) error {
	if err := validateWindowsMarker(marker, u.installRoot, true); err != nil {
		return err
	}
	return writeWindowsMarker(filepath.Join(u.installRoot, "active.json"), marker)
}

func (u *windowsUpdater) writePrevious(marker windowsActiveMarker) error {
	if err := validateWindowsMarker(marker, u.installRoot, true); err != nil {
		return err
	}
	return writeWindowsMarker(filepath.Join(u.installRoot, "previous.json"), marker)
}

func (u *windowsUpdater) clearPrevious() error {
	if err := ensureWindowsPathNoReparse(filepath.Join(u.installRoot, "previous.json")); err != nil {
		return err
	}
	return os.Remove(filepath.Join(u.installRoot, "previous.json"))
}

func validateWindowsTransaction(tx windowsUpdateTransaction, root string) error {
	if tx.Version != 1 || (tx.Operation != "update" && tx.Operation != "rollback") ||
		(tx.Phase != transactionPrepared && tx.Phase != transactionMarkersWritten && tx.Phase != transactionServiceStarted) {
		return errors.New("Windows update transaction is invalid")
	}
	if err := validateWindowsMarker(tx.OldActive, root, false); err != nil {
		return err
	}
	if tx.OldPrevious.Release != "" {
		if err := validateWindowsMarker(tx.OldPrevious, root, false); err != nil {
			return err
		}
	}
	if err := validateWindowsMarker(tx.TargetActive, root, false); err != nil {
		return err
	}
	if tx.TargetPrevious.Release != "" {
		if err := validateWindowsMarker(tx.TargetPrevious, root, false); err != nil {
			return err
		}
	}
	return nil
}

func readWindowsTransaction(filename, root string) (windowsUpdateTransaction, error) {
	if err := ensureWindowsPathNoReparse(filename); err != nil {
		return windowsUpdateTransaction{}, err
	}
	info, err := os.Lstat(filename)
	if err != nil {
		return windowsUpdateTransaction{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return windowsUpdateTransaction{}, errors.New("Windows update transaction is unsafe")
	}
	content, err := os.ReadFile(filename)
	if err != nil {
		return windowsUpdateTransaction{}, err
	}
	var tx windowsUpdateTransaction
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&tx); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return windowsUpdateTransaction{}, errors.New("Windows update transaction is invalid")
	}
	if err := validateWindowsTransaction(tx, root); err != nil {
		return windowsUpdateTransaction{}, err
	}
	return tx, nil
}

func writeWindowsTransaction(filename string, tx windowsUpdateTransaction, root string) error {
	if err := validateWindowsTransaction(tx, root); err != nil {
		return err
	}
	if err := ensureWindowsPathNoReparse(filename); err != nil {
		return err
	}
	content, err := json.Marshal(tx)
	if err != nil {
		return err
	}
	temporary := filename + ".next"
	if err := ensureWindowsPathNoReparse(temporary); err != nil {
		return err
	}
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return errors.New("Windows update transaction staging failed")
	}
	if _, err := file.Write(append(content, '\n')); err != nil || file.Sync() != nil || file.Close() != nil {
		_ = file.Close()
		_ = os.Remove(temporary)
		return errors.New("Windows update transaction staging failed")
	}
	if err := os.Rename(temporary, filename); err != nil {
		_ = os.Remove(temporary)
		return errors.New("Windows update transaction activation failed")
	}
	return nil
}

func clearWindowsTransaction(filename, root string) error {
	if err := ensureWindowsPathNoReparse(filename); err != nil {
		return err
	}
	err := os.Remove(filename)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func writeWindowsMarker(filename string, marker windowsActiveMarker) error {
	if err := ensureWindowsPathNoReparse(filename); err != nil {
		return err
	}
	content, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	temporary := filename + ".next"
	if err := ensureWindowsPathNoReparse(temporary); err != nil {
		return err
	}
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return errors.New("release marker staging failed")
	}
	if _, err := file.Write(append(content, '\n')); err != nil || file.Sync() != nil || file.Close() != nil {
		_ = file.Close()
		_ = os.Remove(temporary)
		return errors.New("release marker staging failed")
	}
	if err := os.Rename(temporary, filename); err != nil {
		_ = os.Remove(temporary)
		return errors.New("release marker activation failed")
	}
	return nil
}

func (s scmServiceController) Active() bool {
	manager, err := mgr.Connect()
	if err != nil {
		return false
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(windowsServiceName)
	if err != nil {
		return false
	}
	defer service.Close()
	status, err := service.Query()
	return err == nil && status.State == svc.Running
}

func (s scmServiceController) Verify(binary string, config windowsServiceConfig) error {
	if err := validateServiceBinary(binary); err != nil {
		return err
	}
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(windowsServiceName)
	if err != nil {
		return err
	}
	defer service.Close()
	serviceConfig, err := service.Config()
	if err != nil || serviceConfig.ServiceStartName != windowsServiceIdentity {
		return errors.New("Windows Edge service identity is invalid")
	}
	if serviceConfig.BinaryPathName != windowsServiceCommand(binary, config) {
		return errors.New("Windows Edge service binary is not the requested release")
	}
	status, err := service.Query()
	if err != nil || status.State != svc.Running {
		return errors.New("Windows Edge service is not running")
	}
	return nil
}

func (s scmServiceController) SwapAndStart(oldBinary, newBinary string, config windowsServiceConfig) error {
	if err := validateServiceBinary(oldBinary); err != nil {
		return err
	}
	if err := validateServiceBinary(newBinary); err != nil {
		return err
	}
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(windowsServiceName)
	if err != nil {
		return err
	}
	defer service.Close()
	serviceConfig, err := service.Config()
	if err != nil || serviceConfig.ServiceStartName != windowsServiceIdentity {
		return errors.New("Windows Edge service identity is invalid")
	}
	oldConfig := serviceConfig
	if err := stopWindowsService(service); err != nil {
		return err
	}
	serviceConfig.BinaryPathName = windowsServiceCommand(newBinary, config)
	if err := service.UpdateConfig(serviceConfig); err != nil {
		_ = service.Start()
		_ = waitWindowsService(service, svc.Running, 30*time.Second)
		return err
	}
	if err := service.Start(); err != nil || !waitWindowsService(service, svc.Running, 30*time.Second) {
		_ = service.UpdateConfig(oldConfig)
		_ = service.Start()
		_ = waitWindowsService(service, svc.Running, 30*time.Second)
		return errors.New("new Windows Edge service failed to start")
	}
	_ = oldBinary // retained in the transaction signature for rollback diagnostics
	return nil
}

func validateServiceBinary(binary string) error {
	if err := ensureWindowsPathNoReparse(binary); err != nil {
		return errors.New("service binary is unsafe")
	}
	info, err := os.Lstat(binary)
	if err != nil || !info.Mode().IsRegular() || isWindowsReparse(binary) {
		return errors.New("service binary is unsafe")
	}
	return nil
}

func windowsServiceCommand(binary string, config windowsServiceConfig) string {
	return `"` + binary + `" windows-agent --state "` + config.StateRoot + `" --root "` + config.WorkspaceRoot + `" --service-identity "` + windowsServiceIdentity + `" --pair-request "` + filepath.Join(config.StateRoot, "pair-request.json") + `"`
}

func stopWindowsService(service *mgr.Service) error {
	status, err := service.Query()
	if err != nil {
		return err
	}
	if status.State == svc.Stopped {
		return nil
	}
	_, err = service.Control(svc.Stop)
	if err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
		return err
	}
	if !waitWindowsService(service, svc.Stopped, 30*time.Second) {
		return errors.New("Windows Edge service did not stop")
	}
	return nil
}

func waitWindowsService(service *mgr.Service, state svc.State, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := service.Query()
		if err == nil && status.State == state {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}
