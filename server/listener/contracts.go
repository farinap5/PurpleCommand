package listener

import (
	"context"
	"encoding/json"
	"time"
)

// State is the observable lifecycle state of a listener instance. Desired
// state is persisted separately; State always describes the current process.
type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateFailed   State = "failed"
)

// OptionType describes a value exposed by a listener driver or carrier. The
// schema is intended for validation and for rendering dynamic operator UIs.
type OptionType string

const (
	OptionString     OptionType = "string"
	OptionInteger    OptionType = "integer"
	OptionBoolean    OptionType = "boolean"
	OptionDuration   OptionType = "duration"
	OptionStringList OptionType = "string_list"
	OptionStringMap  OptionType = "string_map"
	OptionObject     OptionType = "object"
	OptionFile       OptionType = "file"
	OptionSecret     OptionType = "secret"
)

// OptionDefinition documents one typed configuration value. Key uses dotted
// paths for nested JSON objects, for example "bind.host".
type OptionDefinition struct {
	Key                 string          `json:"key"`
	Type                OptionType      `json:"type"`
	Description         string          `json:"description,omitempty"`
	Required            bool            `json:"required,omitempty"`
	Secret              bool            `json:"secret,omitempty"`
	MutableWhileRunning bool            `json:"mutable_while_running,omitempty"`
	Default             json.RawMessage `json:"default,omitempty"`
}

// DriverDefinition is the public, non-secret description of a listener
// implementation registered in this process.
type DriverDefinition struct {
	ID           string             `json:"id"`
	Description  string             `json:"description,omitempty"`
	Capabilities []string           `json:"capabilities,omitempty"`
	Options      []OptionDefinition `json:"options,omitempty"`
}

// CarrierDefinition describes a reversible placement mechanism such as a raw
// body, cookie, response header, or image container.
type CarrierDefinition struct {
	ID          string             `json:"id"`
	Description string             `json:"description,omitempty"`
	Options     []OptionDefinition `json:"options,omitempty"`
}

// CarrierSpec selects a registered carrier and supplies its typed options.
type CarrierSpec struct {
	Type    string          `json:"type"`
	Options json.RawMessage `json:"options,omitempty"`
}

// Route is deliberately transport-neutral. Driver-specific matching and
// response behavior are stored as validated JSON owned by that driver.
type Route struct {
	ID       string          `json:"id"`
	Purpose  string          `json:"purpose"`
	Priority int             `json:"priority,omitempty"`
	Match    json.RawMessage `json:"match,omitempty"`
	Options  json.RawMessage `json:"options,omitempty"`
	Inbound  CarrierSpec     `json:"inbound"`
	Outbound CarrierSpec     `json:"outbound"`
}

// RuntimeConfig is the immutable snapshot supplied to a newly started driver.
// A driver must not retain references to mutable manager-owned data.
type RuntimeConfig struct {
	Name    string
	UUID    string
	Driver  string
	Options json.RawMessage
	Routes  []Route
}

// ManagedListenerConfig is the persisted/control-plane portion of a listener.
// DesiredState accepts only stopped or running.
type ManagedListenerConfig struct {
	Name          string          `json:"name"`
	UUID          string          `json:"uuid"`
	Driver        string          `json:"driver"`
	Options       json.RawMessage `json:"options"`
	Routes        []Route         `json:"routes,omitempty"`
	Persistent    bool            `json:"persistent"`
	DesiredState  State           `json:"desired_state"`
	ConfigVersion int             `json:"config_version"`
}

type ManagedListenerSnapshot struct {
	Config ManagedListenerConfig `json:"config"`
	Status Status                `json:"status"`
}

// Exchange is the normalized request passed from a driver to the callback
// service. Metadata is transport-owned diagnostic context and must not be
// trusted for cryptographic authentication.
type Exchange struct {
	ListenerName         string
	ListenerUUID         string
	Transport            string
	RemoteAddress        string
	AuthenticatedSession string
	Payload              []byte
	Metadata             map[string][]string
}

// ExchangeResult is the transport-neutral callback result. Empty Payload is a
// valid response and normally means that no task is ready.
type ExchangeResult struct {
	MessageType uint16
	Payload     []byte
}

type ExchangeHandler func(context.Context, Exchange) (ExchangeResult, error)

// Runtime is one active driver instance. Done yields the terminal serve error,
// if any, and then closes. Stop must be safe to call more than once.
type Runtime interface {
	Address() string
	Done() <-chan error
	Stop(context.Context) error
}

// Driver validates persisted configuration and starts independent runtime
// instances. Implementations must be safe for concurrent Manager calls.
type Driver interface {
	Definition() DriverDefinition
	Validate(options json.RawMessage, routes []Route) error
	Start(context.Context, RuntimeConfig, ExchangeHandler) (Runtime, error)
}

// CarrierEnvelope is a protocol-independent collection of a body plus named
// fields. Drivers translate their native request/response representation into
// these fields; carriers only decide where encoded callback data is placed.
type CarrierEnvelope struct {
	Body   []byte
	Fields map[string][]string
}

type CarriedMessage struct {
	Payload []byte
	Session string
}

// Carrier is a reversible callback-data placement mechanism.
type Carrier interface {
	Definition() CarrierDefinition
	Validate(json.RawMessage) error
	Decode(CarrierEnvelope, json.RawMessage) (CarriedMessage, error)
	Encode(CarriedMessage, json.RawMessage) (CarrierEnvelope, error)
}

// Status captures runtime observations without mixing them into persisted
// configuration.
type Status struct {
	State     State     `json:"state"`
	Address   string    `json:"address,omitempty"`
	LastError string    `json:"last_error,omitempty"`
	ChangedAt time.Time `json:"changed_at"`
}
