// Package speaker contains the outbound transport foundation used by bind-mode
// implant connections. Each RequestEngine.Do call is one finite request and
// response exchange. Protocol registration, task framing, and future
// interactive transports intentionally do not live in this package.
package speaker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"purpcmd/pkg/teamapi"
)

const (
	defaultRequestTimeout        = 30 * time.Second
	defaultDialTimeout           = 10 * time.Second
	defaultTLSHandshakeTimeout   = 10 * time.Second
	defaultResponseHeaderTimeout = 15 * time.Second
	defaultIdleConnTimeout       = 90 * time.Second
	defaultMaxRequestBytes       = int64(10 << 20)
	defaultMaxResponseBytes      = int64(10 << 20)
	defaultMaxResponseHeaders    = int64(64 << 10)
	defaultMaxRedirects          = 5
)

var (
	ErrRequestTooLarge  = errors.New("speaker HTTP request exceeds configured limit")
	ErrResponseTooLarge = errors.New("speaker HTTP response exceeds configured limit")
)

// TLSClientConfig and HTTPClientConfig remain aliases for callers working in
// the transport package. Their canonical wire definitions live in teamapi.
type TLSClientConfig = teamapi.SpeakerTLSConfig
type HTTPClientConfig = teamapi.SpeakerHTTPClientConfig

// RequestSpec describes one outbound request. Request-specific headers, query
// values, cookies, and Host replace defaults with the same key.
type RequestSpec struct {
	Method         string
	Path           string
	Host           string
	Headers        http.Header
	Query          url.Values
	Cookies        map[string]string
	Body           []byte
	ExpectedStatus []int
}

// Response is a bounded, detached HTTP response. The underlying network body
// is always closed before Do returns.
type Response struct {
	StatusCode int
	Status     string
	Headers    http.Header
	Body       []byte
}

// UnexpectedStatusError is returned with a populated Response when the status
// does not match RequestSpec.ExpectedStatus. With no explicit list, any 2xx
// response is accepted.
type UnexpectedStatusError struct {
	StatusCode int
	Status     string
}

func (err *UnexpectedStatusError) Error() string {
	return fmt.Sprintf("unexpected speaker HTTP status: %s", err.Status)
}

// RequestEngine owns a configured, concurrency-safe HTTP client.
type RequestEngine struct {
	baseURL           *url.URL
	host              string
	headers           http.Header
	query             url.Values
	cookies           map[string]string
	maxRequestBytes   int64
	maxResponseBytes  int64
	closeAfterRequest bool
	client            *http.Client
	transport         *http.Transport
}

func NewRequestEngine(configuration HTTPClientConfig) (*RequestEngine, error) {
	baseURL, err := parseBaseURL(configuration.BaseURL)
	if err != nil {
		return nil, err
	}
	if err := validateHeaders(configuration.Headers); err != nil {
		return nil, fmt.Errorf("default headers: %w", err)
	}
	if err := validateCookies(configuration.Cookies); err != nil {
		return nil, fmt.Errorf("default cookies: %w", err)
	}
	if err := validateHost(configuration.Host); err != nil {
		return nil, fmt.Errorf("default host: %w", err)
	}

	tlsConfiguration, err := buildTLSConfig(configuration.TLS)
	if err != nil {
		return nil, err
	}
	proxy, err := proxyFunction(configuration.ProxyURL, configuration.UseEnvironmentProxy)
	if err != nil {
		return nil, err
	}

	requestTimeout := durationOrDefault(configuration.RequestTimeout, defaultRequestTimeout)
	dialTimeout := durationOrDefault(configuration.DialTimeout, defaultDialTimeout)
	tlsHandshakeTimeout := durationOrDefault(configuration.TLSHandshakeTimeout, defaultTLSHandshakeTimeout)
	responseHeaderTimeout := durationOrDefault(configuration.ResponseHeaderTimeout, defaultResponseHeaderTimeout)
	idleConnTimeout := durationOrDefault(configuration.IdleConnTimeout, defaultIdleConnTimeout)
	maxRequestBytes := sizeOrDefault(configuration.MaxRequestBytes, defaultMaxRequestBytes)
	maxResponseBytes := sizeOrDefault(configuration.MaxResponseBytes, defaultMaxResponseBytes)
	maxResponseHeaders := sizeOrDefault(configuration.MaxResponseHeaderBytes, defaultMaxResponseHeaders)
	maxIdleConnections := configuration.MaxIdleConnections
	if maxIdleConnections <= 0 {
		maxIdleConnections = 100
	}
	maxIdlePerHost := configuration.MaxIdlePerHost
	if maxIdlePerHost <= 0 {
		maxIdlePerHost = 10
	}

	transport := &http.Transport{
		Proxy:                  proxy,
		DialContext:            (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           maxIdleConnections,
		MaxIdleConnsPerHost:    maxIdlePerHost,
		IdleConnTimeout:        idleConnTimeout,
		TLSHandshakeTimeout:    tlsHandshakeTimeout,
		ResponseHeaderTimeout:  responseHeaderTimeout,
		MaxResponseHeaderBytes: maxResponseHeaders,
		TLSClientConfig:        tlsConfiguration,
		DisableCompression:     configuration.DisableCompression,
		DisableKeepAlives:      !configuration.ReuseConnections,
	}
	client := &http.Client{Transport: transport, Timeout: requestTimeout}
	client.CheckRedirect = redirectPolicy(
		configuration.FollowRedirects,
		configuration.AllowCrossOriginRedirects,
		configuration.MaxRedirects,
	)

	return &RequestEngine{
		baseURL:           baseURL,
		host:              strings.TrimSpace(configuration.Host),
		headers:           normalizeHeader(configuration.Headers),
		query:             cloneValues(configuration.Query),
		cookies:           cloneStrings(configuration.Cookies),
		maxRequestBytes:   maxRequestBytes,
		maxResponseBytes:  maxResponseBytes,
		closeAfterRequest: !configuration.ReuseConnections,
		client:            client,
		transport:         transport,
	}, nil
}

func (engine *RequestEngine) Do(ctx context.Context, specification RequestSpec) (Response, error) {
	if ctx == nil {
		return Response{}, errors.New("request context is required")
	}
	if int64(len(specification.Body)) > engine.maxRequestBytes {
		return Response{}, ErrRequestTooLarge
	}
	if err := validateHeaders(specification.Headers); err != nil {
		return Response{}, fmt.Errorf("request headers: %w", err)
	}
	if err := validateCookies(specification.Cookies); err != nil {
		return Response{}, fmt.Errorf("request cookies: %w", err)
	}
	if err := validateHost(specification.Host); err != nil {
		return Response{}, fmt.Errorf("request host: %w", err)
	}

	endpoint, err := engine.resolve(specification.Path, specification.Query)
	if err != nil {
		return Response{}, err
	}
	method := strings.TrimSpace(specification.Method)
	if method == "" {
		method = http.MethodGet
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(specification.Body))
	if err != nil {
		return Response{}, err
	}
	request.Header = mergeHeader(engine.headers, specification.Headers)
	request.Close = engine.closeAfterRequest
	request.Host = engine.host
	if specification.Host != "" {
		request.Host = strings.TrimSpace(specification.Host)
	}
	for name, value := range mergeStrings(engine.cookies, specification.Cookies) {
		request.AddCookie(&http.Cookie{Name: name, Value: value})
	}

	networkResponse, err := engine.client.Do(request)
	if err != nil {
		return Response{}, err
	}
	defer networkResponse.Body.Close()
	body, err := readBounded(networkResponse.Body, engine.maxResponseBytes)
	response := Response{
		StatusCode: networkResponse.StatusCode,
		Status:     networkResponse.Status,
		Headers:    cloneHeader(networkResponse.Header),
		Body:       body,
	}
	if err != nil {
		return response, err
	}
	if !statusAccepted(networkResponse.StatusCode, specification.ExpectedStatus) {
		return response, &UnexpectedStatusError{StatusCode: networkResponse.StatusCode, Status: networkResponse.Status}
	}
	return response, nil
}

// CloseIdleConnections releases pooled connections without making the engine
// unusable. Active requests are unaffected.
func (engine *RequestEngine) CloseIdleConnections() {
	if engine != nil && engine.transport != nil {
		engine.transport.CloseIdleConnections()
	}
}

func (engine *RequestEngine) resolve(path string, requestQuery url.Values) (*url.URL, error) {
	result := *engine.baseURL
	if path != "" {
		reference, err := url.Parse(path)
		if err != nil {
			return nil, fmt.Errorf("parse request path: %w", err)
		}
		if reference.IsAbs() || reference.Host != "" || reference.User != nil {
			return nil, errors.New("request path must not override the configured endpoint")
		}
		if reference.Fragment != "" {
			return nil, errors.New("request path must not contain a fragment")
		}
		resolved := engine.baseURL.ResolveReference(reference)
		result = *resolved
	}
	query := result.Query()
	overrideValues(query, engine.query)
	overrideValues(query, requestQuery)
	result.RawQuery = query.Encode()
	return &result, nil
}

func parseBaseURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return nil, fmt.Errorf("parse speaker base URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("speaker base URL scheme must be http or https")
	}
	if parsed.Host == "" {
		return nil, errors.New("speaker base URL host is required")
	}
	if parsed.User != nil {
		return nil, errors.New("speaker base URL must not contain credentials")
	}
	if parsed.Fragment != "" {
		return nil, errors.New("speaker base URL must not contain a fragment")
	}
	return parsed, nil
}

func buildTLSConfig(configuration TLSClientConfig) (*tls.Config, error) {
	minimumVersion, err := tlsVersion(configuration.MinVersion)
	if err != nil {
		return nil, err
	}
	tlsConfiguration := &tls.Config{
		MinVersion:         minimumVersion,
		ServerName:         strings.TrimSpace(configuration.ServerName),
		InsecureSkipVerify: configuration.InsecureSkipVerify,
	}
	if configuration.RootCAFile != "" {
		pemData, err := os.ReadFile(configuration.RootCAFile)
		if err != nil {
			return nil, fmt.Errorf("read root CA: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pemData) {
			return nil, errors.New("root CA file contains no certificates")
		}
		tlsConfiguration.RootCAs = pool
	}
	if (configuration.ClientCertFile == "") != (configuration.ClientKeyFile == "") {
		return nil, errors.New("client certificate and key files must be provided together")
	}
	if configuration.ClientCertFile != "" {
		certificate, err := tls.LoadX509KeyPair(configuration.ClientCertFile, configuration.ClientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client certificate: %w", err)
		}
		tlsConfiguration.Certificates = []tls.Certificate{certificate}
	}
	pins, err := parsePins(configuration.SPKISHA256Pins)
	if err != nil {
		return nil, err
	}
	if len(pins) > 0 {
		tlsConfiguration.VerifyConnection = func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return errors.New("TLS peer did not provide a certificate")
			}
			actual := sha256.Sum256(state.PeerCertificates[0].RawSubjectPublicKeyInfo)
			if _, ok := pins[actual]; !ok {
				return errors.New("TLS peer SPKI pin does not match")
			}
			return nil
		}
	}
	return tlsConfiguration, nil
}

func tlsVersion(value string) (uint16, error) {
	switch strings.TrimSpace(value) {
	case "", "1.2", "TLS1.2", "tls1.2":
		return tls.VersionTLS12, nil
	case "1.3", "TLS1.3", "tls1.3":
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("unsupported minimum TLS version %q", value)
	}
}

func parsePins(values []string) (map[[sha256.Size]byte]struct{}, error) {
	pins := make(map[[sha256.Size]byte]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "sha256/"))
		var decoded []byte
		var err error
		if len(value) == sha256.Size*2 {
			decoded, err = hex.DecodeString(value)
		} else {
			decoded, err = base64.StdEncoding.DecodeString(value)
			if err != nil {
				decoded, err = base64.RawStdEncoding.DecodeString(value)
			}
		}
		if err != nil || len(decoded) != sha256.Size {
			return nil, fmt.Errorf("invalid SHA-256 SPKI pin %q", value)
		}
		var pin [sha256.Size]byte
		copy(pin[:], decoded)
		pins[pin] = struct{}{}
	}
	return pins, nil
}

func proxyFunction(proxyValue string, useEnvironment bool) (func(*http.Request) (*url.URL, error), error) {
	if proxyValue != "" {
		parsed, err := url.Parse(strings.TrimSpace(proxyValue))
		if err != nil {
			return nil, fmt.Errorf("parse proxy URL: %w", err)
		}
		if parsed.Host == "" {
			return nil, errors.New("proxy URL host is required")
		}
		switch parsed.Scheme {
		case "http", "https", "socks5", "socks5h":
		default:
			return nil, fmt.Errorf("unsupported proxy URL scheme %q", parsed.Scheme)
		}
		return http.ProxyURL(parsed), nil
	}
	if useEnvironment {
		return http.ProxyFromEnvironment, nil
	}
	return nil, nil
}

func redirectPolicy(follow, allowCrossOrigin bool, maximum int) func(*http.Request, []*http.Request) error {
	if !follow {
		return func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	if maximum <= 0 {
		maximum = defaultMaxRedirects
	}
	return func(request *http.Request, previous []*http.Request) error {
		if len(previous) > maximum {
			return fmt.Errorf("stopped after %d redirects", maximum)
		}
		if len(previous) == 0 {
			return nil
		}
		prior := previous[len(previous)-1]
		if !sameOrigin(request.URL, prior.URL) {
			if !allowCrossOrigin {
				return errors.New("cross-origin speaker HTTP redirect is not allowed")
			}
			return nil
		}
		// net/http does not retain an explicit Host override while constructing
		// redirect requests. Keep it for redirects within the same origin.
		request.Host = prior.Host
		return nil
	}
}

func sameOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(originHost(left), originHost(right))
}

func originHost(value *url.URL) string {
	port := value.Port()
	if port == "" {
		switch strings.ToLower(value.Scheme) {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	return net.JoinHostPort(value.Hostname(), port)
}

func readBounded(reader io.Reader, maximum int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maximum))
	if err != nil {
		return nil, err
	}
	var extra [1]byte
	count, err := io.ReadFull(reader, extra[:])
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return data, err
	}
	if count != 0 {
		return data, ErrResponseTooLarge
	}
	return data, nil
}

func statusAccepted(status int, expected []int) bool {
	if len(expected) == 0 {
		return status >= http.StatusOK && status < http.StatusMultipleChoices
	}
	for _, candidate := range expected {
		if status == candidate {
			return true
		}
	}
	return false
}

func validateHeaders(headers http.Header) error {
	for name, values := range headers {
		if !validToken(name) {
			return fmt.Errorf("invalid header name %q", name)
		}
		if strings.EqualFold(name, "Host") {
			return errors.New("Host must be configured through the Host field")
		}
		for _, value := range values {
			if !validHeaderValue(value) {
				return fmt.Errorf("header %q contains an invalid value", name)
			}
		}
	}
	return nil
}

func validateHost(host string) error {
	for index := 0; index < len(host); index++ {
		character := host[index]
		if character <= 0x20 || character > 0x7e || strings.ContainsRune("/\\@?#", rune(character)) {
			return errors.New("host override contains an invalid character")
		}
	}
	return nil
}

func validateCookies(cookies map[string]string) error {
	for name, value := range cookies {
		if !validToken(name) {
			return fmt.Errorf("invalid cookie name %q", name)
		}
		if !validCookieValue(value) {
			return fmt.Errorf("invalid cookie value for %q", name)
		}
	}
	return nil
}

func validToken(value string) bool {
	if value == "" {
		return false
	}
	const separators = "()<>@,;:\\\"/[]?={} \t"
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character < 0x21 || character > 0x7e || strings.ContainsRune(separators, rune(character)) {
			return false
		}
	}
	return true
}

func validHeaderValue(value string) bool {
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character != '\t' && (character < 0x20 || character == 0x7f) {
			return false
		}
	}
	return true
}

func validCookieValue(value string) bool {
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character < 0x20 || character >= 0x7f || character == '"' || character == ';' || character == '\\' {
			return false
		}
	}
	return true
}

func cloneHeader(source http.Header) http.Header {
	if source == nil {
		return make(http.Header)
	}
	return source.Clone()
}

func mergeHeader(defaults, overrides http.Header) http.Header {
	result := normalizeHeader(defaults)
	for name, values := range overrides {
		name = http.CanonicalHeaderKey(name)
		delete(result, name)
		for _, value := range values {
			result.Add(name, value)
		}
	}
	return result
}

func normalizeHeader(source http.Header) http.Header {
	result := make(http.Header, len(source))
	for name, values := range source {
		name = http.CanonicalHeaderKey(name)
		for _, value := range values {
			result.Add(name, value)
		}
	}
	return result
}

func cloneValues(source url.Values) url.Values {
	result := make(url.Values, len(source))
	for name, values := range source {
		result[name] = append([]string(nil), values...)
	}
	return result
}

func overrideValues(destination, source url.Values) {
	for name, values := range source {
		destination.Del(name)
		for _, value := range values {
			destination.Add(name, value)
		}
	}
}

func cloneStrings(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for name, value := range source {
		result[name] = value
	}
	return result
}

func mergeStrings(defaults, overrides map[string]string) map[string]string {
	result := cloneStrings(defaults)
	for name, value := range overrides {
		result[name] = value
	}
	return result
}

func durationOrDefault(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

func sizeOrDefault(value, fallback int64) int64 {
	if value <= 0 {
		return fallback
	}
	return value
}
