package mcpserver

// HTTPOptions contains additive transport options that do not change the MCP wire
// contract. Zero values preserve the existing handler behavior.
type HTTPOptions struct {
	ConsoleSecureCookies bool
}
