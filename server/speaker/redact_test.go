package speaker

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"purpcmd/pkg/teamapi"
)

func TestRedactRemovesCredentialValuesWithoutMutatingRuntimeConfig(t *testing.T) {
	item := teamapi.Speaker{LastError: "request https://example.test/?token=query-secret failed with Bearer secret", Config: teamapi.SpeakerConfig{
		Client: teamapi.SpeakerHTTPClientConfig{
			Headers: http.Header{"Authorization": {"Bearer secret"}},
			Query:   url.Values{"token": {"query-secret"}}, Cookies: map[string]string{"session": "cookie-secret"},
			ProxyURL: "http://user:password@proxy.example", TLS: teamapi.SpeakerTLSConfig{ClientKeyFile: "/secret/key.pem"},
		},
		Request: teamapi.SpeakerHTTPRequestConfig{
			Headers: http.Header{"X-Token": {"request-secret"}},
			Query:   url.Values{"key": {"request-query"}}, Cookies: map[string]string{"auth": "request-cookie"},
		},
	}}
	redacted := Redact(item)
	if redacted.Config.Client.Headers.Get("Authorization") != redactedValue ||
		redacted.Config.Client.Query.Get("token") != redactedValue ||
		redacted.Config.Client.Cookies["session"] != redactedValue ||
		redacted.Config.Client.ProxyURL != redactedValue ||
		redacted.Config.Client.TLS.ClientKeyFile != redactedValue ||
		redacted.Config.Request.Headers.Get("X-Token") != redactedValue ||
		redacted.Config.Request.Query.Get("key") != redactedValue ||
		redacted.Config.Request.Cookies["auth"] != redactedValue ||
		strings.Contains(redacted.LastError, "query-secret") || strings.Contains(redacted.LastError, "Bearer secret") {
		t.Fatalf("redacted speaker = %#v", redacted)
	}
	if item.Config.Client.Headers.Get("Authorization") != "Bearer secret" || item.Config.Request.Cookies["auth"] != "request-cookie" {
		t.Fatal("redaction mutated the runtime configuration")
	}
}
