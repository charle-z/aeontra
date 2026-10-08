package edgeclient

import "context"

// LinuxNetworkProbe remains in the common contract because Windows builds retain
// fail-closed HTB broker stubs, while only Unix implementations can satisfy it.
type LinuxNetworkProbe interface {
	InterfaceIPv4(context.Context, string) (string, error)
	RouteInterface(context.Context, string) (string, error)
}
