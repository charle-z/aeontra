package frontdoorcoordinator

import (
	"context"
	"net"
	"testing"
)

func TestNewClientAllowsOnlyFixedPrivateGatewayHTTP(t *testing.T) {
	t.Parallel()
	if _, err := NewClient(validClientConfig("http+host-gateway://coolify.example:8000")); err != nil {
		t.Fatalf("private host gateway rejected: %v", err)
	}
	for _, raw := range []string{
		"http://coolify.example:8000",
		"http+host-gateway://coolify.example",
		"http+host-gateway://coolify.example:8000/path",
		"http+host-gateway://user@coolify.example:8000",
		"http+host-gateway://coolify.example:8000?x=1",
	} {
		if _, err := NewClient(validClientConfig(raw)); err == nil {
			t.Fatalf("unsafe HTTP origin accepted: %s", raw)
		}
	}
}

func TestPrivateCoolifyAddressPolicyRejectsPublicAndMixedDNS(t *testing.T) {
	t.Parallel()
	private := []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}, {IP: net.ParseIP("10.10.0.1")}, {IP: net.ParseIP("169.254.1.2")}}
	if !privateCoolifyAddressesAllowed(private) {
		t.Fatal("private address set was rejected")
	}
	if privateCoolifyAddressesAllowed(nil) {
		t.Fatal("empty address set was accepted")
	}
	if privateCoolifyAddressesAllowed([]net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}) {
		t.Fatal("public address was accepted")
	}
	if privateCoolifyAddressesAllowed([]net.IPAddr{{IP: net.ParseIP("10.10.0.1")}, {IP: net.ParseIP("8.8.8.8")}}) {
		t.Fatal("mixed private/public DNS answer was accepted")
	}
}

func TestPrivateCoolifyDialRejectsAlternateHostBeforeResolution(t *testing.T) {
	t.Parallel()
	dial := privateCoolifyDialContext("coolify.example:8000")
	if _, err := dial(context.Background(), "tcp", "example.com:8000"); err == nil {
		t.Fatal("alternate host reached private Coolify dialer")
	}
}
