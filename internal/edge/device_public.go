package edge

// DeviceActive reports whether one opaque Edge identity exists and has not been revoked.
// It exposes no device name, key material, pairing metadata, or network information.
func (s *Store) DeviceActive(deviceID string) bool {
	return s != nil && s.deviceActive(deviceID)
}
