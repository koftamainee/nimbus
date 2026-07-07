package scheduler

type DesiredContainer struct {
	Name     string   `json:"name"`
	Image    string   `json:"image"`
	Memory   string   `json:"memory,omitempty"`
	CPUs     float64  `json:"cpus,omitempty"`
	Env      []string `json:"env,omitempty"`
	Cmd      []string `json:"cmd,omitempty"`
	Replicas int      `json:"replicas"`
}

type NodeInfo struct {
	ID       string   `json:"id"`
	Hostname string   `json:"hostname"`
	Addr     string   `json:"addr"`
	Labels   []string `json:"labels,omitempty"`
}

type Heartbeat struct {
	Time string `json:"time"`
}

type Assignment struct {
	ContainerName string            `json:"container_name"`
	Spec          DesiredContainer `json:"spec"`
	NodeID        string            `json:"node_id"`
	Status        string            `json:"status"`
}

type ContainerStatus struct {
	ContainerName string `json:"container_name"`
	NodeID        string `json:"node_id"`
	Status        string `json:"status"`
	PID           int64  `json:"pid,omitempty"`
	ExitCode      *int   `json:"exit_code,omitempty"`
	StartedAt     string `json:"started_at,omitempty"`
	FinishedAt    string `json:"finished_at,omitempty"`
}

const (
	NodeDeadTimeout = 10
	HeartbeatKey    = "/nodes/%s/heartbeat"
	NodeKey         = "/nodes/%s"
	DesiredKey      = "/containers/%s/spec"
	DesiredPrefix   = "/containers/"
	AssignKey       = "/nodes/%s/assignments/%s"
	AssignPrefix    = "/nodes/%s/assignments/"
	StatusKey       = "/containers/%s/status"
	StatusPrefix    = "/containers/"
)
