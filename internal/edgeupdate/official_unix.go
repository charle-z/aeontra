//go:build !windows

package edgeupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
	"github.com/charle-z/mcp-devbox/internal/bundle"
)

const OfficialBaseURL = "https://github.com/charle-z/mcp-devbox/releases/download"

type OfficialResolver struct {
	PublicKey ed25519.PublicKey
	Client    *http.Client
}

func (r OfficialResolver) UpdateStable(ctx context.Context, engine Engine) (Status, error) {
	channel, client, err := r.stableChannel(ctx)
	if err != nil {
		return Status{}, err
	}
	archiveURL := OfficialBaseURL + "/" + channel.Release + "/mcp-devbox-edge_" + channel.Release + "_" + channel.Architecture + ".tar.gz"
	archive, err := getBounded(ctx, client, archiveURL, 512<<20)
	if err != nil || digestBytes(archive) != channel.ArchiveHash {
		return Status{}, &bundle.VerificationError{Code: bundle.BundleMismatch}
	}
	staging, err := os.MkdirTemp("", "mcp-devbox-official-release-")
	if err != nil {
		return Status{}, errors.New("official release staging unavailable")
	}
	defer os.RemoveAll(staging)
	if err := extractOfficialArchive(archive, staging); err != nil {
		return Status{}, err
	}
	return engine.Install(staging, bundle.Compatibility{
		Release: channel.Release, Commit: channel.Commit, ProtocolVersion: channel.ProtocolVersion,
		CatalogHash: channel.CatalogHash, Architecture: channel.Architecture,
	})
}

func (r OfficialResolver) StableAvailable(ctx context.Context, currentRelease string) (bool, error) {
	channel, _, err := r.stableChannel(ctx)
	if err != nil {
		return false, err
	}
	return channel.Release != currentRelease, nil
}

func (r OfficialResolver) stableChannel(ctx context.Context) (bundle.Channel, *http.Client, error) {
	if len(r.PublicKey) != ed25519.PublicKeySize {
		return bundle.Channel{}, nil, &bundle.VerificationError{Code: bundle.ManifestInvalid}
	}
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	channelPath := OfficialBaseURL + "/stable/channel-" + runtime.GOARCH + ".json"
	channelBytes, err := getBounded(ctx, client, channelPath, 64<<10)
	if err != nil {
		return bundle.Channel{}, nil, errors.New("official release channel unavailable")
	}
	signature, err := getBounded(ctx, client, channelPath+".sig", ed25519.SignatureSize)
	if err != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(r.PublicKey, channelBytes, signature) {
		return bundle.Channel{}, nil, &bundle.VerificationError{Code: bundle.ManifestInvalid}
	}
	var channel bundle.Channel
	decoder := json.NewDecoder(strings.NewReader(string(channelBytes)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&channel) != nil {
		return bundle.Channel{}, nil, &bundle.VerificationError{Code: bundle.ManifestInvalid}
	}
	canonical, canonicalErr := bundle.CanonicalChannel(channel)
	if canonicalErr != nil || !bytes.Equal(canonical, channelBytes) ||
		channel.Architecture != runtime.GOARCH || channel.ProtocolVersion != buildinfo.EdgeBundleProtocolVersion {
		return bundle.Channel{}, nil, &bundle.VerificationError{Code: bundle.ManifestInvalid}
	}
	return channel, client, nil
}

func getBounded(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/octet-stream")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("official release request rejected")
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(content)) > limit {
		return nil, errors.New("official release response is invalid")
	}
	return content, nil
}

func extractOfficialArchive(content []byte, destination string) error {
	gzipReader, err := gzip.NewReader(bytes.NewReader(content))
	if err != nil {
		return &bundle.VerificationError{Code: bundle.BundleMismatch}
	}
	defer gzipReader.Close()
	allowed := map[string]struct{}{bundle.ManifestFile: {}, bundle.SignatureFile: {}}
	for _, relative := range bundle.DefaultLayout() {
		allowed[filepath.ToSlash(relative)] = struct{}{}
	}
	seen := map[string]struct{}{}
	reader := tar.NewReader(gzipReader)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return &bundle.VerificationError{Code: bundle.BundleMismatch}
		}
		name := filepath.ToSlash(filepath.Clean(header.Name))
		if name == "." || strings.HasPrefix(name, "../") || filepath.IsAbs(header.Name) {
			return &bundle.VerificationError{Code: bundle.BundleMismatch}
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size < 0 || header.Size > 256<<20 {
			return &bundle.VerificationError{Code: bundle.BundleMismatch}
		}
		if _, ok := allowed[name]; !ok {
			return &bundle.VerificationError{Code: bundle.BundleMismatch}
		}
		if _, duplicate := seen[name]; duplicate {
			return &bundle.VerificationError{Code: bundle.BundleMismatch}
		}
		seen[name] = struct{}{}
		path := filepath.Join(destination, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return errors.New("official release staging failed")
		}
		mode := os.FileMode(0o600)
		if header.Mode&0o111 != 0 {
			mode = 0o700
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			return errors.New("official release staging failed")
		}
		_, copyErr := io.CopyN(file, reader, header.Size)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil {
			return errors.New("official release staging failed")
		}
	}
	if len(seen) != len(allowed) {
		return &bundle.VerificationError{Code: bundle.BundleMismatch}
	}
	return nil
}

func digestBytes(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}
