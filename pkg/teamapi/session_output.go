package teamapi

const MaxSessionOutputMessage = 64 << 10

// SessionOutput is an operator-facing message associated with one task in one
// session. Clients use both identifiers to route the message to the correct
// session view and retain its task context.
type SessionOutput struct {
	Session string `json:"session"`
	TaskID  string `json:"task_id"`
	Message string `json:"message"`
	Source  string `json:"source"`
}
