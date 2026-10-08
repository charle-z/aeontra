package assets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

const MaxPixels = 16 << 20
const MaxDimension = 4096

type Download struct {
	Entry
	Bytes  []byte `json:"-"`
	Size   int64  `json:"size"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

func (l *Library) Fetch(ctx context.Context, id string) (Download, error) {
	entry, err := l.Entry(id)
	if err != nil {
		return Download{}, err
	}
	return fetch(ctx, entry, secureHTTPClient())
}

func fetch(ctx context.Context, entry Entry, client *http.Client) (Download, error) {
	if ctx == nil || entry.validate() != nil {
		return Download{}, errors.New("asset request is invalid")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, entry.URL, nil)
	if err != nil {
		return Download{}, errors.New("asset request is invalid")
	}
	request.Header.Set("Accept", entry.MIME)
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", "Aeontra/1.0 (https://aeontra.com; asset validation)")
	response, err := client.Do(request)
	if err != nil {
		return Download{}, errors.New("asset download unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Download{}, fmt.Errorf("asset source returned HTTP %d; a direct 200 response is required", response.StatusCode)
	}
	media, params, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != entry.MIME || len(params) != 0 || response.Header.Get("Content-Encoding") != "" || response.ContentLength > entry.MaxBytes {
		return Download{}, errors.New("asset response type or size is invalid")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, entry.MaxBytes+1))
	if err != nil || len(body) == 0 || int64(len(body)) > entry.MaxBytes {
		return Download{}, errors.New("asset response exceeds bounds or is incomplete")
	}
	actualHash := fmt.Sprintf("%x", sha256.Sum256(body))
	if actualHash != entry.SHA256 {
		return Download{}, errors.New("asset content hash mismatch")
	}
	configuration, format, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil || formatMIME(format) != entry.MIME || configuration.Width < 1 || configuration.Height < 1 || configuration.Width > MaxDimension || configuration.Height > MaxDimension || int64(configuration.Width)*int64(configuration.Height) > MaxPixels {
		return Download{}, errors.New("asset raster configuration is invalid or exceeds bounds")
	}
	decoded, actualFormat, err := image.Decode(bytes.NewReader(body))
	if err != nil || formatMIME(actualFormat) != entry.MIME || decoded.Bounds().Dx() != configuration.Width || decoded.Bounds().Dy() != configuration.Height {
		return Download{}, errors.New("asset raster data is invalid")
	}
	return Download{Entry: entry, Bytes: body, Size: int64(len(body)), Width: configuration.Width, Height: configuration.Height}, nil
}

func formatMIME(format string) string {
	switch format {
	case "png":
		return "image/png"
	case "jpeg":
		return "image/jpeg"
	}
	return ""
}

func secureHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		Proxy: nil, DisableCompression: true, DisableKeepAlives: true,
		TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second,
		MaxResponseHeaderBytes: 16 << 10,
		DialContext:            publicDial(net.DefaultResolver.LookupNetIP, dialer.DialContext),
	}
	return &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// Resolve once, reject the entire answer set if any address is private/special,
// then dial a numeric validated address. TLS still verifies the original hostname.
func publicDial(resolve func(context.Context, string, string) ([]netip.Addr, error), dial func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != "443" || net.ParseIP(host) != nil {
			return nil, errors.New("asset destination is invalid")
		}
		addresses, err := resolve(ctx, "ip", host)
		if err != nil || len(addresses) == 0 || len(addresses) > 16 {
			return nil, errors.New("asset DNS unavailable")
		}
		for _, address := range addresses {
			if !publicAddress(address) {
				return nil, errors.New("asset destination is not public")
			}
		}
		return dial(ctx, "tcp", net.JoinHostPort(addresses[0].String(), "443"))
	}
}

var deniedPrefixes = func() []netip.Prefix {
	var result []netip.Prefix
	for _, value := range strings.Fields("0.0.0.0/8 10.0.0.0/8 100.64.0.0/10 127.0.0.0/8 169.254.0.0/16 172.16.0.0/12 192.0.0.0/24 192.0.2.0/24 192.88.99.0/24 192.168.0.0/16 198.18.0.0/15 198.51.100.0/24 203.0.113.0/24 224.0.0.0/4 240.0.0.0/4 2001::/23 2001:db8::/32 2002::/16 3fff::/20") {
		result = append(result, netip.MustParsePrefix(value))
	}
	return result
}()

func publicAddress(address netip.Addr) bool {
	if !address.IsValid() || address.Zone() != "" || address.Is4In6() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	if address.Is6() && !netip.MustParsePrefix("2000::/3").Contains(address) {
		return false
	}
	for _, prefix := range deniedPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}
