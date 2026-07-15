// Package observability emits content-free structured operational events.
// It is deliberately separate from the richer private audit log.
package observability

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charle-z/mcp-devbox/internal/policy"
)

const (
	SchemaVersion   = 1
	DefaultMaxBytes = int64(16 << 20)
	MinMaxBytes     = int64(1 << 20)
	MaxMaxBytes     = int64(1 << 30)
)

type Mode string

const (
	ModeOff    Mode = "off"
	ModeStderr Mode = "stderr"
	ModeFile   Mode = "file"
	ModeBoth   Mode = "both"
)

type Level string

const (
	LevelInfo  Level = "info"
	LevelError Level = "error"
)

type Component string

const (
	ComponentServer Component = "server"
	ComponentHTTP   Component = "http"
	ComponentMCP    Component = "mcp"
	ComponentOther  Component = "other"
)

type EventName string

const (
	EventServerStart EventName = "server_start"
	EventServerStop  EventName = "server_stop"
	EventHTTPRequest EventName = "http_request"
	EventRPCRequest  EventName = "rpc_request"
	EventOther       EventName = "other"
)

type Transport string

const (
	TransportStdio    Transport = "stdio"
	TransportHTTP     Transport = "http"
	TransportInternal Transport = "internal"
	TransportOther    Transport = "other"
)

type Route string

const (
	RouteMCP     Route = "mcp"
	RouteHealth  Route = "health"
	RouteVersion Route = "version"
	RouteConsole Route = "console"
	RouteOAuth   Route = "oauth"
	RouteOther   Route = "other"
)

type Method string

const (
	MethodInitialize   Method = "initialize"
	MethodToolsList    Method = "tools/list"
	MethodToolsCall    Method = "tools/call"
	MethodPing         Method = "ping"
	MethodNotification Method = "notification"
	MethodOther        Method = "other"
)

type Outcome string

const (
	OutcomeSuccess   Outcome = "success"
	OutcomeAccepted  Outcome = "accepted"
	OutcomeDenied    Outcome = "denied"
	OutcomeError     Outcome = "error"
	OutcomeCancelled Outcome = "cancelled"
	OutcomeOther     Outcome = "other"
)

type ErrorClass string

const (
	ErrorNone          ErrorClass = ""
	ErrorParse         ErrorClass = "parse_error"
	ErrorInvalidParams ErrorClass = "invalid_params"
	ErrorUnknownMethod ErrorClass = "unknown_method"
	ErrorUnknownTool   ErrorClass = "unknown_tool"
	ErrorTool          ErrorClass = "tool_error"
	ErrorTransport     ErrorClass = "transport_error"
	ErrorInternal      ErrorClass = "internal_error"
)

// Config is immutable startup configuration for the structured event sink.
type Config struct {
	Mode     Mode
	Path     string
	MaxBytes int64
}

func DefaultConfig() Config {
	return Config{Mode: ModeStderr, MaxBytes: DefaultMaxBytes}
}

// ValidateConfig applies defaults and validates immutable startup configuration.
// File modes may omit Path here so the application can resolve its private default
// after the primary jail root is known; Open requires the resolved absolute path.
func ValidateConfig(cfg Config) (Config, error) {
	if cfg.Mode == "" {
		cfg.Mode = ModeStderr
	}
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = DefaultMaxBytes
	}
	if !validMode(cfg.Mode) {
		return Config{}, fmt.Errorf("unknown observability mode %q", cfg.Mode)
	}
	if cfg.MaxBytes < MinMaxBytes || cfg.MaxBytes > MaxMaxBytes {
		return Config{}, fmt.Errorf("observability max bytes must be between %d and %d", MinMaxBytes, MaxMaxBytes)
	}
	if strings.TrimSpace(cfg.Path) != "" && !filepath.IsAbs(cfg.Path) {
		return Config{}, errors.New("observability file path must be absolute")
	}
	if cfg.Path != "" {
		cfg.Path = filepath.Clean(cfg.Path)
	}
	return cfg, nil
}

// Event is intentionally closed: there is no message, body, params, error, path,
// target, or arbitrary attributes map.
type Event struct {
	SchemaVersion int        `json:"schema_version"`
	Time          string     `json:"time"`
	Level         Level      `json:"level"`
	Component     Component  `json:"component"`
	Name          EventName  `json:"event"`
	RequestID     string     `json:"request_id,omitempty"`
	Transport     Transport  `json:"transport,omitempty"`
	Route         Route      `json:"route,omitempty"`
	Method        Method     `json:"method,omitempty"`
	Tool          string     `json:"tool,omitempty"`
	Outcome       Outcome    `json:"outcome,omitempty"`
	StatusCode    int        `json:"status_code,omitempty"`
	DurationMS    int64      `json:"duration_ms,omitempty"`
	ErrorClass    ErrorClass `json:"error_class,omitempty"`
	Commit        string     `json:"commit,omitempty"`
	ToolCount     int        `json:"tool_count,omitempty"`
	CatalogHash   string     `json:"catalog_hash,omitempty"`
	RootCount     int        `json:"root_count,omitempty"`
}

type Logger struct {
	mu           sync.Mutex
	writers      []io.Writer
	closers      []io.Closer
	now          func() time.Time
	off          bool
	failures     atomic.Uint64
	summaryMu    sync.Mutex
	routeSummary map[Route]*routeAccumulator
}

func Open(cfg Config, stderr io.Writer) (*Logger, error) {
	var err error
	cfg, err = ValidateConfig(cfg)
	if err != nil {
		return nil, err
	}
	if stderr == nil {
		stderr = io.Discard
	}
	logger := &Logger{now: time.Now}
	switch cfg.Mode {
	case ModeOff:
		logger.off = true
	case ModeStderr:
		logger.writers = []io.Writer{stderr}
	case ModeFile, ModeBoth:
		if cfg.Path == "" {
			return nil, errors.New("observability file path is required for file mode")
		}
		file, err := openRotatingFile(cfg.Path, cfg.MaxBytes)
		if err != nil {
			return nil, err
		}
		logger.writers = append(logger.writers, file)
		logger.closers = append(logger.closers, file)
		if cfg.Mode == ModeBoth {
			logger.writers = append([]io.Writer{stderr}, logger.writers...)
		}
	}
	return logger, nil
}

func (l *Logger) Emit(event Event) error {
	if l == nil || l.off {
		return nil
	}
	event = normalizeEvent(event)
	l.observeSummary(event)
	// Time and schema version are always server-owned so callers cannot smuggle
	// free-form data through nominal metadata fields.
	event.Time = l.now().UTC().Format(time.RFC3339Nano)
	event.SchemaVersion = SchemaVersion
	encoded, err := json.Marshal(event)
	if err != nil {
		l.failures.Add(1)
		return err
	}
	encoded = append(encoded, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	var joined error
	for _, writer := range l.writers {
		if _, err := writer.Write(encoded); err != nil {
			l.failures.Add(1)
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

// Failures returns the number of event encoding or writer failures observed by this
// process. It exposes no event data or raw error text.
func (l *Logger) Failures() uint64 {
	if l == nil {
		return 0
	}
	return l.failures.Load()
}

// Enabled reports whether this logger emits events. It exposes no writer or path
// details and is used only to retain a sanitized startup diagnostic in off mode.
func (l *Logger) Enabled() bool {
	return l != nil && !l.off
}

func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var joined error
	for _, closer := range l.closers {
		joined = errors.Join(joined, closer.Close())
	}
	l.closers = nil
	return joined
}

func normalizeEvent(event Event) Event {
	if event.Level != LevelError {
		event.Level = LevelInfo
	}
	if !validComponent(event.Component) {
		event.Component = ComponentOther
	}
	if !validEventName(event.Name) {
		event.Name = EventOther
	}
	if event.Transport != "" && !validTransport(event.Transport) {
		event.Transport = TransportOther
	}
	if event.Route != "" && !validRoute(event.Route) {
		event.Route = RouteOther
	}
	if event.Method != "" && !validMethod(event.Method) {
		event.Method = MethodOther
	}
	if event.Outcome != "" && !validOutcome(event.Outcome) {
		event.Outcome = OutcomeOther
	}
	if !validErrorClass(event.ErrorClass) {
		event.ErrorClass = ErrorInternal
	}
	event.RequestID = safeRequestID(event.RequestID)
	event.Tool = safeTool(event.Tool)
	event.Commit = safeCommit(event.Commit)
	event.CatalogHash = safeCatalogHash(event.CatalogHash)
	if event.StatusCode < 0 || event.StatusCode > 999 {
		event.StatusCode = 0
	}
	if event.DurationMS < 0 {
		event.DurationMS = 0
	}
	if event.ToolCount < 0 {
		event.ToolCount = 0
	}
	if event.RootCount < 0 {
		event.RootCount = 0
	}
	return event
}

func validMode(value Mode) bool {
	return value == ModeOff || value == ModeStderr || value == ModeFile || value == ModeBoth
}

func validComponent(value Component) bool {
	return value == ComponentServer || value == ComponentHTTP || value == ComponentMCP || value == ComponentOther
}

func validEventName(value EventName) bool {
	return value == EventServerStart || value == EventServerStop || value == EventHTTPRequest || value == EventRPCRequest || value == EventOther
}

func validTransport(value Transport) bool {
	return value == TransportStdio || value == TransportHTTP || value == TransportInternal || value == TransportOther
}

func validRoute(value Route) bool {
	return value == RouteMCP || value == RouteHealth || value == RouteVersion || value == RouteConsole || value == RouteOAuth || value == RouteOther
}

func validMethod(value Method) bool {
	return value == MethodInitialize || value == MethodToolsList || value == MethodToolsCall || value == MethodPing || value == MethodNotification || value == MethodOther
}

func validOutcome(value Outcome) bool {
	return value == OutcomeSuccess || value == OutcomeAccepted || value == OutcomeDenied || value == OutcomeError || value == OutcomeCancelled || value == OutcomeOther
}

func validErrorClass(value ErrorClass) bool {
	switch value {
	case ErrorNone, ErrorParse, ErrorInvalidParams, ErrorUnknownMethod, ErrorUnknownTool, ErrorTool, ErrorTransport, ErrorInternal:
		return true
	default:
		return false
	}
}

var (
	requestIDPattern = regexp.MustCompile(`^[a-f0-9-]{8,64}$`)
	toolPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,79}$`)
	commitPattern    = regexp.MustCompile(`^(unknown|[a-f0-9]{7,64})$`)
	catalogPattern   = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

func safeRequestID(value string) string {
	if value == "" {
		return ""
	}
	if changedByRedaction(value) || !requestIDPattern.MatchString(value) {
		return "redacted"
	}
	return value
}

func safeTool(value string) string {
	if value == "" {
		return ""
	}
	value = strings.ToLower(strings.TrimSpace(value))
	if changedByRedaction(value) || !toolPattern.MatchString(value) {
		return "redacted"
	}
	return value
}

func safeCommit(value string) string {
	if value == "" {
		return ""
	}
	value = strings.ToLower(strings.TrimSpace(value))
	if changedByRedaction(value) || !commitPattern.MatchString(value) {
		return "redacted"
	}
	return value
}

func safeCatalogHash(value string) string {
	if value == "" {
		return ""
	}
	value = strings.ToLower(strings.TrimSpace(value))
	if changedByRedaction(value) || !catalogPattern.MatchString(value) {
		return "redacted"
	}
	return value
}

func changedByRedaction(value string) bool {
	redacted, changed := policy.Redact(value)
	return changed || redacted != value
}

var fallbackRequestID atomic.Uint64

func NewRequestID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return hex.EncodeToString(bytes[:])
	}
	return fmt.Sprintf("00000000-%016x", fallbackRequestID.Add(1))
}

type rotatingFile struct {
	path     string
	maxBytes int64
	file     *os.File
	size     int64
}

func openRotatingFile(path string, maxBytes int64) (*rotatingFile, error) {
	directory := filepath.Dir(path)
	if err := ensurePrivateDirectory(directory); err != nil {
		return nil, err
	}
	file, size, err := openPrivateAppend(path)
	if err != nil {
		return nil, err
	}
	return &rotatingFile{path: path, maxBytes: maxBytes, file: file, size: size}, nil
}

func ensurePrivateDirectory(directory string) error {
	if err := rejectSymlinkAncestors(directory); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return errors.New("observability directory is unavailable")
		}
		info, err = os.Lstat(directory)
	}
	if err != nil {
		return errors.New("observability directory is unavailable")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("observability directory is not a private directory")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return errors.New("observability directory permissions are too broad")
	}
	return nil
}

func rejectSymlinkAncestors(path string) error {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	rest := strings.TrimPrefix(clean, volume)
	current := volume
	if filepath.IsAbs(clean) {
		current += string(os.PathSeparator)
		rest = strings.TrimPrefix(rest, string(os.PathSeparator))
	}
	for _, part := range strings.Split(rest, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return errors.New("observability directory ancestry is unavailable")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("observability directory ancestry contains a symlink")
		}
		if !info.IsDir() {
			return errors.New("observability directory ancestry is not a directory")
		}
	}
	return nil
}

func openPrivateAppend(path string) (*os.File, int64, error) {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, 0, errors.New("observability file is not a private regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, 0, errors.New("observability file is unavailable")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, 0, errors.New("observability file is unavailable")
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, 0, errors.New("observability file permissions could not be secured")
	}
	fileInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, 0, errors.New("observability file metadata is unavailable")
	}
	pathInfo, err := os.Lstat(path)
	if err != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(fileInfo, pathInfo) {
		_ = file.Close()
		return nil, 0, errors.New("observability file changed during secure open")
	}
	return file, fileInfo.Size(), nil
}

func (writer *rotatingFile) Write(data []byte) (int, error) {
	if writer.size > 0 && writer.size+int64(len(data)) > writer.maxBytes {
		if err := writer.rotate(); err != nil {
			return 0, err
		}
	}
	written, err := writer.file.Write(data)
	writer.size += int64(written)
	return written, err
}

func (writer *rotatingFile) rotate() error {
	if err := writer.file.Close(); err != nil {
		return err
	}
	backup := writer.path + ".1"
	if err := os.Remove(backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(writer.path, backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Chmod(backup, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, size, err := openPrivateAppend(writer.path)
	if err != nil {
		return err
	}
	writer.file = file
	writer.size = size
	return nil
}

func (writer *rotatingFile) Close() error {
	if writer == nil || writer.file == nil {
		return nil
	}
	return writer.file.Close()
}
