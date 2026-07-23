package edge

import (
	"errors"
	"strings"
	"time"
)

// ResolveActiveDeviceName maps one human Edge alias to exactly one active device.
// It returns the opaque identity only to trusted server code; public project tools
// never expose it.
func (s *Store) ResolveActiveDeviceName(name string) (Device, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if s == nil || s.db == nil || !namePattern.MatchString(name) {
		return Device{}, errors.New("edge target alias is invalid")
	}
	rows, err := s.db.Query(`SELECT device_id,name,state,paired_at FROM devices WHERE name=? AND state=? ORDER BY paired_at,device_id`, name, StateActive)
	if err != nil {
		return Device{}, errors.New("edge target alias is unavailable")
	}
	defer rows.Close()
	var matches []Device
	for rows.Next() {
		var item Device
		var pairedAt int64
		if err := rows.Scan(&item.ID, &item.Name, &item.State, &pairedAt); err != nil {
			return Device{}, errors.New("edge target alias is unavailable")
		}
		item.PairedAt = time.Unix(pairedAt, 0).UTC()
		matches = append(matches, item)
	}
	if err := rows.Err(); err != nil {
		return Device{}, errors.New("edge target alias is unavailable")
	}
	if len(matches) != 1 {
		return Device{}, errors.New("active edge target alias not found or ambiguous")
	}
	return matches[0], nil
}
