package teamapi

import "time"

type LootRequest struct {
	UUID string `json:"uuid"`
}

type LootGetReply struct {
	Loot        Loot   `json:"loot"`
	DownloadURL string `json:"download_url"`
}

type Loot struct {
	UUID      string    `json:"uuid"`
	Session   string    `json:"session"`
	FileName  string    `json:"file_name"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}
