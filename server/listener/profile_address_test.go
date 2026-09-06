package listener

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func profileListenerSnapshot(options string) ManagedListenerSnapshot {
	return ManagedListenerSnapshot{Config: ManagedListenerConfig{
		Name: "callback", UUID: "listener-id", Driver: "http", Persistent: true,
		Options: json.RawMessage(options),
	}}
}

func TestHTTPProfileLHOSTScopedIPv6IsURLSafe(t *testing.T) {
	lhost, err := HTTPProfileLHOST(profileListenerSnapshot(
		`{"bind":{"host":"127.0.0.1","port":"8080"},"advertise":{"host":"fe80::1%eth0","port":"8443"}}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse("http://" + lhost + "/")
	if err != nil {
		t.Fatalf("materialized LHOST is not URL-safe: %q: %v", lhost, err)
	}
	if parsed.Hostname() != "fe80::1%eth0" || parsed.Port() != "8443" {
		t.Fatalf("parsed callback URL = %#v", parsed)
	}
}

func TestHTTPProfileLHOSTUsesAdvertisementAndFormatsIPv6(t *testing.T) {
	tests := []struct {
		name    string
		options string
		want    string
	}{
		{
			name: "advertisement overrides bind",
			options: `{"bind":{"host":"127.0.0.1","port":"8080"},` +
				`"advertise":{"host":"callback.example","port":"4444"}}`,
			want: "callback.example:4444",
		},
		{
			name:    "bind is the documented fallback",
			options: `{"bind":{"host":"127.0.0.1","port":"8080"}}`,
			want:    "127.0.0.1:8080",
		},
		{
			name: "IPv6 is bracketed once",
			options: `{"bind":{"host":"127.0.0.1","port":"8080"},` +
				`"advertise":{"host":"[2001:db8::7]","port":"8443"}}`,
			want: "[2001:db8::7]:8443",
		},
		{
			name: "unbracketed scoped IPv6",
			options: `{"bind":{"host":"127.0.0.1","port":"8080"},` +
				`"advertise":{"host":"fe80::1%eth0","port":"8443"}}`,
			want: "[fe80::1%25eth0]:8443",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := HTTPProfileLHOST(profileListenerSnapshot(test.options))
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("LHOST = %q, want %q", got, test.want)
			}
		})
	}
}

func TestHTTPProfileLHOSTRejectsUnsupportedListenerConfigurations(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*ManagedListenerSnapshot)
		wantError string
	}{
		{name: "different driver", mutate: func(snapshot *ManagedListenerSnapshot) { snapshot.Config.Driver = "dns" }, wantError: "not compatible"},
		{name: "ephemeral listener", mutate: func(snapshot *ManagedListenerSnapshot) { snapshot.Config.Persistent = false }, wantError: "persistent"},
		{name: "custom routes", mutate: func(snapshot *ManagedListenerSnapshot) { snapshot.Config.Routes = []Route{{ID: "custom"}} }, wantError: "default routes"},
		{name: "TLS", mutate: func(snapshot *ManagedListenerSnapshot) {
			snapshot.Config.Options = json.RawMessage(`{"bind":{"host":"127.0.0.1","port":"8443"},"tls":{"enabled":true,"cert_file":"cert","key_file":"key"}}`)
		}, wantError: "does not support HTTPS"},
		{name: "wildcard IPv4", mutate: func(snapshot *ManagedListenerSnapshot) {
			snapshot.Config.Options = json.RawMessage(`{"bind":{"host":"0.0.0.0","port":"4444"}}`)
		}, wantError: "wildcard"},
		{name: "wildcard IPv6", mutate: func(snapshot *ManagedListenerSnapshot) {
			snapshot.Config.Options = json.RawMessage(`{"bind":{"host":"::","port":"4444"}}`)
		}, wantError: "wildcard"},
		{name: "scoped wildcard IPv6", mutate: func(snapshot *ManagedListenerSnapshot) {
			snapshot.Config.Options = json.RawMessage(`{"bind":{"host":"::%eth0","port":"4444"}}`)
		}, wantError: "wildcard"},
		{name: "bracketed scoped wildcard IPv6", mutate: func(snapshot *ManagedListenerSnapshot) {
			snapshot.Config.Options = json.RawMessage(`{"bind":{"host":"[::%eth0]","port":"4444"}}`)
		}, wantError: "wildcard"},
		{name: "expanded scoped wildcard IPv6", mutate: func(snapshot *ManagedListenerSnapshot) {
			snapshot.Config.Options = json.RawMessage(`{"bind":{"host":"0:0:0:0:0:0:0:0%eth0","port":"4444"}}`)
		}, wantError: "wildcard"},
		{name: "IPv4-mapped wildcard", mutate: func(snapshot *ManagedListenerSnapshot) {
			snapshot.Config.Options = json.RawMessage(`{"bind":{"host":"::ffff:0.0.0.0","port":"4444"}}`)
		}, wantError: "wildcard"},
		{name: "bracketed IPv4-mapped wildcard", mutate: func(snapshot *ManagedListenerSnapshot) {
			snapshot.Config.Options = json.RawMessage(`{"bind":{"host":"[::ffff:0:0]","port":"4444"}}`)
		}, wantError: "wildcard"},
		{name: "ephemeral port", mutate: func(snapshot *ManagedListenerSnapshot) {
			snapshot.Config.Options = json.RawMessage(`{"bind":{"host":"127.0.0.1","port":"0"}}`)
		}, wantError: "between 1 and 65535"},
		{name: "embedded port", mutate: func(snapshot *ManagedListenerSnapshot) {
			snapshot.Config.Options = json.RawMessage(`{"bind":{"host":"callback.example:8080","port":"4444"}}`)
		}, wantError: "not a valid IPv6"},
		{name: "URL", mutate: func(snapshot *ManagedListenerSnapshot) {
			snapshot.Config.Options = json.RawMessage(`{"bind":{"host":"http://callback.example","port":"4444"}}`)
		}, wantError: "without a URL"},
		{name: "path", mutate: func(snapshot *ManagedListenerSnapshot) {
			snapshot.Config.Options = json.RawMessage(`{"bind":{"host":"callback.example/path","port":"4444"}}`)
		}, wantError: "without a URL"},
		{name: "control character", mutate: func(snapshot *ManagedListenerSnapshot) {
			snapshot.Config.Options = json.RawMessage("{\"bind\":{\"host\":\"callback.example\\ninvalid\",\"port\":\"4444\"}}")
		}, wantError: "whitespace"},
		{name: "unmatched opening bracket", mutate: func(snapshot *ManagedListenerSnapshot) {
			snapshot.Config.Options = json.RawMessage(`{"bind":{"host":"[2001:db8::1","port":"4444"}}`)
		}, wantError: "unmatched"},
		{name: "unmatched closing bracket", mutate: func(snapshot *ManagedListenerSnapshot) {
			snapshot.Config.Options = json.RawMessage(`{"bind":{"host":"2001:db8::1]","port":"4444"}}`)
		}, wantError: "unmatched"},
		{name: "IPv4 zone", mutate: func(snapshot *ManagedListenerSnapshot) {
			snapshot.Config.Options = json.RawMessage(`{"bind":{"host":"127.0.0.1%eth0","port":"4444"}}`)
		}, wantError: "not a valid IPv6"},
		{name: "empty zone", mutate: func(snapshot *ManagedListenerSnapshot) {
			snapshot.Config.Options = json.RawMessage(`{"bind":{"host":"fe80::1%","port":"4444"}}`)
		}, wantError: "empty IPv6 zone"},
		{name: "repeated zone", mutate: func(snapshot *ManagedListenerSnapshot) {
			snapshot.Config.Options = json.RawMessage(`{"bind":{"host":"fe80::1%eth0%extra","port":"4444"}}`)
		}, wantError: "invalid IPv6 zone"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := profileListenerSnapshot(`{"bind":{"host":"127.0.0.1","port":"4444"}}`)
			test.mutate(&snapshot)
			_, err := HTTPProfileLHOST(snapshot)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want containing %q", err, test.wantError)
			}
		})
	}
}
