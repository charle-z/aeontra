// Package buildinfo owns the process build identity reported by every transport.
// Values are intentionally centralized so CLI output, MCP initialize, health checks,
// and deployment diagnostics cannot drift.
package buildinfo

const (
	// Version is the human semantic version of the mcp-devbox server.
	Version = "0.2.0"
	// ProtocolVersion is the default MCP protocol version when the client does not
	// request a specific compatible version.
	ProtocolVersion = "2024-11-05"
	// EdgeBundleProtocolVersion is the compatibility contract shared by the
	// packaged Edge, provider, driver and local autopilot worker.
	EdgeBundleProtocolVersion = "mcp-devbox.edge-bundle.v1"
)

// Commit and BuiltAt may be set at link time with -ldflags. They keep safe explicit
// defaults for local builds and can be overridden from deployment environment values
// during startup.
var (
	Commit  = "unknown"
	BuiltAt = "unknown"
	// Edge bundle identity is injected into packaged binaries. Unstamped local
	// builds deliberately cannot validate or run a production Edge bundle.
	EdgeBundleRelease     = "unbundled"
	EdgeBundleCatalogHash = ""
	EdgeBundlePublicKey   = ""
)

// Info is the stable build identity shared by CLI and transports.
type Info struct {
	Version         string `json:"version"`
	ProtocolVersion string `json:"protocol_version"`
	Commit          string `json:"commit"`
	BuiltAt         string `json:"built_at"`
}

// Current returns a value copy of the current process build identity.
func Current() Info {
	return Info{
		Version:         Version,
		ProtocolVersion: ProtocolVersion,
		Commit:          Commit,
		BuiltAt:         BuiltAt,
	}
}
