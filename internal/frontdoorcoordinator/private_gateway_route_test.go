package frontdoorcoordinator

import (
	"errors"
	"net"
	"testing"
)

func TestPrivateCoolifyGatewayFromRoute(t *testing.T) {
	valid := []byte("Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\neth0\t00000000\t0101000A\t0003\t0\t0\t0\t00000000\n")
	gateway, err := privateCoolifyGatewayFromRoute(valid)
	if err != nil {
		t.Fatal(err)
	}
	if !gateway.Equal(net.ParseIP("10.0.1.1")) {
		t.Fatalf("gateway = %v", gateway)
	}
}

func TestPrivateCoolifyGatewayFromRouteFailsClosed(t *testing.T) {
	cases := map[string]string{
		"missing":   "Iface\tDestination\tGateway\tFlags\neth0\t0001000A\t00000000\t0001\n",
		"multiple":  "Iface\tDestination\tGateway\tFlags\neth0\t00000000\t0101000A\t0003\neth1\t00000000\t0102000A\t0003\n",
		"public":    "Iface\tDestination\tGateway\tFlags\neth0\t00000000\t08080808\t0003\n",
		"not-route": "Iface\tDestination\tGateway\tFlags\neth0\t00000000\t0101000A\t0001\n",
		"malformed": "Iface\tDestination\tGateway\tFlags\neth0\t00000000\tNOTHEX00\t0003\n",
	}
	for name, table := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := privateCoolifyGatewayFromRoute([]byte(table)); !errors.Is(err, ErrCoolifyPrivateResolve) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
