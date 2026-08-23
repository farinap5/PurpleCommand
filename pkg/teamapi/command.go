package teamapi

type Command struct {
	PayloadType string `json:"payload_type"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type CommandListRequest struct {
	PayloadType string `json:"payload_type"`
}

type CommandExecuteRequest struct {
	Session     string            `json:"session"`
	Name        string            `json:"name"`
	Arguments   string            `json:"arguments,omitempty"`
	Attachments map[string]string `json:"attachments,omitempty"`
}

type CommandExecuteReply struct {
	TaskIDs []string `json:"task_ids,omitempty"`
	Message string   `json:"message,omitempty"`
}