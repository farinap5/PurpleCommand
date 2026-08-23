package speaker

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRequestEngineCustomRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !request.Close {
			t.Error("default request did not close its connection")
		}
		if request.Method != http.MethodPatch {
			t.Errorf("method = %q", request.Method)
		}
		if request.URL.Path != "/bind/exchange" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if request.Host != "cover.example:8443" {
			t.Errorf("host = %q", request.Host)
		}
		if got := request.URL.Query()["profile"]; len(got) != 2 || got[0] != "one" || got[1] != "two" {
			t.Errorf("profile query = %#v", got)
		}
		if got := request.URL.Query().Get("replace"); got != "request" {
			t.Errorf("replace query = %q", got)
		}
		if got := request.Header.Values("X-Default"); len(got) != 2 || got[0] != "one" || got[1] != "two" {
			t.Errorf("default header = %#v", got)
		}
		if got := request.Header.Values("X-Override"); len(got) != 1 || got[0] != "request" {
			t.Errorf("override header = %#v", got)
		}
		if cookie, err := request.Cookie("profile"); err != nil || cookie.Value != "default" {
			t.Errorf("profile cookie = %#v, %v", cookie, err)
		}
		if cookie, err := request.Cookie("replace"); err != nil || cookie.Value != "request" {
			t.Errorf("replace cookie = %#v, %v", cookie, err)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if string(body) != "request-body" {
			t.Errorf("body = %q", body)
		}
		writer.Header().Set("X-Response", "present")
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte("response-body"))
	}))
	defer server.Close()

	configuration := HTTPClientConfig{
		BaseURL: server.URL + "/base/",
		Host:    "cover.example:8443",
		Headers: http.Header{
			"x-default":  {"one", "two"},
			"x-override": {"default"},
		},
		Query: url.Values{
			"profile": {"one", "two"},
			"replace": {"default"},
		},
		Cookies: map[string]string{"profile": "default", "replace": "default"},
	}
	engine, err := NewRequestEngine(configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.CloseIdleConnections()

	// Construction must detach the engine from mutable caller-owned defaults.
	configuration.Headers.Set("X-Default", "mutated")
	configuration.Query.Set("profile", "mutated")
	configuration.Cookies["profile"] = "mutated"

	response, err := engine.Do(context.Background(), RequestSpec{
		Method:         http.MethodPatch,
		Path:           "/bind/exchange?path=value",
		Headers:        http.Header{"X-Override": {"request"}},
		Query:          url.Values{"replace": {"request"}},
		Cookies:        map[string]string{"replace": "request"},
		Body:           []byte("request-body"),
		ExpectedStatus: []int{http.StatusCreated},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated || string(response.Body) != "response-body" {
		t.Fatalf("response = %#v", response)
	}
	if response.Headers.Get("X-Response") != "present" {
		t.Fatalf("response headers = %#v", response.Headers)
	}
}

func TestRequestEngineConnectionReuseIsOptIn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Close {
			t.Error("opt-in reusable request asked to close its connection")
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	engine, err := NewRequestEngine(HTTPClientConfig{
		BaseURL:          server.URL,
		ReuseConnections: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.CloseIdleConnections()
	if _, err := engine.Do(context.Background(), RequestSpec{}); err != nil {
		t.Fatal(err)
	}
}

func TestRequestEngineLimitsAndValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/large":
			_, _ = writer.Write([]byte("response-too-large"))
		case "/exact":
			_, _ = writer.Write([]byte("12345678"))
		case "/status":
			http.Error(writer, "missing", http.StatusNotFound)
		default:
			writer.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	engine, err := NewRequestEngine(HTTPClientConfig{
		BaseURL:          server.URL,
		MaxRequestBytes:  4,
		MaxResponseBytes: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.CloseIdleConnections()

	if _, err := engine.Do(context.Background(), RequestSpec{Body: []byte("12345")}); !errors.Is(err, ErrRequestTooLarge) {
		t.Fatalf("request limit error = %v", err)
	}
	response, err := engine.Do(context.Background(), RequestSpec{Path: "/large"})
	if !errors.Is(err, ErrResponseTooLarge) || len(response.Body) != 8 {
		t.Fatalf("large response = %#v, %v", response, err)
	}
	response, err = engine.Do(context.Background(), RequestSpec{Path: "/exact"})
	if err != nil || string(response.Body) != "12345678" {
		t.Fatalf("exact-size response = %#v, %v", response, err)
	}
	response, err = engine.Do(context.Background(), RequestSpec{Path: "/status"})
	var statusError *UnexpectedStatusError
	if !errors.As(err, &statusError) || response.StatusCode != http.StatusNotFound {
		t.Fatalf("status response = %#v, %v", response, err)
	}
	if _, err := engine.Do(context.Background(), RequestSpec{Path: "https://example.invalid/override"}); err == nil {
		t.Fatal("absolute request path was accepted")
	}
	if _, err := engine.Do(context.Background(), RequestSpec{Headers: http.Header{"X-Test": {"value\r\nInjected: yes"}}}); err == nil {
		t.Fatal("header newline was accepted")
	}
	if _, err := engine.Do(context.Background(), RequestSpec{Host: "valid.example\r\nInjected: yes"}); err == nil {
		t.Fatal("host newline was accepted")
	}
	if _, err := engine.Do(context.Background(), RequestSpec{Cookies: map[string]string{"key": "bad\\value"}}); err == nil {
		t.Fatal("invalid cookie value was accepted")
	}
}

func TestRequestEngineRedirectPolicy(t *testing.T) {
	var finalRequests int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/redirect" {
			http.Redirect(writer, request, "/final", http.StatusFound)
			return
		}
		if request.Host != "cover.example" {
			t.Errorf("redirected host = %q", request.Host)
		}
		atomic.AddInt32(&finalRequests, 1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	engine, err := NewRequestEngine(HTTPClientConfig{BaseURL: server.URL, Host: "cover.example"})
	if err != nil {
		t.Fatal(err)
	}
	response, err := engine.Do(context.Background(), RequestSpec{Path: "/redirect", ExpectedStatus: []int{http.StatusFound}})
	if err != nil || response.StatusCode != http.StatusFound || atomic.LoadInt32(&finalRequests) != 0 {
		t.Fatalf("disabled redirect response = %#v, requests=%d, err=%v", response, atomic.LoadInt32(&finalRequests), err)
	}

	following, err := NewRequestEngine(HTTPClientConfig{
		BaseURL:         server.URL,
		Host:            "cover.example",
		FollowRedirects: true,
		MaxRedirects:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err = following.Do(context.Background(), RequestSpec{Path: "/redirect"})
	if err != nil || response.StatusCode != http.StatusNoContent || atomic.LoadInt32(&finalRequests) != 1 {
		t.Fatalf("enabled redirect response = %#v, requests=%d, err=%v", response, atomic.LoadInt32(&finalRequests), err)
	}
}

func TestRequestEngineCrossOriginRedirectRequiresOptIn(t *testing.T) {
	var targetRequests int32
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&targetRequests, 1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	defer source.Close()

	engine, err := NewRequestEngine(HTTPClientConfig{
		BaseURL:         source.URL,
		FollowRedirects: true,
		MaxRedirects:    1,
		Headers:         http.Header{"X-Security-Key": {"secret"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Do(context.Background(), RequestSpec{}); err == nil || !strings.Contains(err.Error(), "cross-origin") {
		t.Fatalf("cross-origin redirect error = %v", err)
	}
	if got := atomic.LoadInt32(&targetRequests); got != 0 {
		t.Fatalf("cross-origin target received %d requests", got)
	}

	allowed, err := NewRequestEngine(HTTPClientConfig{
		BaseURL:                   source.URL,
		FollowRedirects:           true,
		AllowCrossOriginRedirects: true,
		MaxRedirects:              1,
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := allowed.Do(context.Background(), RequestSpec{})
	if err != nil || response.StatusCode != http.StatusNoContent {
		t.Fatalf("allowed redirect response = %#v, err=%v", response, err)
	}
	if got := atomic.LoadInt32(&targetRequests); got != 1 {
		t.Fatalf("allowed target received %d requests", got)
	}
}

func TestRequestEngineTLSRootAndSPKIPin(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	certificate := server.Certificate()
	caFile := t.TempDir() + "/ca.pem"
	pemData := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})
	if err := os.WriteFile(caFile, pemData, 0600); err != nil {
		t.Fatal(err)
	}
	pin := sha256.Sum256(certificate.RawSubjectPublicKeyInfo)
	pinValue := "sha256/" + base64.StdEncoding.EncodeToString(pin[:])
	engine, err := NewRequestEngine(HTTPClientConfig{
		BaseURL: server.URL,
		TLS: TLSClientConfig{
			RootCAFile:     caFile,
			SPKISHA256Pins: []string{pinValue},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Do(context.Background(), RequestSpec{}); err != nil {
		t.Fatal(err)
	}

	wrongPin := strings.Repeat("00", sha256.Size)
	wrong, err := NewRequestEngine(HTTPClientConfig{
		BaseURL: server.URL,
		TLS:     TLSClientConfig{RootCAFile: caFile, SPKISHA256Pins: []string{wrongPin}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrong.Do(context.Background(), RequestSpec{}); err == nil || !strings.Contains(err.Error(), "SPKI pin") {
		t.Fatalf("wrong pin error = %v", err)
	}
}

func TestRequestEngineConfigurationValidation(t *testing.T) {
	tests := []struct {
		name          string
		configuration HTTPClientConfig
	}{
		{name: "missing URL", configuration: HTTPClientConfig{}},
		{name: "URL credentials", configuration: HTTPClientConfig{BaseURL: "https://user:pass@example.test"}},
		{name: "unsupported URL", configuration: HTTPClientConfig{BaseURL: "ftp://example.test"}},
		{name: "invalid proxy", configuration: HTTPClientConfig{BaseURL: "https://example.test", ProxyURL: "file:///tmp/proxy"}},
		{name: "invalid TLS version", configuration: HTTPClientConfig{BaseURL: "https://example.test", TLS: TLSClientConfig{MinVersion: "1.1"}}},
		{name: "unpaired client cert", configuration: HTTPClientConfig{BaseURL: "https://example.test", TLS: TLSClientConfig{ClientCertFile: "cert.pem"}}},
		{name: "Host header", configuration: HTTPClientConfig{BaseURL: "https://example.test", Headers: http.Header{"Host": {"cover.test"}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewRequestEngine(test.configuration); err == nil {
				t.Fatal("invalid configuration was accepted")
			}
		})
	}
}

func ExampleRequestEngine() {
	engine, err := NewRequestEngine(HTTPClientConfig{
		BaseURL: "https://bind.example:8443",
		Headers: http.Header{"User-Agent": {"PurpleCommand"}},
	})
	if err != nil {
		return
	}
	defer engine.CloseIdleConnections()

	response, err := engine.Do(context.Background(), RequestSpec{
		Method:         http.MethodPost,
		Path:           "/exchange",
		Body:           []byte("protocol-frame"),
		ExpectedStatus: []int{http.StatusOK, http.StatusNoContent},
	})
	if err == nil {
		fmt.Println(response.Status)
	}
}
