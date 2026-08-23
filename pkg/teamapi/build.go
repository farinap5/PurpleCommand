package teamapi

import "time"

type BuildRequest struct {
	Profile string `json:"profile"`
}

type BuildDeleteRequest struct {
	ID string `json:"id"`
}

type Build struct {
	ID           string    `json:"id"`
	Profile      string    `json:"profile"`
	Status       string    `json:"status"`
	ArtifactName string    `json:"artifact_name,omitempty"`
	Error        string    `json:"error,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
	DownloadURL  string    `json:"download_url,omitempty"`
}
