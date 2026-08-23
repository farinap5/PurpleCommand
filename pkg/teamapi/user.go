package teamapi

import "time"

type User struct {
	Name      string    `json:"name"`
	UUID      string    `json:"uuid"`
	Admin     bool      `json:"admin"`
	Connected bool      `json:"connected"`
	Created   time.Time `json:"created"`
	LastSeen  time.Time `json:"last_seen,omitempty"`
}

type UserCreateRequest struct {
	Name string `json:"name"`
}

// UserUpdateRequest deliberately contains only the user name. Updating a user
// rotates the token; no other user properties can be changed through this API.
type UserUpdateRequest struct {
	Name string `json:"name"`
}

type UserCredentials struct {
	User  User   `json:"user"`
	Token string `json:"token"`
}

type UserMessageRequest struct {
	Message string `json:"message"`
}

type UserMessage struct {
	User    string `json:"user"`
	Message string `json:"message"`
}