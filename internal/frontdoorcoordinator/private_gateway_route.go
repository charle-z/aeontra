package frontdoorcoordinator

import (
	"context"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
)

const (
	privateRoutePath     = "/proc/net/route"
	privateRouteMaxBytes = int64(64 << 10)
)

func privateCoolifyGatewayFromRoute(data []byte) (net.IP, error) {
	var gateway net.IP
	for index, line := range strings.Split(string(data), "\n") {
		if index == 0 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[1] != "00000000" {
			continue
		}
		flags, err := strconv.ParseUint(fields[3], 16, 16)
		if err != nil || flags&0x3 != 0x3 || len(fields[2]) != 8 {
			return nil, ErrCoolifyPrivateResolve
		}
		raw, err := strconv.ParseUint(fields[2], 16, 32)
		if err != nil {
			return nil, ErrCoolifyPrivateResolve
		}
		candidate := net.IPv4(byte(raw), byte(raw>>8), byte(raw>>16), byte(raw>>24))
		if gateway != nil || !privateCoolifyAddress(candidate) || candidate.IsUnspecified() {
			return nil, ErrCoolifyPrivateResolve
		}
		gateway = candidate
	}
	if gateway == nil {
		return nil, ErrCoolifyPrivateResolve
	}
	return gateway, nil
}

func privateCoolifyRouteGateway() (net.IP, error) {
	file, err := os.Open(privateRoutePath)
	if err != nil {
		return nil, ErrCoolifyPrivateResolve
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, privateRouteMaxBytes+1))
	if err != nil || int64(len(data)) > privateRouteMaxBytes {
		return nil, ErrCoolifyPrivateResolve
	}
	return privateCoolifyGatewayFromRoute(data)
}

func privateCoolifyAddresses(ctx context.Context) ([]net.IPAddr, error) {
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, privateCoolifyHost)
	if err == nil {
		return addresses, nil
	}
	gateway, err := privateCoolifyRouteGateway()
	if err != nil {
		return nil, ErrCoolifyPrivateResolve
	}
	return []net.IPAddr{{IP: gateway}}, nil
}
