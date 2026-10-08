// Package assets acquires only operator-reviewed, hash-pinned raster images.
// It has no caller URL, credential, provider, filesystem, or online search API.
package assets

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charle-z/mcp-devbox/internal/policy"
)

const (
	MaxLibraryBytes  = 512 << 10
	MaxAssets        = 256
	MaxAssetBytes    = 8 << 20
	MaxSearchResults = 20
	MaxQueryBytes    = 200
)

type Entry struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	MIME        string `json:"mime"`
	MaxBytes    int64  `json:"max_bytes"`
	License     string `json:"license"`
	LicenseURL  string `json:"license_url"`
	Attribution string `json:"attribution"`
	Provenance  string `json:"provenance"`
	ReviewedBy  string `json:"reviewed_by"`
	ReviewedAt  string `json:"reviewed_at"`
}

type manifest struct {
	Version int     `json:"version"`
	Assets  []Entry `json:"assets"`
}

// Library is an immutable startup snapshot. Search never makes a network request.
type Library struct {
	entries []Entry
	digest  string
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var hostPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$`)

func ParseLibrary(body []byte) (*Library, error) {
	if len(body) == 0 || len(body) > MaxLibraryBytes {
		return nil, errors.New("asset library exceeds bounds")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var payload manifest
	if err := decoder.Decode(&payload); err != nil {
		return nil, errors.New("asset library is invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("asset library must contain one manifest")
	}
	if payload.Version != 1 || len(payload.Assets) == 0 || len(payload.Assets) > MaxAssets {
		return nil, errors.New("asset library version or count is invalid")
	}
	seen := make(map[string]bool, len(payload.Assets))
	for _, entry := range payload.Assets {
		if err := entry.validate(); err != nil || seen[entry.ID] {
			return nil, errors.New("asset library entry is invalid or duplicated")
		}
		seen[entry.ID] = true
	}
	normalized, _ := json.Marshal(payload)
	return &Library{entries: append([]Entry(nil), payload.Assets...), digest: fmt.Sprintf("%x", sha256.Sum256(normalized))}, nil
}

func (l *Library) Digest() string { return l.digest }

func (l *Library) Entry(id string) (Entry, error) {
	if l == nil || !idPattern.MatchString(id) {
		return Entry{}, errors.New("asset is unavailable")
	}
	for _, entry := range l.entries {
		if entry.ID == id {
			return entry, nil
		}
	}
	return Entry{}, errors.New("asset is unavailable")
}

func (l *Library) Search(query string, limit int) ([]Entry, error) {
	query = strings.TrimSpace(query)
	if l == nil || !safeText(query, MaxQueryBytes) {
		return nil, errors.New("asset search query is invalid")
	}
	if limit == 0 {
		limit = 5
	}
	if limit < 1 || limit > MaxSearchResults {
		return nil, errors.New("asset search limit is invalid")
	}
	query = strings.ToLower(query)
	results := make([]Entry, 0, limit)
	for _, entry := range l.entries {
		if strings.Contains(strings.ToLower(entry.ID+" "+entry.Title+" "+entry.Attribution+" "+entry.License), query) {
			results = append(results, entry)
			if len(results) == limit {
				break
			}
		}
	}
	return results, nil
}

func (e Entry) validate() error {
	if !idPattern.MatchString(e.ID) || !safeText(e.ID, 64) || !hashPattern.MatchString(e.SHA256) || (e.MIME != "image/png" && e.MIME != "image/jpeg") || e.MaxBytes < 1 || e.MaxBytes > MaxAssetBytes {
		return errors.New("asset identity is invalid")
	}
	for _, value := range []string{e.URL, e.LicenseURL, e.Provenance} {
		if _, err := credentialFreeHTTPS(value); err != nil {
			return err
		}
	}
	if !safeText(e.Title, 160) || !safeText(e.License, 80) || !safeText(e.Attribution, 500) || !safeText(e.ReviewedBy, 100) {
		return errors.New("asset rights review is incomplete")
	}
	if date, err := time.Parse("2006-01-02", e.ReviewedAt); err != nil || date.Format("2006-01-02") != e.ReviewedAt {
		return errors.New("asset rights review date is invalid")
	}
	return nil
}

func safeText(value string, max int) bool {
	if strings.TrimSpace(value) == "" || len(value) > max || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	_, changed := policy.Redact(value)
	return !changed
}

func credentialFreeHTTPS(value string) (*url.URL, error) {
	if _, changed := policy.Redact(value); changed {
		return nil, errors.New("asset source contains secret-shaped content")
	}
	u, err := url.Parse(value)
	if err != nil || len(value) > 2048 || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Hostname() == "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return nil, errors.New("asset source must be credential-free HTTPS on port 443")
	}
	host := strings.ToLower(u.Hostname())
	if !hostPattern.MatchString(host) || !strings.Contains(host, ".") || strings.Contains(host, "..") || strings.HasSuffix(host, ".") || net.ParseIP(host) != nil {
		return nil, errors.New("asset source hostname is invalid")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return nil, errors.New("asset source hostname is invalid")
		}
	}
	if u.RawPath != "" || strings.ContainsAny(u.Path, "\x00\r\n\\") {
		return nil, errors.New("asset source path is invalid")
	}
	return u, nil
}
