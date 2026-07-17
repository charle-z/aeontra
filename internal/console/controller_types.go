package console

type ControllerData struct {
	Kind             string `json:"kind"`
	State            string `json:"state"`
	LastSeenAt       string `json:"last_seen_at"`
	ActiveOperations int64  `json:"active_operations"`
	ActiveRuntimes   int64  `json:"active_runtimes"`
}

type RuntimeData struct {
	RuntimeID    string `json:"runtime_id"`
	State        string `json:"state"`
	Controller   string `json:"controller"`
	LastActivity string `json:"last_activity"`
}
