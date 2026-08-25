package edge

import "errors"

const (
	maxOperationStorageReservePages = int64(512)
	operationStoragePruneBatch      = 256
	operationStoragePruneAttempts   = 64
)

func (s *Store) ensureOperationStorageCapacityLocked() error {
	for attempt := 0; attempt < operationStoragePruneAttempts; attempt++ {
		available, reserve, err := s.operationStorageCapacityLocked()
		if err != nil {
			return err
		}
		if available >= reserve {
			return nil
		}
		result, err := s.db.Exec(`DELETE FROM edge_operations WHERE operation_id IN (
			SELECT operation_id FROM edge_operations
			WHERE state IN (?,?,?)
			ORDER BY updated_at,created_at,operation_id
			LIMIT ?
		)`, OperationSucceeded, OperationFailed, OperationCancelled, operationStoragePruneBatch)
		if err != nil {
			return errors.New("edge operation retention failed")
		}
		removed, err := result.RowsAffected()
		if err != nil || removed == 0 {
			return errors.New("edge operation storage is exhausted")
		}
	}
	return errors.New("edge operation storage is exhausted")
}

func (s *Store) operationStorageCapacityLocked() (int64, int64, error) {
	var pageCount, freePages, maxPages int64
	if err := s.db.QueryRow(`PRAGMA page_count`).Scan(&pageCount); err != nil {
		return 0, 0, errors.New("edge operation storage is unavailable")
	}
	if err := s.db.QueryRow(`PRAGMA freelist_count`).Scan(&freePages); err != nil {
		return 0, 0, errors.New("edge operation storage is unavailable")
	}
	if err := s.db.QueryRow(`PRAGMA max_page_count`).Scan(&maxPages); err != nil || maxPages < 1 || pageCount < 0 || freePages < 0 || pageCount > maxPages {
		return 0, 0, errors.New("edge operation storage is unavailable")
	}
	reserve := maxPages / 16
	if reserve < 1 {
		reserve = 1
	}
	if reserve > maxOperationStorageReservePages {
		reserve = maxOperationStorageReservePages
	}
	return maxPages - pageCount + freePages, reserve, nil
}
