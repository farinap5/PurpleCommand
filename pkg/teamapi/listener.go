package teamapi

import "encoding/json"

type Listener struct {
	Name          string          `json:"name"`
	UUID          string          `json:"uuid"`
	Host          string          `json:"host"`
	Port          string          `json:"port"`
	Running       bool            `json:"running"`
	Persistent    bool            `json:"persistent"`
	Associations  int             `json:"associations"`
	Driver        string          `json:"driver,omitempty"`
	Options       json.RawMessage `json:"options,omitempty"`
	Routes        []ListenerRoute `json:"routes,omitempty"`
	State         string          `json:"state,omitempty"`
	DesiredState  string          `json:"desired_state,omitempty"`
	Address       string          `json:"address,omitempty"`
	LastError     string          `json:"last_error,omitempty"`
	ConfigVersion int             `json:"config_version,omitempty"`
}

type ListenerCreateRequest struct {
	Name       string          `json:"name"`
	Host       string          `json:"host,omitempty"`
	Port       string          `json:"port,omitempty"`
	Persistent *bool           `json:"persistent,omitempty"`
	Driver     string          `json:"driver,omitempty"`
	Options    json.RawMessage `json:"options,omitempty"`
	Routes     []ListenerRoute `json:"routes,omitempty"`
	Start      bool            `json:"start,omitempty"`
}

type ListenerUpdateRequest struct {
	Name                  string          `json:"name"`
	Driver                string          `json:"driver,omitempty"`
	Key                   string          `json:"key,omitempty"`
	Value                 string          `json:"value,omitempty"`
	Options               json.RawMessage `json:"options,omitempty"`
	Routes                []ListenerRoute `json:"routes,omitempty"`
	Persistent            *bool           `json:"persistent,omitempty"`
	ExpectedConfigVersion int             `json:"expected_config_version,omitempty"`
}

type ListenerCarrierSpec struct {
	Type    string          `json:"type"`
	Options json.RawMessage `json:"options,omitempty"`
}

type ListenerRoute struct {
	ID       string              `json:"id"`
	Purpose  string              `json:"purpose"`
	Priority int                 `json:"priority,omitempty"`
	Match    json.RawMessage     `json:"match,omitempty"`
	Options  json.RawMessage     `json:"options,omitempty"`
	Inbound  ListenerCarrierSpec `json:"inbound"`
	Outbound ListenerCarrierSpec `json:"outbound"`
}

type ListenerOptionDefinition struct {
	Key                 string          `json:"key"`
	Type                string          `json:"type"`
	Description         string          `json:"description,omitempty"`
	Required            bool            `json:"required,omitempty"`
	Secret              bool            `json:"secret,omitempty"`
	MutableWhileRunning bool            `json:"mutable_while_running,omitempty"`
	Default             json.RawMessage `json:"default,omitempty"`
}

type ListenerDriverDefinition struct {
	ID           string                     `json:"id"`
	Description  string                     `json:"description,omitempty"`
	Capabilities []string                   `json:"capabilities,omitempty"`
	Options      []ListenerOptionDefinition `json:"options,omitempty"`
}

type ListenerCarrierDefinition struct {
	ID          string                     `json:"id"`
	Description string                     `json:"description,omitempty"`
	Options     []ListenerOptionDefinition `json:"options,omitempty"`
}
