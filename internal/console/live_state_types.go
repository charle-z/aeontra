package console

type ProjectData struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Current bool   `json:"current"`
}

type EdgeDeviceData struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	PairedAt string `json:"paired_at"`
}

type StorageData struct {
	Available     bool   `json:"available"`
	DatabaseBytes int64  `json:"database_bytes"`
	WALBytes      int64  `json:"wal_bytes"`
	LogBytes      int64  `json:"log_bytes"`
	TotalBytes    int64  `json:"total_bytes"`
	LimitBytes    int64  `json:"limit_bytes"`
	State         string `json:"state"`
}
