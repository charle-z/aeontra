package edge

import (
	"database/sql"
	"errors"
	"regexp"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

var (
	modelCreateIDPattern = regexp.MustCompile(`^ec_[a-f0-9]{32}$`)
	modelWaitIDPattern   = regexp.MustCompile(`^ew_[a-f0-9]{32}$`)
)

type modelTurnCreateReceipt struct {
	DeviceID      string
	RuntimeID     string
	CreateID      string
	TurnID        modelturn.TurnID
	Sequence      uint64
	RequestDigest string
	RequestRef    string
	CreatedAt     time.Time
}

type modelTurnWaitReceipt struct {
	DeviceID  string
	RuntimeID string
	TurnID    modelturn.TurnID
	WaitID    string
	CreatedAt time.Time
}

func (s *Store) ensureModelRelaySchema() error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS edge_model_turn_creates (
			device_id TEXT NOT NULL,
			runtime_id TEXT NOT NULL,
			create_id TEXT NOT NULL,
			turn_id TEXT NOT NULL,
			sequence INTEGER NOT NULL,
			request_digest TEXT NOT NULL,
			request_ref TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			PRIMARY KEY(device_id,create_id),
			UNIQUE(runtime_id,sequence),
			UNIQUE(turn_id),
			FOREIGN KEY(device_id) REFERENCES devices(device_id)
		) WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS edge_model_turn_waits (
			device_id TEXT NOT NULL,
			runtime_id TEXT NOT NULL,
			turn_id TEXT NOT NULL,
			wait_id TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			PRIMARY KEY(device_id,wait_id),
			UNIQUE(turn_id),
			FOREIGN KEY(device_id) REFERENCES devices(device_id)
		) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS edge_model_turn_creates_runtime ON edge_model_turn_creates(device_id,runtime_id,sequence)`,
		`CREATE INDEX IF NOT EXISTS edge_model_turn_waits_runtime ON edge_model_turn_waits(device_id,runtime_id,turn_id)`,
	} {
		if _, err := s.db.Exec(statement); err != nil {
			return errors.New("edge model relay schema initialization failed")
		}
	}
	return nil
}

func (s *Store) modelCreateReceipt(deviceID, createID string) (modelTurnCreateReceipt, error) {
	if !idPattern.MatchString(deviceID) || !modelCreateIDPattern.MatchString(createID) {
		return modelTurnCreateReceipt{}, errors.New("model turn create identity is invalid")
	}
	var receipt modelTurnCreateReceipt
	var createdAt int64
	err := s.db.QueryRow(`SELECT device_id,runtime_id,create_id,turn_id,sequence,request_digest,request_ref,created_at FROM edge_model_turn_creates WHERE device_id=? AND create_id=?`, deviceID, createID).Scan(
		&receipt.DeviceID, &receipt.RuntimeID, &receipt.CreateID, &receipt.TurnID, &receipt.Sequence, &receipt.RequestDigest, &receipt.RequestRef, &createdAt,
	)
	if err != nil {
		return modelTurnCreateReceipt{}, err
	}
	receipt.CreatedAt = time.Unix(0, createdAt).UTC()
	return receipt, nil
}

func (s *Store) recordModelCreate(deviceID, runtimeID, createID string, turn modelturn.Turn) error {
	if !idPattern.MatchString(deviceID) || !modelCreateIDPattern.MatchString(createID) || turn.RuntimeID != runtimeID || turn.Sequence == 0 || turn.ID == "" || turn.RequestDigest == "" || turn.RequestRef == "" {
		return errors.New("model turn create receipt is invalid")
	}
	_, err := s.db.Exec(`INSERT INTO edge_model_turn_creates(device_id,runtime_id,create_id,turn_id,sequence,request_digest,request_ref,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		deviceID, runtimeID, createID, turn.ID, turn.Sequence, turn.RequestDigest, turn.RequestRef, s.now().UTC().UnixNano())
	if err == nil {
		return nil
	}
	receipt, readErr := s.modelCreateReceipt(deviceID, createID)
	if readErr == nil && receipt.RuntimeID == runtimeID && receipt.TurnID == turn.ID && receipt.Sequence == turn.Sequence && receipt.RequestDigest == turn.RequestDigest && receipt.RequestRef == turn.RequestRef {
		return nil
	}
	return errors.New("model turn create receipt conflict")
}

func (s *Store) ensureModelWaitReceipt(deviceID, runtimeID string, turnID modelturn.TurnID, waitID string) (modelTurnWaitReceipt, bool, error) {
	if !idPattern.MatchString(deviceID) || !modelWaitIDPattern.MatchString(waitID) || turnID == "" {
		return modelTurnWaitReceipt{}, false, errors.New("model turn wait identity is invalid")
	}
	now := s.now().UTC()
	_, err := s.db.Exec(`INSERT INTO edge_model_turn_waits(device_id,runtime_id,turn_id,wait_id,created_at) VALUES(?,?,?,?,?)`, deviceID, runtimeID, turnID, waitID, now.UnixNano())
	if err == nil {
		return modelTurnWaitReceipt{DeviceID: deviceID, RuntimeID: runtimeID, TurnID: turnID, WaitID: waitID, CreatedAt: now}, true, nil
	}
	var receipt modelTurnWaitReceipt
	var createdAt int64
	readErr := s.db.QueryRow(`SELECT device_id,runtime_id,turn_id,wait_id,created_at FROM edge_model_turn_waits WHERE device_id=? AND wait_id=?`, deviceID, waitID).Scan(
		&receipt.DeviceID, &receipt.RuntimeID, &receipt.TurnID, &receipt.WaitID, &createdAt,
	)
	if readErr == nil {
		receipt.CreatedAt = time.Unix(0, createdAt).UTC()
		if receipt.RuntimeID == runtimeID && receipt.TurnID == turnID {
			return receipt, false, nil
		}
		return modelTurnWaitReceipt{}, false, errors.New("model turn wait receipt conflict")
	}
	if errors.Is(readErr, sql.ErrNoRows) {
		return modelTurnWaitReceipt{}, false, errors.New("model turn already has another wait identity")
	}
	return modelTurnWaitReceipt{}, false, errors.New("model turn wait receipt unavailable")
}

func (s *Store) deviceActive(deviceID string) bool {
	if !idPattern.MatchString(deviceID) {
		return false
	}
	var state State
	return s.db.QueryRow(`SELECT state FROM devices WHERE device_id=?`, deviceID).Scan(&state) == nil && state == StateActive
}
