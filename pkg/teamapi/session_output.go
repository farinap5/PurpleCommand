package teamapi

const MaxSessionOutputMessage = 64 << 10

// SessionOutput is an operator-facing message associated with a session and,
// optionally, a task. Clients route by Session and retain TaskID context when
// it is non-empty.
type SessionOutput struct {
	Session string `json:"session"`
	TaskID  string `json:"task_id,omitempty"`
	Message string `json:"message"`
	Source  string `json:"source"`
}
