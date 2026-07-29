//go:build !windows

package edgeclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const edgeInstanceLockFile = "edge-instance.lock"
const edgeInstanceLockVersion = 1

var ErrEdgeInstanceLocked = errors.New("another Edge process already owns this state root")

var edgeReleasePattern = regexp.MustCompile(`^p15\.[0-9]+\.[0-9]+$`)
var edgeCommitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var edgeInvocationPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

type EdgeInstanceMetadata struct {
	SchemaVersion     int       `json:"schema_version"`
	PID               int       `json:"pid"`
	ProcessStartTicks uint64    `json:"process_start_ticks"`
	Release           string    `json:"release"`
	Commit            string    `json:"commit"`
	InvocationID      string    `json:"invocation_id,omitempty"`
	AcquiredAt        time.Time `json:"acquired_at"`
}

type EdgeInstanceLockReport struct {
	State            string
	Metadata         EdgeInstanceMetadata
	MetadataValid    bool
	ProcessActive    bool
	LockHeld         bool
	StaleRecoverable bool
}

type EdgeInstanceLock struct {
	mu     sync.Mutex
	file   *os.File
	closed bool
}

func AcquireEdgeInstanceLock(stateRoot, release, commit string) (*EdgeInstanceLock, error) {
	stateRoot = filepath.Clean(strings.TrimSpace(stateRoot))
	release = strings.TrimSpace(release)
	commit = strings.TrimSpace(commit)
	if !edgeReleasePattern.MatchString(release) || !edgeCommitPattern.MatchString(commit) {
		return nil, errors.New("edge instance identity is invalid")
	}
	if err := preparePrivateRoot(stateRoot); err != nil {
		return nil, err
	}
	file, err := openEdgeInstanceLockFile(filepath.Join(stateRoot, edgeInstanceLockFile))
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrEdgeInstanceLocked
		}
		return nil, errors.New("edge instance lock unavailable")
	}
	startTicks, err := processStartTicks(os.Getpid())
	if err != nil {
		_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
		_ = file.Close()
		return nil, errors.New("edge process identity unavailable")
	}
	invocationID := currentSystemdInvocationID(os.Getpid())
	metadata := EdgeInstanceMetadata{
		SchemaVersion:     edgeInstanceLockVersion,
		PID:               os.Getpid(),
		ProcessStartTicks: startTicks,
		Release:           release,
		Commit:            commit,
		InvocationID:      invocationID,
		AcquiredAt:        time.Now().UTC(),
	}
	if err := persistEdgeInstanceMetadata(file, metadata); err != nil {
		_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
		_ = file.Close()
		return nil, err
	}
	return &EdgeInstanceLock{file: file}, nil
}

func (lock *EdgeInstanceLock) Close() error {
	if lock == nil {
		return nil
	}
	lock.mu.Lock()
	defer lock.mu.Unlock()
	if lock.closed {
		return nil
	}
	lock.closed = true
	if lock.file == nil {
		return nil
	}
	unlockErr := unix.Flock(int(lock.file.Fd()), unix.LOCK_UN)
	closeErr := lock.file.Close()
	if unlockErr != nil || closeErr != nil {
		return errors.New("edge instance lock release failed")
	}
	return nil
}

func InspectEdgeInstanceLock(stateRoot string) (EdgeInstanceLockReport, error) {
	stateRoot = filepath.Clean(strings.TrimSpace(stateRoot))
	if err := validatePrivateRoot(stateRoot); err != nil {
		return EdgeInstanceLockReport{}, err
	}
	path := filepath.Join(stateRoot, edgeInstanceLockFile)
	file, err := openExistingEdgeInstanceLockFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return EdgeInstanceLockReport{State: "missing"}, nil
	}
	if err != nil {
		return EdgeInstanceLockReport{}, err
	}
	defer file.Close()

	metadata, metadataErr := readEdgeInstanceMetadata(file)
	report := EdgeInstanceLockReport{Metadata: metadata, MetadataValid: metadataErr == nil}
	lockErr := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if lockErr == nil {
		_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
		if metadataErr != nil {
			report.State = "stale_recoverable"
			report.StaleRecoverable = true
			return report, nil
		}
		report.ProcessActive = edgeMetadataProcessActive(metadata)
		if report.ProcessActive {
			report.State = "unlocked_live_owner"
			return report, nil
		}
		report.State = "stale_recoverable"
		report.StaleRecoverable = true
		return report, nil
	}
	if !errors.Is(lockErr, unix.EWOULDBLOCK) && !errors.Is(lockErr, unix.EAGAIN) {
		return EdgeInstanceLockReport{}, errors.New("edge instance lock inspection failed")
	}
	report.LockHeld = true
	if metadataErr != nil {
		report.State = "held_unverified"
		return report, nil
	}
	report.ProcessActive = edgeMetadataProcessActive(metadata)
	if !report.ProcessActive {
		report.State = "held_incoherent"
		return report, nil
	}
	report.State = "held"
	return report, nil
}

func EdgeInstanceLockPath(stateRoot string) string {
	return filepath.Join(filepath.Clean(strings.TrimSpace(stateRoot)), edgeInstanceLockFile)
}

func openEdgeInstanceLockFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, errors.New("edge instance lock unavailable")
	}
	file := os.NewFile(uintptr(fd), path)
	if err := validateEdgeInstanceLockFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func openExistingEdgeInstanceLockFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, os.ErrNotExist
		}
		return nil, errors.New("edge instance lock unavailable")
	}
	file := os.NewFile(uintptr(fd), path)
	if err := validateEdgeInstanceLockFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func validateEdgeInstanceLockFile(file *os.File) error {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o077 != 0 || int(stat.Uid) != os.Geteuid() {
		return errors.New("edge instance lock is unsafe")
	}
	return nil
}

func persistEdgeInstanceMetadata(file *os.File, metadata EdgeInstanceMetadata) error {
	content, err := json.Marshal(metadata)
	if err != nil {
		return errors.New("edge instance metadata unavailable")
	}
	content = append(content, '\n')
	if len(content) > 2048 {
		return errors.New("edge instance metadata unavailable")
	}
	if err := file.Truncate(0); err != nil {
		return errors.New("edge instance metadata persistence failed")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return errors.New("edge instance metadata persistence failed")
	}
	if _, err := file.Write(content); err != nil {
		return errors.New("edge instance metadata persistence failed")
	}
	if err := file.Sync(); err != nil {
		return errors.New("edge instance metadata persistence failed")
	}
	return nil
}

func readEdgeInstanceMetadata(file *os.File) (EdgeInstanceMetadata, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return EdgeInstanceMetadata{}, errors.New("edge instance metadata unavailable")
	}
	content, err := io.ReadAll(io.LimitReader(file, 2049))
	if err != nil || len(content) == 0 || len(content) > 2048 {
		return EdgeInstanceMetadata{}, errors.New("edge instance metadata invalid")
	}
	var metadata EdgeInstanceMetadata
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decodeSingleJSON(decoder, &metadata); err != nil || !validEdgeInstanceMetadata(metadata) {
		return EdgeInstanceMetadata{}, errors.New("edge instance metadata invalid")
	}
	return metadata, nil
}

func currentSystemdInvocationID(pid int) string {
	invocationID := strings.TrimSpace(os.Getenv("INVOCATION_ID"))
	if !edgeInvocationPattern.MatchString(invocationID) {
		return ""
	}
	execPID := strings.TrimSpace(os.Getenv("SYSTEMD_EXEC_PID"))
	if execPID != "" && execPID != strconv.Itoa(pid) {
		return ""
	}
	return invocationID
}

func validEdgeInstanceMetadata(metadata EdgeInstanceMetadata) bool {
	return metadata.SchemaVersion == edgeInstanceLockVersion && metadata.PID > 1 && metadata.ProcessStartTicks > 0 && edgeReleasePattern.MatchString(metadata.Release) && edgeCommitPattern.MatchString(metadata.Commit) && (metadata.InvocationID == "" || edgeInvocationPattern.MatchString(metadata.InvocationID)) && !metadata.AcquiredAt.IsZero()
}

func edgeMetadataProcessActive(metadata EdgeInstanceMetadata) bool {
	if !validEdgeInstanceMetadata(metadata) || unix.Kill(metadata.PID, 0) != nil {
		return false
	}
	startTicks, err := processStartTicks(metadata.PID)
	return err == nil && startTicks == metadata.ProcessStartTicks
}

func processStartTicks(pid int) (uint64, error) {
	content, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, err
	}
	closing := bytes.LastIndexByte(content, ')')
	if closing < 0 || closing+2 >= len(content) {
		return 0, errors.New("invalid process stat")
	}
	fields := strings.Fields(string(content[closing+2:]))
	const startTimeIndex = 19
	if len(fields) <= startTimeIndex {
		return 0, errors.New("invalid process stat")
	}
	value, err := strconv.ParseUint(fields[startTimeIndex], 10, 64)
	if err != nil || value == 0 {
		return 0, errors.New("invalid process start time")
	}
	return value, nil
}
