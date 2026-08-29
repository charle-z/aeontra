package edgeclient

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

const (
	identityFile   = "identity.json"
	privateKeyFile = "device.key"
	clientTimeout  = 30 * time.Second
)

type Identity struct {
	SchemaVersion    int    `json:"schema_version"`
	ServerURL        string `json:"server_url"`
	DeviceID         string `json:"device_id"`
	Name             string `json:"name"`
	ControlPublicKey string `json:"control_public_key,omitempty"`
}

type PairOptions struct {
	ServerURL  string
	Code       string
	Name       string
	StateRoot  string
	HTTPClient *http.Client
}

func Pair(ctx context.Context, opts PairOptions) (Identity, error) {
	serverURL, err := validateServerURL(opts.ServerURL)
	if err != nil {
		return Identity{}, err
	}
	if err := preparePrivateRoot(opts.StateRoot); err != nil {
		return Identity{}, err
	}
	for _, name := range []string{identityFile, privateKeyFile} {
		if _, err := os.Lstat(filepath.Join(opts.StateRoot, name)); err == nil {
			return Identity{}, errors.New("edge device is already paired")
		} else if !errors.Is(err, os.ErrNotExist) {
			return Identity{}, errors.New("edge state unavailable")
		}
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Identity{}, errors.New("device key generation failed")
	}
	body, _ := json.Marshal(map[string]string{
		"code":       strings.TrimSpace(opts.Code),
		"name":       strings.TrimSpace(opts.Name),
		"public_key": edge.EncodePublicKey(publicKey),
	})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL+edge.PairPath, bytes.NewReader(body))
	if err != nil {
		return Identity{}, errors.New("pairing request failed")
	}
	request.Header.Set("Content-Type", "application/json")
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: clientTimeout}
	}
	response, err := client.Do(request)
	if err != nil {
		return Identity{}, errors.New("pairing endpoint unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return Identity{}, fmt.Errorf("pairing rejected with HTTP %d", response.StatusCode)
	}
	var device edge.Device
	decoder := json.NewDecoder(io.LimitReader(response.Body, 4<<10))
	decoder.DisallowUnknownFields()
	if err := decodeSingleJSON(decoder, &device); err != nil || !deviceIDPattern.MatchString(device.ID) || !deviceNamePattern.MatchString(device.Name) || device.State != edge.StateActive {
		return Identity{}, errors.New("invalid pairing response")
	}
	if controlKey, keyErr := edge.DecodePublicKey(device.ControlPublicKey); keyErr != nil || len(controlKey) != ed25519.PublicKeySize {
		return Identity{}, errors.New("invalid pairing response")
	}
	identity := Identity{SchemaVersion: 2, ServerURL: serverURL, DeviceID: device.ID, Name: device.Name, ControlPublicKey: device.ControlPublicKey}
	if err := persistIdentity(opts.StateRoot, identity, privateKey); err != nil {
		return Identity{}, err
	}
	return identity, nil
}

func LoadIdentity(root string) (Identity, ed25519.PrivateKey, error) {
	if err := validatePrivateRoot(root); err != nil {
		return Identity{}, nil, err
	}
	identityPath := filepath.Join(root, identityFile)
	keyPath := filepath.Join(root, privateKeyFile)
	if err := requirePrivateRegularFile(identityPath); err != nil {
		return Identity{}, nil, err
	}
	if err := requirePrivateRegularFile(keyPath); err != nil {
		return Identity{}, nil, err
	}
	// Native Windows services created before operator ACL support retain a
	// safe service-and-SYSTEM DACL. Reconcile those files while the service
	// identity still owns them so elevated operator diagnostics remain usable.
	if err := reconcilePrivateRegularFilePlatform(identityPath); err != nil {
		return Identity{}, nil, errors.New("edge identity permissions unavailable")
	}
	if err := reconcilePrivateRegularFilePlatform(keyPath); err != nil {
		return Identity{}, nil, errors.New("device key permissions unavailable")
	}
	return loadIdentityContents(root)
}

func loadIdentityContents(root string) (Identity, ed25519.PrivateKey, error) {
	identityPath := filepath.Join(root, identityFile)
	keyPath := filepath.Join(root, privateKeyFile)
	identityBytes, err := os.ReadFile(identityPath)
	if err != nil || len(identityBytes) > 4<<10 {
		return Identity{}, nil, errors.New("edge identity unavailable")
	}
	var identity Identity
	decoder := json.NewDecoder(bytes.NewReader(identityBytes))
	decoder.DisallowUnknownFields()
	if err := decodeSingleJSON(decoder, &identity); err != nil || (identity.SchemaVersion != 1 && identity.SchemaVersion != 2) {
		return Identity{}, nil, errors.New("edge identity is invalid")
	}
	_, controlKeyErr := edge.DecodePublicKey(identity.ControlPublicKey)
	if _, err := validateServerURL(identity.ServerURL); err != nil || !deviceIDPattern.MatchString(identity.DeviceID) || !deviceNamePattern.MatchString(identity.Name) || (identity.SchemaVersion == 1 && identity.ControlPublicKey != "") || (identity.SchemaVersion == 2 && controlKeyErr != nil) {
		return Identity{}, nil, errors.New("edge identity is invalid")
	}
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil || len(keyBytes) > 256 {
		return Identity{}, nil, errors.New("device key unavailable")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(keyBytes)))
	if err != nil || len(decoded) != ed25519.PrivateKeySize {
		return Identity{}, nil, errors.New("device key is invalid")
	}
	return identity, ed25519.PrivateKey(decoded), nil
}

func persistIdentityOnly(root string, identity Identity) error {
	identityBytes, _ := json.Marshal(identity)
	if err := writePrivateAtomic(filepath.Join(root, identityFile), append(identityBytes, '\n')); err != nil {
		return errors.New("identity persistence failed")
	}
	return nil
}

func persistIdentity(root string, identity Identity, privateKey ed25519.PrivateKey) error {
	identityBytes, _ := json.Marshal(identity)
	identityBytes = append(identityBytes, '\n')
	if err := writePrivateAtomic(filepath.Join(root, identityFile), identityBytes); err != nil {
		return errors.New("identity persistence failed")
	}
	keyBytes := []byte(base64.RawURLEncoding.EncodeToString(privateKey) + "\n")
	if err := writePrivateAtomic(filepath.Join(root, privateKeyFile), keyBytes); err != nil {
		_ = os.Remove(filepath.Join(root, identityFile))
		return errors.New("device key persistence failed")
	}
	return nil
}

func NormalizeServerURL(raw string) (string, error) {
	return validateServerURL(raw)
}

func validateServerURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("edge server must be an HTTPS origin")
	}
	return strings.TrimSuffix(parsed.String(), "/"), nil
}

func preparePrivateRoot(root string) error {
	root = filepath.Clean(strings.TrimSpace(root))
	if !filepath.IsAbs(root) {
		return errors.New("edge state root must be absolute")
	}
	if err := rejectSymlinkPath(root); err != nil {
		return err
	}
	_, statErr := os.Lstat(root)
	created := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !created {
		return errors.New("edge state root unavailable")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return errors.New("edge state root unavailable")
	}
	if err := securePrivateRoot(root, created); err != nil {
		return errors.New("edge state permissions failed")
	}
	return validatePrivateRoot(root)
}

func validatePrivateRoot(root string) error {
	root = filepath.Clean(strings.TrimSpace(root))
	if !filepath.IsAbs(root) {
		return errors.New("edge state root must be absolute")
	}
	if err := rejectSymlinkPath(root); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("edge state root is unsafe")
	}
	return validatePrivateRootPlatform(root, info)
}

func rejectSymlinkPath(path string) error {
	current := filepath.Clean(path)
	for {
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return errors.New("edge path contains a symlink")
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.New("edge path unavailable")
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

func requirePrivateRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("edge private file is unsafe")
	}
	return validatePrivateFilePlatform(path, info)
}

// ValidatePrivateRegularFile exposes the same platform-specific private-file
// validation used by the Edge's durable stores. Windows callers use this
// after creating an inherited state file so a plan cannot silently move to a
// broader ACL; Unix callers retain the existing mode/ownership checks.
func ValidatePrivateRegularFile(path string) error {
	return requirePrivateRegularFile(path)
}

// SecurePrivateOpenFile applies the platform-native private-file policy to an
// already-open regular file and verifies the resulting descriptor.
func SecurePrivateOpenFile(file *os.File) error {
	if file == nil {
		return errors.New("edge private file is unavailable")
	}
	if err := securePrivateFile(file); err != nil {
		return err
	}
	return ValidatePrivateOpenFile(file)
}

// ValidatePrivateOpenFile verifies permissions or ACLs through the retained
// file descriptor so a path replacement cannot change the object being checked.
func ValidatePrivateOpenFile(file *os.File) error {
	if file == nil {
		return errors.New("edge private file is unavailable")
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("edge private file is unsafe")
	}
	return validatePrivateOpenFilePlatform(file, info)
}

// PreparePrivateRoot creates or validates an administrator-owned private root
// using the platform's native ownership and ACL rules. It is exported for
// platform-specific durable stores that live below the Edge state root.
func PreparePrivateRoot(path string) error {
	return preparePrivateRoot(path)
}

func securePrivateRegularPath(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	secureErr := securePrivateFile(file)
	closeErr := file.Close()
	if secureErr != nil {
		return secureErr
	}
	if closeErr != nil {
		return closeErr
	}
	return requirePrivateRegularFile(path)
}

func writePrivateAtomic(path string, content []byte) error {
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return errors.New("unsafe destination")
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".edge-private-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := securePrivateFile(temporary); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return err
	}
	return requirePrivateRegularFile(path)
}

func decodeSingleJSON(decoder *json.Decoder, output any) error {
	if err := decoder.Decode(output); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON value")
	}
	return nil
}

var deviceIDPattern = regexp.MustCompile(`^ed_[a-f0-9]{32}$`)
var deviceNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
