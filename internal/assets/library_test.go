package assets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
)

func fixture(t *testing.T) (Entry, []byte) {
	t.Helper()
	var body bytes.Buffer
	im := image.NewRGBA(image.Rect(0, 0, 2, 2))
	im.Set(0, 0, color.White)
	if err := png.Encode(&body, im); err != nil {
		t.Fatal(err)
	}
	return Entry{ID: "sample", Title: "Sample logo", URL: "https://images.example.com/sample.png", SHA256: fmt.Sprintf("%x", sha256.Sum256(body.Bytes())), MIME: "image/png", MaxBytes: 4096, License: "CC0-1.0", LicenseURL: "https://example.com/license", Attribution: "Example author", Provenance: "https://example.com/original", ReviewedBy: "operator", ReviewedAt: "2026-10-07"}, body.Bytes()
}

func libraryFixture(t *testing.T, entry Entry) *Library {
	t.Helper()
	encoded, err := json.Marshal(manifest{Version: 1, Assets: []Entry{entry}})
	if err != nil {
		t.Fatal(err)
	}
	library, err := ParseLibrary(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return library
}

func TestLibraryIsStrictBoundedAndImmutable(t *testing.T) {
	entry, _ := fixture(t)
	library := libraryFixture(t, entry)
	got, err := library.Search("logo", 5)
	if err != nil || len(got) != 1 || got[0].ID != entry.ID {
		t.Fatalf("search=%+v err=%v", got, err)
	}
	got[0].URL = "https://evil.example.com/"
	selected, err := library.Entry(entry.ID)
	if err != nil || selected != entry {
		t.Fatalf("entry mutated: %+v %v", selected, err)
	}
	if _, err := library.Search("", 5); err == nil {
		t.Fatal("empty query accepted")
	}
	if _, err := library.Search("logo", 21); err == nil {
		t.Fatal("unbounded search accepted")
	}
	if _, err := ParseLibrary([]byte(`{"version":1,"assets":[],"unknown":true}`)); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, err := ParseLibrary([]byte(strings.Repeat(" ", MaxLibraryBytes+1))); err == nil {
		t.Fatal("oversized manifest accepted")
	}
	if _, err := library.Entry("../sample"); err == nil {
		t.Fatal("path-shaped ID accepted")
	}
	encoded, _ := json.Marshal(manifest{Version: 1, Assets: []Entry{entry, entry}})
	if _, err := ParseLibrary(encoded); err == nil {
		t.Fatal("duplicate ID accepted")
	}
}

func TestLibraryRejectsUnreviewedOrUnsafeSources(t *testing.T) {
	entry, _ := fixture(t)
	for name, change := range map[string]func(*Entry){
		"http":       func(e *Entry) { e.URL = "http://images.example.com/x" },
		"port":       func(e *Entry) { e.URL = "https://images.example.com:8443/x" },
		"userinfo":   func(e *Entry) { e.URL = "https://user:secret@images.example.com/x" },
		"query":      func(e *Entry) { e.URL += "?token=secret" },
		"fragment":   func(e *Entry) { e.URL += "#fragment" },
		"literal IP": func(e *Entry) { e.URL = "https://127.0.0.1/x" },
		"license":    func(e *Entry) { e.License = "" },
		"review":     func(e *Entry) { e.ReviewedBy = "" },
		"date":       func(e *Entry) { e.ReviewedAt = "tomorrow" },
		"hash":       func(e *Entry) { e.SHA256 = strings.Repeat("A", 64) },
		"size":       func(e *Entry) { e.MaxBytes = MaxAssetBytes + 1 },
		"svg":        func(e *Entry) { e.MIME = "image/svg+xml" },
		"provenance": func(e *Entry) { e.Provenance = "https://user:password@example.com/x" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := entry
			change(&bad)
			encoded, _ := json.Marshal(manifest{Version: 1, Assets: []Entry{bad}})
			if _, err := ParseLibrary(encoded); err == nil {
				t.Fatal("unsafe entry accepted")
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFetchIdentifiesClient(t *testing.T) {
	entry, body := fixture(t)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("User-Agent"); got != "Aeontra/1.0 (https://aeontra.com; asset validation)" {
			t.Fatalf("unidentified asset client: %q", got)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{entry.MIME}}, Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}
	if _, err := fetch(context.Background(), entry, client); err != nil {
		t.Fatal(err)
	}
}

func TestFetchReportsStatusWithoutSourceContent(t *testing.T) {
	entry, _ := fixture(t)
	for _, code := range []int{302, 403, 429, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader("private origin response"))}, nil
			})}
			_, err := fetch(context.Background(), entry, client)
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", code)) {
				t.Fatalf("missing actionable status: %v", err)
			}
			if strings.Contains(err.Error(), entry.URL) || strings.Contains(err.Error(), "private origin response") {
				t.Fatalf("origin content leaked: %v", err)
			}
		})
	}
}

func TestFetchVerifiesActualBytesAndBounds(t *testing.T) {
	entry, body := fixture(t)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Accept-Encoding") != "identity" {
			t.Fatal("credential/compression header")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body))}, nil
	})}
	result, err := fetch(context.Background(), entry, client)
	if err != nil || !bytes.Equal(result.Bytes, body) || result.SHA256 != entry.SHA256 || result.Width != 2 || result.Height != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for name, response := range map[string]*http.Response{
		"redirect":      {StatusCode: 302, Header: http.Header{"Location": []string{"https://127.0.0.1/x"}}, Body: io.NopCloser(strings.NewReader(""))},
		"wrong MIME":    {StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(bytes.NewReader(body))},
		"wrong hash":    {StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(append(body, 0)))},
		"large header":  {StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, ContentLength: 5000, Body: io.NopCloser(bytes.NewReader(body))},
		"large chunked": {StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, ContentLength: -1, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 4097)))},
		"compression":   {StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}, "Content-Encoding": []string{"gzip"}}, Body: io.NopCloser(bytes.NewReader(body))},
	} {
		t.Run(name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response, nil })}
			if _, err := fetch(context.Background(), entry, client); err == nil {
				t.Fatal("unsafe response accepted")
			}
		})
	}
}

func TestFetchRejectsInvalidImageAndExcessivePixels(t *testing.T) {
	entry, _ := fixture(t)
	configuration, format, err := image.DecodeConfig(bytes.NewReader(hugePNGHeader()))
	if err != nil || format != "png" || configuration.Width <= MaxDimension {
		t.Fatalf("dimension fixture invalid: %+v %s %v", configuration, format, err)
	}
	for _, body := range [][]byte{[]byte("not an image"), hugePNGHeader()} {
		entry.SHA256 = fmt.Sprintf("%x", sha256.Sum256(body))
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(body))}, nil
		})}
		if _, err := fetch(context.Background(), entry, client); err == nil {
			t.Fatal("invalid/huge image accepted")
		}
	}
}

func hugePNGHeader() []byte {
	// The parser must reject the dimension budget before attempting pixel decode.
	result := []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 0, 255, 255, 0, 0, 255, 255, 8, 6, 0, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(result[29:], crc32.ChecksumIEEE(result[12:29]))
	return result
}

func TestPublicAddressPolicyRejectsPrivateAndSpecialRanges(t *testing.T) {
	for _, ip := range []string{"0.0.0.0", "10.0.0.1", "100.64.0.1", "127.0.0.1", "169.254.169.254", "172.16.0.1", "192.0.0.1", "192.0.2.1", "192.168.1.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "240.0.0.1", "::1", "fc00::1", "fe80::1", "::ffff:127.0.0.1", "2001:db8::1", "2002:7f00:1::"} {
		if publicAddress(netip.MustParseAddr(ip)) {
			t.Errorf("special address accepted: %s", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !publicAddress(netip.MustParseAddr(ip)) {
			t.Errorf("public address rejected: %s", ip)
		}
	}
}

func TestSecureHTTPClientDisablesRedirectsAndAmbientAuthority(t *testing.T) {
	client := secureHTTPClient()
	transport := client.Transport.(*http.Transport)
	if transport.Proxy != nil || !transport.DisableCompression || client.Jar != nil || client.Timeout <= 0 || transport.MaxResponseHeaderBytes <= 0 {
		t.Fatal("ambient authority or unbounded client")
	}
	if client.CheckRedirect(&http.Request{}, nil) != http.ErrUseLastResponse {
		t.Fatal("redirect enabled")
	}
}

func TestPublicDialPinsAddressAndRejectsMixedDNSAnswers(t *testing.T) {
	lookups, dials := 0, 0
	dial := publicDial(func(_ context.Context, network, host string) ([]netip.Addr, error) {
		lookups++
		if network != "ip" || host != "images.example.com" {
			t.Fatal("unexpected DNS target")
		}
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}, func(_ context.Context, network, address string) (net.Conn, error) {
		dials++
		if network != "tcp" || address != "8.8.8.8:443" {
			t.Fatal("hostname resolved again or IP not pinned")
		}
		return nil, nil
	})
	if _, err := dial(context.Background(), "tcp", "images.example.com:443"); err != nil || lookups != 1 || dials != 1 {
		t.Fatalf("lookup=%d dial=%d err=%v", lookups, dials, err)
	}
	dial = publicDial(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")}, nil
	}, func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("mixed DNS set reached dial")
		return nil, nil
	})
	if _, err := dial(context.Background(), "tcp", "images.example.com:443"); err == nil {
		t.Fatal("mixed DNS set accepted")
	}
	if _, err := dial(context.Background(), "tcp", "images.example.com:8443"); err == nil {
		t.Fatal("port override accepted")
	}
}
