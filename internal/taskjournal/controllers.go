package taskjournal

import (
	"errors"
	"time"
)

type ControllerActivity struct {
	Controller       string
	LastSeenAt       time.Time
	ActiveOperations int64
}

func (j *Journal) ControllerActivity() ([]ControllerActivity, error) {
	if j == nil || j.store == nil || j.store.db == nil {
		return []ControllerActivity{}, nil
	}
	j.store.mu.Lock()
	defer j.store.mu.Unlock()
	rows, err := j.store.db.Query(`SELECT controller,COALESCE(MAX(heartbeat_at),0),
		COALESCE(SUM(CASE WHEN terminal_at IS NULL THEN 1 ELSE 0 END),0)
		FROM tasks GROUP BY controller ORDER BY controller`)
	if err != nil {
		return nil, errors.New("task journal: controller activity unavailable")
	}
	defer rows.Close()
	activities := make([]ControllerActivity, 0, 3)
	for rows.Next() {
		var activity ControllerActivity
		var seen int64
		if err := rows.Scan(&activity.Controller, &seen, &activity.ActiveOperations); err != nil {
			return nil, errors.New("task journal: controller activity invalid")
		}
		if _, ok := validControllers[activity.Controller]; !ok {
			continue
		}
		if seen > 0 {
			activity.LastSeenAt = time.Unix(0, seen).UTC()
		}
		activities = append(activities, activity)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("task journal: controller activity failed")
	}
	return activities, nil
}
