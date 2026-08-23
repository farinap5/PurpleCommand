package teamapi

type Profile struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	LHOST     string `json:"lhost"`
	OS        string `json:"os"`
	ARCH      string `json:"arch"`
	URI       string `json:"uri"`
	UA        string `json:"ua"`
	Output    string `json:"output"`
	Template  string `json:"template"`
	PublicKey string `json:"public_key"`
}

type ProfileUpdateRequest struct {
	Name  string `json:"name"`
	Key   string `json:"key"`
	Value string `json:"value"`
}