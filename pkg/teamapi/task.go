package teamapi

import "time"

type TaskCreateRequest struct {
	Session string `json:"session"`
	Code    uint16 `json:"code"`
	Payload []byte `json:"payload,omitempty"`
}

type Task struct {
	ID           string    `json:"id"`
	Session      string    `json:"session"`
	Code         uint16    `json:"code"`
	Status       string    `json:"status"`
	Attempts     uint32    `json:"attempts"`
	Registered   time.Time `json:"registered"`
	LastSent     time.Time `json:"last_sent,omitempty"`
	ResponseTime time.Time `json:"response_time,omitempty"`
	Response     []byte    `json:"response,omitempty"`
}

type TaskListRequest struct {
	Session string `json:"session,omitempty"`
}

type TaskGetRequest struct {
	Session string `json:"session"`
	TaskID  string `json:"task_id"`
}