package teamapi

import "time"

type BuildRequest struct {
	Profile string `json:"profile"`
	Builder string `json:"builder,omitempty"`
}

type BuildDeleteRequest struct {
	ID string `json:"id"`
}

type Build struct {
	ID           string    `json:"id"`
	Profile      string    `json:"profile"`
	Builder      string    `json:"builder,omitempty"`
	Status       string    `json:"status"`
	ArtifactName string    `json:"artifact_name,omitempty"`
	Error        string    `json:"error,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
	DownloadURL  string    `json:"download_url,omitempty"`
}

// BuildOutput is emitted for stdout and stderr produced by a build command.
// BuildID correlates output with lifecycle events even when multiple profiles
// or queued jobs use the same builder.
type BuildOutput struct {
	BuildID string `json:"build_id"`
	Profile string `json:"profile"`
	Builder string `json:"builder"`
	Message string `json:"message"`
}
