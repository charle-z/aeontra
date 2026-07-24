package edgeclient

import "errors"

func (j *Journal) PendingDeliveries(limit int) ([]JournalEntry, error) {
	if j == nil || j.db == nil || limit < 1 || limit > 128 {
		return nil, errors.New("journal pending delivery limit is invalid")
	}
	rows, err := j.db.Query(journalEntrySelect+` WHERE state=? AND delivered_at IS NULL AND lease_id IS NOT NULL AND lease_id!='' ORDER BY completed_at,idempotency_key LIMIT ?`, JournalCompleted, limit)
	if err != nil {
		return nil, errors.New("edge journal unavailable")
	}
	defer rows.Close()
	entries := make([]JournalEntry, 0, limit)
	for rows.Next() {
		entry, storedTask, found, err := queryJournalEntry(rows)
		if err != nil || !found || storedTask != entry.TaskID || entry.State != JournalCompleted || entry.Delivered || entry.LeaseID == "" {
			return nil, errors.New("edge journal pending delivery is invalid")
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("edge journal unavailable")
	}
	return entries, nil
}
