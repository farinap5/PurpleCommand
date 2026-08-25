package teamapi

import (
	"encoding/json"
	"time"
)

type Profile struct {
	Name                string          `json:"name"`
	Type                string          `json:"type"`
	LHOST               string          `json:"lhost"`
	OS                  string          `json:"os"`
	ARCH                string          `json:"arch"`
	OSOptions           []string        `json:"os_options"`
	ARCHOptions         []string        `json:"arch_options"`
	Protocol            string          `json:"protocol"`
	Options             json.RawMessage `json:"options"`
	OTS                 string          `json:"ots,omitempty"` // Write-only; never populated in replies.
	OTSConfigured       bool            `json:"ots_configured"`
	OTSExpiresAt        *time.Time      `json:"ots_expires_at,omitempty"`
	OTSUsedAt           *time.Time      `json:"ots_used_at,omitempty"`
	ConfigVersion       int             `json:"config_version,omitempty"`
	DefinitionCreatedAt *time.Time      `json:"definition_created_at,omitempty"`
	DefinitionUpdatedAt *time.Time      `json:"definition_updated_at,omitempty"`
	Output              string          `json:"output"`
	Template            string          `json:"template"`
	PublicKey           string          `json:"public_key"`
}

type ProfileUpdateRequest struct {
	Name  string `json:"name"`
	Key   string `json:"key"`
	Value string `json:"value"`
}
