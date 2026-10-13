package edge

import (
	"database/sql"
	"errors"
	"time"
)

// Contact is evidence of authenticated transport, not host power or readiness.
const DeviceContactWindow = 90 * time.Second

type DeviceConnectivity struct {
	State            string `json:"state"`
	LastContactAt    string `json:"last_contact_at,omitempty"`
	ObservedAt       string `json:"observed_at"`
	FreshnessSeconds int    `json:"freshness_seconds"`
}

func (s *Store) DeviceConnectivity(deviceID string) (DeviceConnectivity, error) {
	if !idPattern.MatchString(deviceID) {
		return DeviceConnectivity{}, errors.New("device id is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var last sql.NullInt64
	err := s.db.QueryRow(`SELECT c.last_contact_at FROM devices d LEFT JOIN edge_device_contact c ON c.device_id=d.device_id WHERE d.device_id=? AND d.state=?`, deviceID, StateActive).Scan(&last)
	if err != nil {
		return DeviceConnectivity{}, errors.New("active edge contact unavailable")
	}
	now := s.now().UTC()
	view := DeviceConnectivity{State: "unknown", ObservedAt: now.Format(time.RFC3339), FreshnessSeconds: int(DeviceContactWindow / time.Second)}
	if last.Valid {
		at := time.Unix(last.Int64, 0).UTC()
		view.LastContactAt = at.Format(time.RFC3339)
		if !at.After(now) {
			view.State = "no_recent_contact"
			if now.Sub(at) <= DeviceContactWindow {
				view.State = "recent_contact"
			}
		}
	}
	return view, nil
}
