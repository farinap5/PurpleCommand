package teamapi

// PayloadBuilder describes a build function registered by a loaded Lua script.
type PayloadBuilder struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
}
