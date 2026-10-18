package console

import (
	"errors"
	"strings"
	"time"
	_ "time/tzdata"
)

const (
	DefaultTimezone  = "America/Bogota"
	maxTimezoneBytes = 64
)

// ValidateTimezone accepts explicit IANA location names only. UTC is the sole
// slash-less exception; ambiguous abbreviations and fixed-offset aliases are
// intentionally rejected.
func ValidateTimezone(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		name = DefaultTimezone
	}
	if len([]byte(name)) > maxTimezoneBytes {
		return "", errors.New("console timezone exceeds the allowed length")
	}
	if name != "UTC" && (!strings.Contains(name, "/") || strings.Contains(name, "..")) {
		return "", errors.New("console timezone must be an explicit IANA location")
	}
	if _, err := time.LoadLocation(name); err != nil {
		return "", errors.New("console timezone is not a valid IANA location")
	}
	return name, nil
}
