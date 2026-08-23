package teamapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"
)

func TestSpeakerCreateRequestRoundTrip(t *testing.T) {
	persistent := false
	expected := SpeakerCreateRequest{
		Name:       "bind-http",
		Persistent: &persistent,
		Config: SpeakerConfig{
			Client: SpeakerHTTPClientConfig{
				BaseURL:        "https://bind.example:8443/base",
				Headers:        http.Header{"X-Security-Key": {"secret"}},
				Query:          url.Values{"profile": {"one", "two"}},
				Cookies:        map[string]string{"session": "value"},
				RequestTimeout: 15 * time.Second,
				TLS: SpeakerTLSConfig{
					ServerName:     "bind.example",
					SPKISHA256Pins: []string{"sha256/example"},
				},
			},
			Request: SpeakerHTTPRequestConfig{
				Method:         http.MethodPost,
				Path:           "/command",
				ExpectedStatus: []int{http.StatusOK, http.StatusNoContent},
			},
		},
	}

	data, err := MarshalData(expected)
	if err != nil {
		t.Fatal(err)
	}
	var actual SpeakerCreateRequest
	if err := DecodeData(Envelope{Data: data}, &actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("round trip = %#v, want %#v", actual, expected)
	}
}

func TestSpeakerRequestRejectsUnknownNestedFields(t *testing.T) {
	data := json.RawMessage(`{
		"name":"bind-http",
		"config":{
			"client":{"base_url":"https://bind.example","unknown":true},
			"request":{}
		}
	}`)
	var request SpeakerCreateRequest
	if err := DecodeData(Envelope{Data: data}, &request); err == nil {
		t.Fatal("unknown nested field was accepted")
	}
}
