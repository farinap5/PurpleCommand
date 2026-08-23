package teamapi

type Script struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Loaded bool   `json:"loaded"`
	SHA256 string `json:"sha256,omitempty"`
}

type ScriptLoadRequest struct {
	Name     string `json:"name,omitempty"`
	Path     string `json:"path,omitempty"`
	UploadID string `json:"upload_id,omitempty"`
}