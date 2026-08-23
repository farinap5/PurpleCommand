package teamapi

type Listener struct {
	Name         string `json:"name"`
	UUID         string `json:"uuid"`
	Host         string `json:"host"`
	Port         string `json:"port"`
	Running      bool   `json:"running"`
	Persistent   bool   `json:"persistent"`
	Associations int    `json:"associations"`
}

type ListenerCreateRequest struct {
	Name       string `json:"name"`
	Host       string `json:"host,omitempty"`
	Port       string `json:"port,omitempty"`
	Persistent *bool  `json:"persistent,omitempty"`
}

type ListenerUpdateRequest struct {
	Name  string `json:"name"`
	Key   string `json:"key"`
	Value string `json:"value"`
}