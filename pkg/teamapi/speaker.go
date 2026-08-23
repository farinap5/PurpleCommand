package teamapi

import (
	"net/http"
	"net/url"
	"time"
)

const (
	SessionTransportListener = "listener"
	SessionTransportSpeaker  = "speaker"
)

// Speaker is the public representation of an outbound bind-mode endpoint.
// Running means the speaker may dispatch commands; it does not represent a
// persistent network connection. InFlight is true only while a request is in
// progress.
type Speaker struct {
	Name          string        `json:"name"`
	UUID          string        `json:"uuid"`
	Running       bool          `json:"running"`
	InFlight      bool          `json:"in_flight"`
	Persistent    bool          `json:"persistent"`
	Session       string        `json:"session,omitempty"`
	LastAttemptAt time.Time     `json:"last_attempt_at,omitempty"`
	LastSuccessAt time.Time     `json:"last_success_at,omitempty"`
	LastError     string        `json:"last_error,omitempty"`
	Config        SpeakerConfig `json:"config"`
}

// SpeakerConfig contains the transport defaults and the template used for
// each command request.
type SpeakerConfig struct {
	Client  SpeakerHTTPClientConfig  `json:"client"`
	Request SpeakerHTTPRequestConfig `json:"request"`
}

// SpeakerCreateRequest creates a stopped speaker. Persistent defaults to true
// when omitted by the caller.
type SpeakerCreateRequest struct {
	Name       string        `json:"name"`
	Persistent *bool         `json:"persistent,omitempty"`
	Config     SpeakerConfig `json:"config"`
}

// SpeakerUpdateRequest atomically replaces the supplied speaker settings.
// Runtime fields such as Running and InFlight cannot be changed through this
// request. A nil field leaves its current value unchanged.
type SpeakerUpdateRequest struct {
	Name       string         `json:"name"`
	Persistent *bool          `json:"persistent,omitempty"`
	Config     *SpeakerConfig `json:"config,omitempty"`
}

// SpeakerTLSConfig controls server trust, optional mutual TLS, and certificate
// pinning for a speaker's HTTPS requests.
type SpeakerTLSConfig struct {
	ServerName         string   `json:"server_name,omitempty"`
	RootCAFile         string   `json:"root_ca_file,omitempty"`
	ClientCertFile     string   `json:"client_cert_file,omitempty"`
	ClientKeyFile      string   `json:"client_key_file,omitempty"`
	SPKISHA256Pins     []string `json:"spki_sha256_pins,omitempty"`
	MinVersion         string   `json:"min_version,omitempty"`
	InsecureSkipVerify bool     `json:"insecure_skip_verify,omitempty"`
}

// SpeakerHTTPClientConfig defines the endpoint and transport defaults shared
// by command requests. Header, query, and cookie values may contain security
// credentials and must not be written to logs.
type SpeakerHTTPClientConfig struct {
	BaseURL string            `json:"base_url"`
	Host    string            `json:"host,omitempty"`
	Headers http.Header       `json:"headers,omitempty"`
	Query   url.Values        `json:"query,omitempty"`
	Cookies map[string]string `json:"cookies,omitempty"`

	ProxyURL                  string `json:"proxy_url,omitempty"`
	UseEnvironmentProxy       bool   `json:"use_environment_proxy,omitempty"`
	FollowRedirects           bool   `json:"follow_redirects,omitempty"`
	AllowCrossOriginRedirects bool   `json:"allow_cross_origin_redirects,omitempty"`
	MaxRedirects              int    `json:"max_redirects,omitempty"`

	RequestTimeout         time.Duration `json:"request_timeout,omitempty"`
	DialTimeout            time.Duration `json:"dial_timeout,omitempty"`
	TLSHandshakeTimeout    time.Duration `json:"tls_handshake_timeout,omitempty"`
	ResponseHeaderTimeout  time.Duration `json:"response_header_timeout,omitempty"`
	IdleConnTimeout        time.Duration `json:"idle_connection_timeout,omitempty"`
	MaxRequestBytes        int64         `json:"max_request_bytes,omitempty"`
	MaxResponseBytes       int64         `json:"max_response_bytes,omitempty"`
	MaxResponseHeaderBytes int64         `json:"max_response_header_bytes,omitempty"`
	MaxIdleConnections     int           `json:"max_idle_connections,omitempty"`
	MaxIdlePerHost         int           `json:"max_idle_per_host,omitempty"`
	DisableCompression     bool          `json:"disable_compression,omitempty"`
	// ReuseConnections permits HTTP connection pooling but does not create a
	// persistent command channel. Every command still uses a new HTTP request.
	ReuseConnections bool `json:"reuse_connections,omitempty"`

	TLS SpeakerTLSConfig `json:"tls,omitempty"`
}

// SpeakerHTTPRequestConfig is the customizable portion of every command
// request. The command payload is supplied at dispatch time and is therefore
// intentionally absent from this persisted template.
type SpeakerHTTPRequestConfig struct {
	Method         string            `json:"method,omitempty"`
	Path           string            `json:"path,omitempty"`
	Host           string            `json:"host,omitempty"`
	Headers        http.Header       `json:"headers,omitempty"`
	Query          url.Values        `json:"query,omitempty"`
	Cookies        map[string]string `json:"cookies,omitempty"`
	ExpectedStatus []int             `json:"expected_status,omitempty"`
}
