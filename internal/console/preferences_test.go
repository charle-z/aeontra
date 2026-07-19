package console

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/taskjournal"
)

func newPreferenceHandler(t *testing.T, path string, now *time.Time, random []byte, journal *taskjournal.Journal) *Handler {
	t.Helper()
	handler, err := New(Config{
		Runtime:     Status{Status: "ok", Version: "0.2.0", ProtocolVersion: "2024-11-05", Commit: "abcdef0", ToolCount: 86, CatalogHash: "sha256:deb3419f64ac9e63e1f85b4ed841b19c2ac252f411fcef9ff9aca5b5e1108a85"},
		TaskJournal: journal,
		Session: SessionConfig{
			Path: path, TTL: time.Hour, MaxSessions: 8,
			Now:  func() time.Time { return *now },
			Rand: bytes.NewReader(random),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func preferenceRequest(t *testing.T, handler *Handler, method, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, preferencesPath, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	return serveConsole(t, handler, request)
}

func readTimezone(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 1 {
		t.Fatalf("preference response keys=%v", raw)
	}
	var timezone string
	if err := json.Unmarshal(raw["timezone"], &timezone); err != nil {
		t.Fatal(err)
	}
	return timezone
}

func TestTimezonePreferenceDefaultsPersistsAndIsSessionScoped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "console", "sessions.db")
	now := time.Date(2026, 7, 17, 19, 32, 10, 0, time.UTC)
	random := append(bytes.Repeat([]byte{0x11}, sessionBytes), bytes.Repeat([]byte{0x22}, sessionBytes)...)
	first := newPreferenceHandler(t, path, &now, random, nil)
	firstRaw, err := first.sessions.Create()
	if err != nil {
		t.Fatal(err)
	}
	secondRaw, err := first.sessions.Create()
	if err != nil {
		t.Fatal(err)
	}
	firstCookie := &http.Cookie{Name: cookieName, Value: firstRaw}
	secondCookie := &http.Cookie{Name: cookieName, Value: secondRaw}

	defaultResponse := preferenceRequest(t, first, http.MethodGet, "", firstCookie)
	if got := readTimezone(t, defaultResponse); got != DefaultTimezone {
		t.Fatalf("default timezone=%q", got)
	}
	if defaultResponse.Header().Get("Cache-Control") != "no-store" || defaultResponse.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("preference response is not hardened")
	}
	updated := preferenceRequest(t, first, http.MethodPut, `{"timezone":"Europe/Moscow"}`, firstCookie)
	if got := readTimezone(t, updated); got != "Europe/Moscow" {
		t.Fatalf("updated timezone=%q", got)
	}
	if got := readTimezone(t, preferenceRequest(t, first, http.MethodGet, "", secondCookie)); got != DefaultTimezone {
		t.Fatalf("second session inherited timezone=%q", got)
	}
	digest := sha256.Sum256([]byte(firstRaw))
	for _, secret := range []string{firstRaw, secondRaw, string(digest[:])} {
		if strings.Contains(updated.Body.String(), secret) {
			t.Fatal("preference response leaked session identity")
		}
	}
	if err := first.sessions.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := newPreferenceHandler(t, path, &now, bytes.Repeat([]byte{0x33}, sessionBytes), nil)
	defer reopened.sessions.Close()
	if got := readTimezone(t, preferenceRequest(t, reopened, http.MethodGet, "", firstCookie)); got != "Europe/Moscow" {
		t.Fatalf("reopened timezone=%q", got)
	}
	if err := reopened.sessions.SetTimezone(firstRaw, DefaultTimezone); err != nil {
		t.Fatal(err)
	}
	if err := reopened.sessions.Close(); err != nil {
		t.Fatal(err)
	}

	reopenedAgain := newPreferenceHandler(t, path, &now, bytes.Repeat([]byte{0x34}, sessionBytes), nil)
	defer reopenedAgain.sessions.Close()
	if got := readTimezone(t, preferenceRequest(t, reopenedAgain, http.MethodGet, "", firstCookie)); got != DefaultTimezone {
		t.Fatalf("restored timezone=%q", got)
	}
}

func TestTimezonePreferenceRejectsInvalidUnknownAndUnauthenticatedRequests(t *testing.T) {
	now := time.Date(2026, 7, 17, 19, 32, 10, 0, time.UTC)
	handler := newPreferenceHandler(t, filepath.Join(t.TempDir(), "console", "sessions.db"), &now, bytes.Repeat([]byte{0x44}, sessionBytes), nil)
	defer handler.sessions.Close()
	raw, err := handler.sessions.Create()
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: cookieName, Value: raw}

	if got := preferenceRequest(t, handler, http.MethodGet, "", nil).Code; got != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d", got)
	}
	for _, body := range []string{
		`{"timezone":"COT"}`,
		`{"timezone":"America/Not_A_Real_Zone"}`,
		`{"timezone":"America/Bogota","extra":true}`,
		`{"timezone":"America/Bogota"} {}`,
		`{"timezone":""}`,
	} {
		if got := preferenceRequest(t, handler, http.MethodPut, body, cookie).Code; got != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d", body, got)
		}
	}
	request := httptest.NewRequest(http.MethodPut, preferencesPath, strings.NewReader(`{"timezone":"UTC"}`))
	request.AddCookie(cookie)
	request.Header.Set("Content-Type", "text/plain")
	if got := serveConsole(t, handler, request).Code; got != http.StatusUnsupportedMediaType {
		t.Fatalf("content type status=%d", got)
	}
}

func TestTimezonePreferenceIsRemovedOrUnusableAfterLogoutAndExpiry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "console", "sessions.db")
	now := time.Date(2026, 7, 17, 19, 32, 10, 0, time.UTC)
	random := append(bytes.Repeat([]byte{0x51}, sessionBytes), bytes.Repeat([]byte{0x52}, sessionBytes)...)
	random = append(random, bytes.Repeat([]byte{0x53}, sessionBytes)...)
	handler := newPreferenceHandler(t, path, &now, random, nil)
	defer handler.sessions.Close()

	loggedOut, _ := handler.sessions.Create()
	if err := handler.sessions.SetTimezone(loggedOut, "Europe/Moscow"); err != nil {
		t.Fatal(err)
	}
	logout := httptest.NewRequest(http.MethodPost, logoutPath, nil)
	logout.AddCookie(&http.Cookie{Name: cookieName, Value: loggedOut})
	if got := serveConsole(t, handler, logout).Code; got != http.StatusSeeOther {
		t.Fatalf("logout status=%d", got)
	}
	if _, ok := handler.sessions.Timezone(loggedOut, DefaultTimezone); ok {
		t.Fatal("revoked session retained usable preference")
	}
	var preferences int
	if err := handler.sessions.db.QueryRow(`SELECT COUNT(*) FROM console_preferences`).Scan(&preferences); err != nil || preferences != 0 {
		t.Fatalf("preferences=%d err=%v", preferences, err)
	}

	expired, _ := handler.sessions.Create()
	if err := handler.sessions.SetTimezone(expired, "UTC"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour + time.Second)
	if _, ok := handler.sessions.Timezone(expired, DefaultTimezone); ok {
		t.Fatal("expired session retained usable preference")
	}
	if _, err := handler.sessions.Create(); err != nil {
		t.Fatal(err)
	}
	if err := handler.sessions.db.QueryRow(`SELECT COUNT(*) FROM console_preferences`).Scan(&preferences); err != nil || preferences != 0 {
		t.Fatalf("expired preferences=%d err=%v", preferences, err)
	}
}

func TestTimezonePreferenceNeverRewritesUTCJournalTimestamps(t *testing.T) {
	journal, err := taskjournal.Open(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if err := journal.Start("0123456789abcdef0123456789abcdef", "repo_status", "http"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	handler := newPreferenceHandler(t, filepath.Join(t.TempDir(), "console", "sessions.db"), &now, bytes.Repeat([]byte{0x61}, sessionBytes), journal)
	defer handler.sessions.Close()
	raw, _ := handler.sessions.Create()
	cookie := &http.Cookie{Name: cookieName, Value: raw}

	readTaskTimes := func() (string, string, string) {
		request := httptest.NewRequest(http.MethodGet, tasksPath+"?limit=1", nil)
		request.AddCookie(cookie)
		response := serveConsole(t, handler, request)
		var payload struct {
			Tasks []struct {
				CreatedAt   string `json:"created_at"`
				UpdatedAt   string `json:"updated_at"`
				HeartbeatAt string `json:"heartbeat_at"`
			} `json:"tasks"`
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &payload) != nil || len(payload.Tasks) != 1 {
			t.Fatalf("task response=%d %s", response.Code, response.Body.String())
		}
		return payload.Tasks[0].CreatedAt, payload.Tasks[0].UpdatedAt, payload.Tasks[0].HeartbeatAt
	}
	readEventTime := func() string {
		request := httptest.NewRequest(http.MethodGet, eventLogPath+"?limit=1", nil)
		request.AddCookie(cookie)
		response := serveConsole(t, handler, request)
		var payload struct {
			Events []struct {
				OccurredAt string `json:"occurred_at"`
			} `json:"events"`
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &payload) != nil || len(payload.Events) != 1 {
			t.Fatalf("event response=%d %s", response.Code, response.Body.String())
		}
		return payload.Events[0].OccurredAt
	}
	beforeCreated, beforeUpdated, beforeHeartbeat := readTaskTimes()
	beforeEvent := readEventTime()
	if err := handler.sessions.SetTimezone(raw, "America/Argentina/Buenos_Aires"); err != nil {
		t.Fatal(err)
	}
	afterCreated, afterUpdated, afterHeartbeat := readTaskTimes()
	afterEvent := readEventTime()
	if beforeCreated != afterCreated || beforeUpdated != afterUpdated || beforeHeartbeat != afterHeartbeat || beforeEvent != afterEvent {
		t.Fatal("timezone preference rewrote UTC task timestamps")
	}
	for _, value := range []string{afterCreated, afterUpdated, afterHeartbeat, afterEvent} {
		if !strings.HasSuffix(value, "Z") {
			t.Fatalf("timestamp is not UTC RFC3339: %q", value)
		}
	}
}
