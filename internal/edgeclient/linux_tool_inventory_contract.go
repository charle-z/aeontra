package edgeclient

type LinuxToolInventoryEntry struct {
	Name       string `json:"name"`
	Available  bool   `json:"available"`
	Version    string `json:"version"`
	Capability string `json:"capability"`
}
