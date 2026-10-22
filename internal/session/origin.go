package session

type Origin struct {
	Kind       string `json:"kind"`
	Sub        string `json:"sub"`
	Name       string `json:"name,omitempty"`
	ID         string `json:"id,omitempty"`
	Schedule   string `json:"schedule,omitempty"`
	Exit       *int   `json:"exit,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
}
