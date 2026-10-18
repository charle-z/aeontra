package console

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

const (
	preferencesPath    = "/console/preferences"
	maxPreferencesBody = 1024
)

type preferencesResponse struct {
	Timezone string `json:"timezone"`
}

func (h *Handler) handlePreferences(w http.ResponseWriter, r *http.Request) {
	rawSession, ok := h.validSessionCookie(r)
	if !ok {
		writeUnauthorized(w)
		return
	}
	switch r.Method {
	case http.MethodGet:
		timezone, ok := h.sessions.Timezone(rawSession, h.defaultTimezone)
		if !ok {
			writeUnauthorized(w)
			return
		}
		writePreferences(w, timezone)
	case http.MethodPut:
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeGenericError(w, http.StatusUnsupportedMediaType)
			return
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPreferencesBody))
		decoder.DisallowUnknownFields()
		var request preferencesResponse
		if err := decoder.Decode(&request); err != nil {
			writeGenericError(w, http.StatusBadRequest)
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			writeGenericError(w, http.StatusBadRequest)
			return
		}
		timezone, err := ValidateTimezone(request.Timezone)
		if err != nil || strings.TrimSpace(request.Timezone) == "" {
			writeGenericError(w, http.StatusBadRequest)
			return
		}
		if err := h.sessions.SetTimezone(rawSession, timezone); err != nil {
			writeGenericError(w, http.StatusServiceUnavailable)
			return
		}
		writePreferences(w, timezone)
	default:
		methodNotAllowed(w, "GET, PUT")
	}
}

func writePreferences(w http.ResponseWriter, timezone string) {
	hardenResponse(w, "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(preferencesResponse{Timezone: timezone})
}

func (h *Handler) validSessionCookie(r *http.Request) (string, bool) {
	if h == nil || h.sessions == nil || r == nil {
		return "", false
	}
	cookie, err := r.Cookie(cookieName)
	if err != nil || !h.sessions.Valid(cookie.Value) {
		return "", false
	}
	return cookie.Value, true
}

func (s *SessionStore) Timezone(raw, defaultTimezone string) (string, bool) {
	if s == nil || s.db == nil || raw == "" {
		return "", false
	}
	digest := sha256.Sum256([]byte(raw))
	now := s.now().UTC().UnixNano()
	var timezone sql.NullString
	var expires int64
	var revoked sql.NullInt64
	err := s.db.QueryRow(`SELECT p.timezone,s.expires_at,s.revoked_at
		FROM console_sessions s LEFT JOIN console_preferences p ON p.digest=s.digest
		WHERE s.digest=?`, digest[:]).Scan(&timezone, &expires, &revoked)
	if err != nil || revoked.Valid || expires <= now {
		return "", false
	}
	if timezone.Valid {
		return timezone.String, true
	}
	return defaultTimezone, true
}

func (s *SessionStore) SetTimezone(raw, timezone string) error {
	if s == nil || s.db == nil || raw == "" {
		return errors.New("console preference store is unavailable")
	}
	validated, err := ValidateTimezone(timezone)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(raw))
	now := s.now().UTC().UnixNano()
	tx, err := s.db.Begin()
	if err != nil {
		return errors.New("console preference transaction failed")
	}
	defer tx.Rollback()
	var expires int64
	var revoked sql.NullInt64
	if err := tx.QueryRow(`SELECT expires_at,revoked_at FROM console_sessions WHERE digest=?`, digest[:]).Scan(&expires, &revoked); err != nil || revoked.Valid || expires <= now {
		return errors.New("console preference session is unavailable")
	}
	if _, err := tx.Exec(`INSERT INTO console_preferences(digest,timezone,updated_at) VALUES(?,?,?)
		ON CONFLICT(digest) DO UPDATE SET timezone=excluded.timezone,updated_at=excluded.updated_at`, digest[:], validated, now); err != nil {
		return errors.New("console preference persistence failed")
	}
	if err := tx.Commit(); err != nil {
		return errors.New("console preference commit failed")
	}
	return nil
}
