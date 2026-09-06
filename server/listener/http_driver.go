package listener

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/textproto"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"purpcmd/internal"
	"purpcmd/server/interactive"
	"purpcmd/server/utils"

	"github.com/gorilla/websocket"
)

const (
	defaultHTTPMaxBodyBytes int64 = 10 << 20
	maxHTTPHostedFiles            = 256
)

var unsafeResponseHeaders = map[string]bool{
	"connection": true, "content-length": true, "keep-alive": true,
	"proxy-authenticate": true, "proxy-authorization": true, "te": true,
	"trailer": true, "transfer-encoding": true, "upgrade": true,
}

type HTTPDriver struct {
	registry   *Registry
	hostedRoot string
}

type HTTPDriverConfig struct {
	HostedRoot string
}

type httpAddressOptions struct {
	Host string `json:"host"`
	Port string `json:"port"`
}

type httpTLSOptions struct {
	Enabled  bool   `json:"enabled,omitempty"`
	CertFile string `json:"cert_file,omitempty"`
	KeyFile  string `json:"key_file,omitempty"`
}

type httpTimeoutOptions struct {
	ReadHeader string `json:"read_header,omitempty"`
	Read       string `json:"read,omitempty"`
	Write      string `json:"write,omitempty"`
	Idle       string `json:"idle,omitempty"`
}

type httpListenerOptions struct {
	Bind            httpAddressOptions              `json:"bind"`
	Advertise       httpAddressOptions              `json:"advertise,omitempty"`
	TLS             httpTLSOptions                  `json:"tls,omitempty"`
	ResponseHeaders map[string]string               `json:"response_headers,omitempty"`
	HostedFiles     map[string]HTTPHostedFileConfig `json:"hosted_files,omitempty"`
	NotFoundPage    *HTTPHostedFileConfig           `json:"not_found_page,omitempty"`
	Timeouts        httpTimeoutOptions              `json:"timeouts,omitempty"`
	MaxBodyBytes    int64                           `json:"max_body_bytes,omitempty"`
	MaxHeaderBytes  int                             `json:"max_header_bytes,omitempty"`
}

type HTTPHostedFileConfig struct {
	SourcePath string            `json:"source_path"`
	Status     int               `json:"status,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
}

type HTTPHostedFilesConfig struct {
	HostedFiles  map[string]HTTPHostedFileConfig
	NotFoundPage *HTTPHostedFileConfig
}

type resolvedHTTPOptions struct {
	httpListenerOptions
	readHeaderTimeout time.Duration
	readTimeout       time.Duration
	writeTimeout      time.Duration
	idleTimeout       time.Duration
}

type httpRouteMatch struct {
	Path       string   `json:"path,omitempty"`
	PathPrefix string   `json:"path_prefix,omitempty"`
	Methods    []string `json:"methods,omitempty"`
	Host       string   `json:"host,omitempty"`
	Extensions []string `json:"extensions,omitempty"`
}

type httpSessionSource struct {
	Source string `json:"source,omitempty"`
	Name   string `json:"name,omitempty"`
}

type httpResponseOptions struct {
	Status     int               `json:"status,omitempty"`
	NilStatus  int               `json:"nil_status,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	NoTaskBody string            `json:"no_task_body,omitempty"`
}

type httpRouteOptions struct {
	Session     httpSessionSource   `json:"session,omitempty"`
	Response    httpResponseOptions `json:"response,omitempty"`
	StreamQuery string              `json:"stream_query,omitempty"`
}

type compiledHTTPRoute struct {
	route    Route
	match    httpRouteMatch
	options  httpRouteOptions
	inbound  Carrier
	outbound Carrier
}

type compiledHTTPHostedFile struct {
	file        *os.File
	size        int64
	status      int
	headers     map[string]string
	contentType string
}

type compiledHTTPHostedFiles struct {
	files       map[string]*compiledHTTPHostedFile
	notFound    *compiledHTTPHostedFile
	all         []*compiledHTTPHostedFile
	close       sync.Once
	lifecycleMu sync.Mutex
	references  int
	retired     bool
}

func (files *compiledHTTPHostedFiles) Close() {
	if files == nil {
		return
	}
	files.lifecycleMu.Lock()
	files.retired = true
	closeNow := files.references == 0
	files.lifecycleMu.Unlock()
	if closeNow {
		files.closeFiles()
	}
}

func (files *compiledHTTPHostedFiles) acquire() bool {
	if files == nil {
		return false
	}
	files.lifecycleMu.Lock()
	defer files.lifecycleMu.Unlock()
	if files.retired {
		return false
	}
	files.references++
	return true
}

func (files *compiledHTTPHostedFiles) release() {
	files.lifecycleMu.Lock()
	files.references--
	closeNow := files.retired && files.references == 0
	files.lifecycleMu.Unlock()
	if closeNow {
		files.closeFiles()
	}
}

func (files *compiledHTTPHostedFiles) closeFiles() {
	files.close.Do(func() {
		for _, file := range files.all {
			_ = file.file.Close()
		}
	})
}

type httpRuntime struct {
	server           *http.Server
	listener         net.Listener
	address          string
	done             chan error
	served           chan struct{}
	closeInteractive func()
	closeHosted      func()
	replaceHosted    func(*compiledHTTPHostedFiles) bool
	stopMu           sync.Mutex
	stopped          bool
}

type trackedConnection struct {
	net.Conn
	once    sync.Once
	onClose func()
}

func (connection *trackedConnection) Close() error {
	err := connection.Conn.Close()
	connection.once.Do(connection.onClose)
	return err
}

type connectionTracker struct {
	mu          sync.Mutex
	connections map[*trackedConnection]struct{}
	closed      bool
}

func newConnectionTracker() *connectionTracker {
	return &connectionTracker{connections: make(map[*trackedConnection]struct{})}
}

func (tracker *connectionTracker) Add(connection net.Conn) (*trackedConnection, bool) {
	tracked := &trackedConnection{Conn: connection}
	tracked.onClose = func() {
		tracker.mu.Lock()
		delete(tracker.connections, tracked)
		tracker.mu.Unlock()
	}
	tracker.mu.Lock()
	if tracker.closed {
		tracker.mu.Unlock()
		_ = tracked.Close()
		return nil, false
	}
	tracker.connections[tracked] = struct{}{}
	tracker.mu.Unlock()
	return tracked, true
}

func (tracker *connectionTracker) CloseAll() {
	tracker.mu.Lock()
	tracker.closed = true
	connections := make([]*trackedConnection, 0, len(tracker.connections))
	for connection := range tracker.connections {
		connections = append(connections, connection)
	}
	tracker.mu.Unlock()
	for _, connection := range connections {
		_ = connection.Close()
	}
}

func NewHTTPDriver(registry *Registry) *HTTPDriver {
	return NewHTTPDriverWithConfig(registry, HTTPDriverConfig{})
}

func NewHTTPDriverWithConfig(registry *Registry, configuration HTTPDriverConfig) *HTTPDriver {
	root := strings.TrimSpace(configuration.HostedRoot)
	if root == "" {
		root = "hosted"
	}
	return &HTTPDriver{registry: registry, hostedRoot: root}
}

func (driver *HTTPDriver) Definition() DriverDefinition {
	return DriverDefinition{
		ID: "http", Description: "HTTP callback listener with configurable routing and carriers.",
		Capabilities: []string{"routes", "custom_headers", "tls", "interactive", "carriers", "file_hosting", "custom_404"},
		Options: []OptionDefinition{
			{Key: "bind.host", Type: OptionString, Default: json.RawMessage(`"0.0.0.0"`), Description: "Local interface or address to bind."},
			{Key: "bind.port", Type: OptionString, Default: json.RawMessage(`"4444"`), Description: "Local TCP port; zero requests an ephemeral port."},
			{Key: "advertise.host", Type: OptionString, Description: "Host embedded in generated payload configuration."},
			{Key: "advertise.port", Type: OptionString, Description: "Port embedded in generated payload configuration."},
			{Key: "tls.enabled", Type: OptionBoolean, Default: json.RawMessage(`false`)},
			{Key: "tls.cert_file", Type: OptionFile},
			{Key: "tls.key_file", Type: OptionFile},
			{Key: "response_headers", Type: OptionStringMap, Description: "Headers applied to all listener responses."},
			{Key: "hosted_files", Type: OptionObject, MutableWhileRunning: true, Description: "Exact URL paths mapped to files beneath the teamserver hosted-file directory."},
			{Key: "not_found_page", Type: OptionObject, MutableWhileRunning: true, Description: "Optional file response used when no listener route or hosted URL matches."},
			{Key: "max_body_bytes", Type: OptionInteger, Default: json.RawMessage(`10485760`)},
			{Key: "max_header_bytes", Type: OptionInteger, Default: json.RawMessage(`32768`)},
			{Key: "timeouts.read_header", Type: OptionDuration, Default: json.RawMessage(`"5s"`)},
			{Key: "timeouts.read", Type: OptionDuration, Default: json.RawMessage(`"30s"`)},
			{Key: "timeouts.write", Type: OptionDuration, Default: json.RawMessage(`"30s"`)},
			{Key: "timeouts.idle", Type: OptionDuration, Default: json.RawMessage(`"60s"`)},
		},
	}
}

func (driver *HTTPDriver) Validate(options json.RawMessage, routes []Route) error {
	if driver.registry == nil {
		return errors.New("HTTP driver registry is required")
	}
	if _, err := resolveHTTPOptions(options); err != nil {
		return err
	}
	_, err := driver.compileRoutes(routes)
	return err
}

func (driver *HTTPDriver) Start(ctx context.Context, configuration RuntimeConfig, handler ExchangeHandler) (Runtime, error) {
	if handler == nil {
		return nil, errors.New("HTTP exchange handler is required")
	}
	options, err := resolveHTTPOptions(configuration.Options)
	if err != nil {
		return nil, err
	}
	routes, err := driver.compileRoutes(configuration.Routes)
	if err != nil {
		return nil, err
	}
	hostedFiles, err := driver.openHostedFiles(options)
	if err != nil {
		return nil, err
	}
	interactiveConnections := newConnectionTracker()
	httpHandler := &httpListenerHandler{
		configuration: configuration, options: options, routes: routes, exchange: handler,
		upgrader: websocket.Upgrader{}, interactiveConnections: interactiveConnections,
		defaultRoutes: len(configuration.Routes) == 0, hostedFiles: hostedFiles,
	}
	server := &http.Server{
		Handler: httpHandler, ReadHeaderTimeout: options.readHeaderTimeout,
		ReadTimeout: options.readTimeout, WriteTimeout: options.writeTimeout,
		IdleTimeout: options.idleTimeout, MaxHeaderBytes: options.MaxHeaderBytes,
	}
	address := net.JoinHostPort(options.Bind.Host, options.Bind.Port)
	networkListener, err := net.Listen("tcp", address)
	if err != nil {
		hostedFiles.Close()
		return nil, err
	}
	serveListener := networkListener
	if options.TLS.Enabled {
		certificate, certificateErr := tls.LoadX509KeyPair(options.TLS.CertFile, options.TLS.KeyFile)
		if certificateErr != nil {
			_ = networkListener.Close()
			hostedFiles.Close()
			return nil, fmt.Errorf("load listener TLS certificate: %w", certificateErr)
		}
		serveListener = tls.NewListener(networkListener, &tls.Config{
			Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12,
		})
	}
	runtime := &httpRuntime{
		server: server, listener: serveListener, address: serveListener.Addr().String(),
		done: make(chan error, 1), served: make(chan struct{}),
		closeInteractive: interactiveConnections.CloseAll,
		closeHosted:      httpHandler.closeHostedFiles,
		replaceHosted:    httpHandler.replaceHostedFiles,
	}
	go runtime.serve()
	go func() {
		select {
		case <-ctx.Done():
			shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = runtime.Stop(shutdownContext)
		case <-runtime.served:
		}
	}()
	return runtime, nil
}

func (runtime *httpRuntime) Address() string    { return runtime.address }
func (runtime *httpRuntime) Done() <-chan error { return runtime.done }
func (runtime *httpRuntime) Stop(ctx context.Context) error {
	runtime.stopMu.Lock()
	defer runtime.stopMu.Unlock()
	if runtime.stopped {
		return nil
	}
	if runtime.closeInteractive != nil {
		runtime.closeInteractive()
	}
	if err := runtime.server.Shutdown(ctx); err != nil {
		// Shutdown is graceful and may time out on an active callback. Close
		// provides the bounded fallback required by listener delete/shutdown.
		if closeErr := runtime.server.Close(); closeErr != nil {
			if runtime.closeHosted != nil {
				runtime.closeHosted()
			}
			return errors.Join(err, closeErr)
		}
	}
	if runtime.closeHosted != nil {
		runtime.closeHosted()
	}
	runtime.stopped = true
	return nil
}

func (runtime *httpRuntime) serve() {
	defer close(runtime.served)
	err := runtime.server.Serve(runtime.listener)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	} else {
		_ = runtime.server.Close()
		if runtime.closeHosted != nil {
			runtime.closeHosted()
		}
		if errors.Is(err, net.ErrClosed) {
			err = nil
		}
	}
	runtime.done <- err
	close(runtime.done)
}

type httpListenerHandler struct {
	configuration          RuntimeConfig
	options                resolvedHTTPOptions
	routes                 []compiledHTTPRoute
	hostedFiles            *compiledHTTPHostedFiles
	exchange               ExchangeHandler
	upgrader               websocket.Upgrader
	interactiveConnections *connectionTracker
	defaultRoutes          bool
	hostedMu               sync.RWMutex
	hostedClosed           bool
}

type httpCallbackDisposition uint8

const (
	httpCallbackHandled httpCallbackDisposition = iota
	httpCallbackInvalid
)

func (handler *httpListenerHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	for name, value := range handler.options.ResponseHeaders {
		writer.Header().Set(name, value)
	}
	route := handler.matchRoute(request)
	if route == nil {
		if (request.Method == http.MethodGet || request.Method == http.MethodHead) &&
			handler.serveExactHostedFile(writer, request) {
			return
		}
		if handler.serveNotFoundPage(writer, request) {
			return
		}
		if handler.defaultRoutes && request.URL.Path == "/" &&
			request.Method != http.MethodGet && request.Method != http.MethodPost {
			writer.Header().Set("Allow", "GET, POST")
			http.Error(writer, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		http.NotFound(writer, request)
		return
	}
	switch route.route.Purpose {
	case "callback":
		if handler.callback(writer, request, route) == httpCallbackInvalid {
			if handler.serveExactHostedFile(writer, request) {
				return
			}
			http.Error(writer, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		}
	case "interactive":
		if !websocket.IsWebSocketUpgrade(request) && handler.serveExactHostedFile(writer, request) {
			return
		}
		handler.interactive(writer, request, route)
	default:
		http.Error(writer, "route purpose is not available", http.StatusNotImplemented)
	}
}

func (handler *httpListenerHandler) serveExactHostedFile(writer http.ResponseWriter, request *http.Request) bool {
	files := handler.acquireHostedFiles()
	if files == nil {
		return false
	}
	defer files.release()
	hosted := files.files[request.URL.Path]
	if hosted == nil {
		return false
	}
	handler.serveHostedFile(writer, request, hosted)
	return true
}

func (handler *httpListenerHandler) serveNotFoundPage(writer http.ResponseWriter, request *http.Request) bool {
	files := handler.acquireHostedFiles()
	if files == nil {
		return false
	}
	defer files.release()
	if files.notFound == nil {
		return false
	}
	handler.serveHostedFile(writer, request, files.notFound)
	return true
}

func (handler *httpListenerHandler) acquireHostedFiles() *compiledHTTPHostedFiles {
	handler.hostedMu.RLock()
	defer handler.hostedMu.RUnlock()
	if handler.hostedClosed || !handler.hostedFiles.acquire() {
		return nil
	}
	return handler.hostedFiles
}

func (handler *httpListenerHandler) replaceHostedFiles(files *compiledHTTPHostedFiles) bool {
	handler.hostedMu.Lock()
	if handler.hostedClosed {
		handler.hostedMu.Unlock()
		files.Close()
		return false
	}
	previous := handler.hostedFiles
	handler.hostedFiles = files
	handler.hostedMu.Unlock()
	previous.Close()
	return true
}

func (handler *httpListenerHandler) closeHostedFiles() {
	handler.hostedMu.Lock()
	handler.hostedClosed = true
	previous := handler.hostedFiles
	handler.hostedFiles = nil
	handler.hostedMu.Unlock()
	previous.Close()
}

func (handler *httpListenerHandler) serveHostedFile(writer http.ResponseWriter, request *http.Request, hosted *compiledHTTPHostedFile) {
	for name, value := range hosted.headers {
		writer.Header().Set(name, value)
	}
	if writer.Header().Get("Content-Type") == "" {
		writer.Header().Set("Content-Type", hosted.contentType)
	}
	writer.Header().Set("Content-Length", strconv.FormatInt(hosted.size, 10))
	writer.WriteHeader(hosted.status)
	if request.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(writer, io.NewSectionReader(hosted.file, 0, hosted.size))
}

func (handler *httpListenerHandler) callback(writer http.ResponseWriter, request *http.Request, route *compiledHTTPRoute) httpCallbackDisposition {
	request.Body = http.MaxBytesReader(writer, request.Body, handler.options.MaxBodyBytes)
	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(writer, "invalid callback body", http.StatusRequestEntityTooLarge)
		return httpCallbackHandled
	}
	envelope := requestEnvelope(request, body)
	message, err := route.inbound.Decode(envelope, route.route.Inbound.Options)
	if err != nil {
		return httpCallbackInvalid
	}
	session := message.Session
	if session == "" {
		session = fieldValue(envelope, route.options.Session)
	}
	result, err := handler.exchange(request.Context(), Exchange{
		ListenerName: handler.configuration.Name, ListenerUUID: handler.configuration.UUID,
		Transport: handler.transport(), RemoteAddress: request.RemoteAddr,
		AuthenticatedSession: session, Payload: message.Payload,
		Metadata: cloneStringMap(envelope.Fields),
	})
	if err != nil {
		if errors.Is(err, ErrInvalidImplantRequest) {
			return httpCallbackInvalid
		}
		http.Error(writer, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return httpCallbackHandled
	}
	for name, value := range route.options.Response.Headers {
		writer.Header().Set(name, value)
	}
	status := route.options.Response.Status
	if status == 0 {
		status = http.StatusOK
	}
	if result.MessageType == internal.NIL {
		if route.options.Response.NilStatus != 0 {
			status = route.options.Response.NilStatus
		} else {
			status = http.StatusNotFound
		}
	}
	responseBody := []byte("Hi!")
	if route.options.Response.NoTaskBody != "" {
		responseBody = []byte(route.options.Response.NoTaskBody)
	}
	if len(result.Payload) > 0 {
		response, encodeErr := route.outbound.Encode(CarriedMessage{Payload: result.Payload, Session: session}, route.route.Outbound.Options)
		if encodeErr != nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return httpCallbackHandled
		}
		applyResponseEnvelope(writer, response)
		responseBody = response.Body
	}
	writer.WriteHeader(status)
	_, _ = writer.Write(responseBody)
	return httpCallbackHandled
}

func (handler *httpListenerHandler) transport() string {
	if handler.options.TLS.Enabled {
		return "https"
	}
	return "http"
}

func (handler *httpListenerHandler) interactive(writer http.ResponseWriter, request *http.Request, route *compiledHTTPRoute) {
	connection, err := handler.upgrader.Upgrade(writer, request, nil)
	if err != nil {
		return
	}
	streamQuery := route.options.StreamQuery
	if streamQuery == "" {
		streamQuery = "stream"
	}
	stream := net.Conn(utils.New(connection))
	if handler.interactiveConnections != nil {
		tracked, accepted := handler.interactiveConnections.Add(stream)
		if !accepted {
			return
		}
		stream = tracked
	}
	if err := interactive.Default.AttachImplant(request.URL.Query().Get(streamQuery), stream); err != nil {
		_ = stream.Close()
	}
}

func (handler *httpListenerHandler) matchRoute(request *http.Request) *compiledHTTPRoute {
	for index := range handler.routes {
		route := &handler.routes[index]
		if routeMatches(route.match, request) {
			return route
		}
	}
	return nil
}

func (driver *HTTPDriver) compileRoutes(routes []Route) ([]compiledHTTPRoute, error) {
	if len(routes) == 0 {
		routes = defaultHTTPRoutes()
	}
	seen := make(map[string]bool, len(routes))
	compiled := make([]compiledHTTPRoute, 0, len(routes))
	for _, route := range routes {
		route.ID = strings.TrimSpace(route.ID)
		route.Purpose = strings.ToLower(strings.TrimSpace(route.Purpose))
		if route.ID == "" || seen[route.ID] {
			return nil, fmt.Errorf("HTTP route IDs must be non-empty and unique: %q", route.ID)
		}
		seen[route.ID] = true
		if route.Purpose != "callback" && route.Purpose != "interactive" {
			return nil, fmt.Errorf("HTTP route %q has unsupported purpose %q", route.ID, route.Purpose)
		}
		var match httpRouteMatch
		if err := decodeStrictJSONObject(route.Match, &match); err != nil {
			return nil, fmt.Errorf("decode HTTP route %q match: %w", route.ID, err)
		}
		if err := validateHTTPRouteMatch(&match); err != nil {
			return nil, fmt.Errorf("HTTP route %q: %w", route.ID, err)
		}
		var options httpRouteOptions
		if err := decodeStrictJSONObject(route.Options, &options); err != nil {
			return nil, fmt.Errorf("decode HTTP route %q options: %w", route.ID, err)
		}
		if err := validateHTTPRouteOptions(&options); err != nil {
			return nil, fmt.Errorf("HTTP route %q: %w", route.ID, err)
		}
		var inbound, outbound Carrier
		if route.Purpose == "callback" {
			var found bool
			inbound, found = driver.registry.Carrier(route.Inbound.Type)
			if !found {
				return nil, fmt.Errorf("HTTP route %q inbound carrier %q is not registered", route.ID, route.Inbound.Type)
			}
			outbound, found = driver.registry.Carrier(route.Outbound.Type)
			if !found {
				return nil, fmt.Errorf("HTTP route %q outbound carrier %q is not registered", route.ID, route.Outbound.Type)
			}
			if normalizeRegistryID(route.Outbound.Type) == "query" {
				return nil, fmt.Errorf("HTTP route %q cannot use query as a response carrier", route.ID)
			}
			if err := inbound.Validate(route.Inbound.Options); err != nil {
				return nil, fmt.Errorf("HTTP route %q inbound carrier: %w", route.ID, err)
			}
			if err := outbound.Validate(route.Outbound.Options); err != nil {
				return nil, fmt.Errorf("HTTP route %q outbound carrier: %w", route.ID, err)
			}
		}
		compiled = append(compiled, compiledHTTPRoute{route: route, match: match, options: options, inbound: inbound, outbound: outbound})
	}
	sort.SliceStable(compiled, func(left, right int) bool {
		return compiled[left].route.Priority > compiled[right].route.Priority
	})
	return compiled, nil
}

func resolveHTTPOptions(raw json.RawMessage) (resolvedHTTPOptions, error) {
	var options resolvedHTTPOptions
	if err := decodeStrictJSONObject(raw, &options.httpListenerOptions); err != nil {
		return options, fmt.Errorf("decode HTTP listener options: %w", err)
	}
	if options.Bind.Host == "" {
		options.Bind.Host = "0.0.0.0"
	}
	if options.Bind.Port == "" {
		options.Bind.Port = "4444"
	}
	if options.Advertise.Host == "" {
		options.Advertise.Host = options.Bind.Host
	}
	if options.Advertise.Port == "" {
		options.Advertise.Port = options.Bind.Port
	}
	if err := validatePort(options.Bind.Port); err != nil {
		return options, fmt.Errorf("HTTP bind port: %w", err)
	}
	if err := validatePort(options.Advertise.Port); err != nil {
		return options, fmt.Errorf("HTTP advertise port: %w", err)
	}
	if options.TLS.Enabled && (strings.TrimSpace(options.TLS.CertFile) == "" || strings.TrimSpace(options.TLS.KeyFile) == "") {
		return options, errors.New("HTTP TLS requires cert_file and key_file")
	}
	if !options.TLS.Enabled && (options.TLS.CertFile != "" || options.TLS.KeyFile != "") {
		return options, errors.New("HTTP TLS certificate options require tls.enabled")
	}
	if err := validateResponseHeaders(options.ResponseHeaders); err != nil {
		return options, err
	}
	if err := validateHTTPHostedFiles(options.HostedFiles, options.NotFoundPage); err != nil {
		return options, err
	}
	if options.MaxBodyBytes == 0 {
		options.MaxBodyBytes = defaultHTTPMaxBodyBytes
	}
	if options.MaxBodyBytes < 1 || options.MaxBodyBytes > 64<<20 {
		return options, errors.New("HTTP max_body_bytes must be between 1 and 67108864")
	}
	if options.MaxHeaderBytes == 0 {
		options.MaxHeaderBytes = 32 << 10
	}
	if options.MaxHeaderBytes < 1024 || options.MaxHeaderBytes > 1<<20 {
		return options, errors.New("HTTP max_header_bytes must be between 1024 and 1048576")
	}
	var err error
	if options.readHeaderTimeout, err = parseHTTPDuration(options.Timeouts.ReadHeader, 5*time.Second, "read_header"); err != nil {
		return options, err
	}
	if options.readTimeout, err = parseHTTPDuration(options.Timeouts.Read, 30*time.Second, "read"); err != nil {
		return options, err
	}
	if options.writeTimeout, err = parseHTTPDuration(options.Timeouts.Write, 30*time.Second, "write"); err != nil {
		return options, err
	}
	if options.idleTimeout, err = parseHTTPDuration(options.Timeouts.Idle, 60*time.Second, "idle"); err != nil {
		return options, err
	}
	return options, nil
}

func validateHTTPHostedFiles(files map[string]HTTPHostedFileConfig, notFound *HTTPHostedFileConfig) error {
	if len(files) > maxHTTPHostedFiles {
		return fmt.Errorf("HTTP hosted_files contains %d entries; maximum is %d", len(files), maxHTTPHostedFiles)
	}
	for urlPath, file := range files {
		if err := validateHTTPHostedURLPath(urlPath); err != nil {
			return fmt.Errorf("HTTP hosted file %q: %w", urlPath, err)
		}
		if err := validateHTTPHostedFileOptions(&file, http.StatusOK); err != nil {
			return fmt.Errorf("HTTP hosted file %q: %w", urlPath, err)
		}
		files[urlPath] = file
	}
	if notFound != nil {
		if err := validateHTTPHostedFileOptions(notFound, http.StatusNotFound); err != nil {
			return fmt.Errorf("HTTP not-found page: %w", err)
		}
		if notFound.Status != http.StatusNotFound {
			return errors.New("HTTP not-found page status must be 404")
		}
	}
	return nil
}

func validateHTTPHostedURLPath(value string) error {
	if value == "" || !strings.HasPrefix(value, "/") {
		return errors.New("URL path must start with /")
	}
	if strings.ContainsAny(value, "?#\r\n\x00") {
		return errors.New("URL path must not contain a query, fragment, line break, or NUL byte")
	}
	if path.Clean(value) != value || strings.HasPrefix(value, "//") {
		return errors.New("URL path must be canonical")
	}
	return nil
}

func validateHTTPHostedFileOptions(options *HTTPHostedFileConfig, defaultStatus int) error {
	options.SourcePath = strings.TrimSpace(options.SourcePath)
	if options.SourcePath == "" || strings.ContainsRune(options.SourcePath, '\x00') {
		return errors.New("source_path is required and must not contain a NUL byte")
	}
	if options.Status == 0 {
		options.Status = defaultStatus
	}
	if options.Status < 200 || options.Status > 599 || options.Status == http.StatusNoContent ||
		options.Status == http.StatusResetContent || options.Status == http.StatusNotModified {
		return errors.New("status must be a final HTTP status between 200 and 599 that permits a response body")
	}
	return validateResponseHeaders(options.Headers)
}

func (driver *HTTPDriver) openHostedFiles(options resolvedHTTPOptions) (*compiledHTTPHostedFiles, error) {
	result := &compiledHTTPHostedFiles{files: make(map[string]*compiledHTTPHostedFile, len(options.HostedFiles))}
	if len(options.HostedFiles) == 0 && options.NotFoundPage == nil {
		return result, nil
	}
	root, err := filepath.Abs(driver.hostedRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve HTTP hosted-file root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve HTTP hosted-file root: %w", err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("inspect HTTP hosted-file root: %w", err)
	}
	if !rootInfo.IsDir() {
		return nil, errors.New("HTTP hosted-file root is not a directory")
	}
	for urlPath, configuration := range options.HostedFiles {
		file, openErr := openHTTPHostedFile(root, urlPath, configuration)
		if openErr != nil {
			result.Close()
			return nil, fmt.Errorf("open HTTP hosted file %q: %w", urlPath, openErr)
		}
		result.files[urlPath] = file
		result.all = append(result.all, file)
	}
	if options.NotFoundPage != nil {
		file, openErr := openHTTPHostedFile(root, "", *options.NotFoundPage)
		if openErr != nil {
			result.Close()
			return nil, fmt.Errorf("open HTTP not-found page: %w", openErr)
		}
		result.notFound = file
		result.all = append(result.all, file)
	}
	return result, nil
}

func openHTTPHostedFile(root, urlPath string, configuration HTTPHostedFileConfig) (*compiledHTTPHostedFile, error) {
	source := configuration.SourcePath
	if !filepath.IsAbs(source) {
		source = filepath.Join(root, source)
	}
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return nil, err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil {
		return nil, err
	}
	if relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, errors.New("source_path resolves outside the hosted-file root")
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, errors.New("source_path is not a regular file")
	}
	contentType := configuration.Headers["Content-Type"]
	if contentType == "" {
		extension := filepath.Ext(urlPath)
		if extension == "" {
			extension = filepath.Ext(resolved)
		}
		contentType = mime.TypeByExtension(extension)
		if contentType == "" {
			prefix := make([]byte, 512)
			read, readErr := file.ReadAt(prefix, 0)
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				_ = file.Close()
				return nil, readErr
			}
			contentType = http.DetectContentType(prefix[:read])
		}
	}
	return &compiledHTTPHostedFile{
		file: file, size: info.Size(), status: configuration.Status,
		headers: cloneHeaderMap(configuration.Headers), contentType: contentType,
	}, nil
}

func cloneHeaderMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for name, value := range source {
		result[name] = value
	}
	return result
}

func parseHTTPDuration(value string, fallback time.Duration, field string) (time.Duration, error) {
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("HTTP timeout %s must be a positive duration", field)
	}
	return duration, nil
}

func validatePort(value string) error {
	port, err := strconv.Atoi(value)
	if err != nil || port < 0 || port > 65535 {
		return errors.New("must be a number between 0 and 65535")
	}
	return nil
}

func validateResponseHeaders(headers map[string]string) error {
	normalized := make(map[string]string, len(headers))
	for name, value := range headers {
		name = strings.TrimSpace(name)
		if !validHTTPHeaderName(name) || !validHTTPHeaderValue(value) {
			return fmt.Errorf("invalid HTTP response header %q", name)
		}
		canonical := textproto.CanonicalMIMEHeaderKey(name)
		if unsafeResponseHeaders[strings.ToLower(canonical)] {
			return fmt.Errorf("HTTP response header %q is controlled by the server", name)
		}
		if _, exists := normalized[canonical]; exists {
			return fmt.Errorf("duplicate HTTP response header %q", canonical)
		}
		normalized[canonical] = value
	}
	for name := range headers {
		delete(headers, name)
	}
	for name, value := range normalized {
		headers[name] = value
	}
	return nil
}

func validHTTPHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, character := range []byte(name) {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(character)) {
			continue
		}
		return false
	}
	return true
}

func validHTTPHeaderValue(value string) bool {
	for _, character := range []byte(value) {
		if character == '\t' || character >= ' ' && character != '\x7f' {
			continue
		}
		return false
	}
	return true
}

func validateHTTPRouteMatch(match *httpRouteMatch) error {
	if match.Path != "" && match.PathPrefix != "" {
		return errors.New("path and path_prefix are mutually exclusive")
	}
	if match.Path != "" && !strings.HasPrefix(match.Path, "/") || match.PathPrefix != "" && !strings.HasPrefix(match.PathPrefix, "/") {
		return errors.New("HTTP route paths must start with /")
	}
	for index, method := range match.Methods {
		method = strings.ToUpper(strings.TrimSpace(method))
		if method == "" || strings.ContainsAny(method, " \t\r\n") {
			return errors.New("HTTP route methods must be valid tokens")
		}
		match.Methods[index] = method
	}
	for index, extension := range match.Extensions {
		extension = strings.ToLower(strings.TrimSpace(extension))
		if extension == "" {
			return errors.New("HTTP route extension must not be empty")
		}
		if !strings.HasPrefix(extension, ".") {
			extension = "." + extension
		}
		match.Extensions[index] = extension
	}
	return nil
}

func validateHTTPRouteOptions(options *httpRouteOptions) error {
	options.Session.Source = strings.ToLower(strings.TrimSpace(options.Session.Source))
	if options.Session.Source != "" {
		if options.Session.Source != "query" && options.Session.Source != "header" && options.Session.Source != "cookie" {
			return errors.New("session source must be query, header, or cookie")
		}
		if strings.TrimSpace(options.Session.Name) == "" || strings.ContainsAny(options.Session.Name, "\r\n") {
			return errors.New("session source name is required")
		}
	}
	for _, status := range []int{options.Response.Status, options.Response.NilStatus} {
		if status != 0 && (status < 100 || status > 599) {
			return errors.New("HTTP response status must be between 100 and 599")
		}
	}
	return validateResponseHeaders(options.Response.Headers)
}

func routeMatches(match httpRouteMatch, request *http.Request) bool {
	if match.Path != "" && request.URL.Path != match.Path {
		return false
	}
	if match.PathPrefix != "" && !strings.HasPrefix(request.URL.Path, match.PathPrefix) {
		return false
	}
	if match.Host != "" && !strings.EqualFold(request.Host, match.Host) {
		return false
	}
	if len(match.Methods) > 0 && !containsString(match.Methods, request.Method) {
		return false
	}
	if len(match.Extensions) > 0 {
		lowerPath := strings.ToLower(request.URL.Path)
		matched := false
		for _, extension := range match.Extensions {
			if strings.HasSuffix(lowerPath, extension) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func requestEnvelope(request *http.Request, body []byte) CarrierEnvelope {
	fields := make(map[string][]string)
	for name, values := range request.Header {
		fields["header:"+strings.ToLower(name)] = append([]string(nil), values...)
	}
	for name, values := range request.URL.Query() {
		fields["query:"+name] = append([]string(nil), values...)
	}
	for _, cookie := range request.Cookies() {
		fields["cookie:"+cookie.Name] = append(fields["cookie:"+cookie.Name], cookie.Value)
	}
	return CarrierEnvelope{Body: body, Fields: fields}
}

func fieldValue(envelope CarrierEnvelope, source httpSessionSource) string {
	if source.Source == "" {
		return ""
	}
	name := source.Name
	if source.Source == "header" {
		name = strings.ToLower(name)
	}
	values := envelope.Fields[source.Source+":"+name]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func applyResponseEnvelope(writer http.ResponseWriter, envelope CarrierEnvelope) {
	for key, values := range envelope.Fields {
		switch {
		case strings.HasPrefix(key, "header:"):
			name := strings.TrimPrefix(key, "header:")
			if !validHTTPToken(name) || unsafeResponseHeaders[strings.ToLower(name)] {
				continue
			}
			for index, value := range values {
				if strings.ContainsAny(value, "\r\n") {
					continue
				}
				if index == 0 {
					writer.Header().Set(name, value)
				} else {
					writer.Header().Add(name, value)
				}
			}
		case strings.HasPrefix(key, "cookie:"):
			name := strings.TrimPrefix(key, "cookie:")
			if !validHTTPToken(name) {
				continue
			}
			for _, value := range values {
				http.SetCookie(writer, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true})
			}
		}
	}
}

func cloneStringMap(source map[string][]string) map[string][]string {
	result := make(map[string][]string, len(source))
	for key, values := range source {
		result[key] = append([]string(nil), values...)
	}
	return result
}

func defaultHTTPRoutes() []Route {
	return []Route{
		{
			ID: "interactive-images", Purpose: "interactive", Priority: 100,
			Match:   json.RawMessage(`{"methods":["GET"],"extensions":[".png",".jpg",".gif"]}`),
			Options: json.RawMessage(`{"stream_query":"stream"}`),
		},
		{
			ID: "callback-get", Purpose: "callback", Match: json.RawMessage(`{"path":"/","methods":["GET"]}`),
			Options:  json.RawMessage(`{"session":{"source":"query","name":"a"},"response":{"nil_status":404,"no_task_body":"Hi!"}}`),
			Inbound:  CarrierSpec{Type: "cookie", Options: json.RawMessage(`{"name":"a"}`)},
			Outbound: CarrierSpec{Type: "body", Options: json.RawMessage(`{}`)},
		},
		{
			ID: "callback-post", Purpose: "callback", Match: json.RawMessage(`{"path":"/","methods":["POST"]}`),
			Options:  json.RawMessage(`{"session":{"source":"query","name":"a"},"response":{"nil_status":404,"no_task_body":"Hi!"}}`),
			Inbound:  CarrierSpec{Type: "body", Options: json.RawMessage(`{}`)},
			Outbound: CarrierSpec{Type: "body", Options: json.RawMessage(`{}`)},
		},
	}
}

func RegisterHTTPBuiltins(registry *Registry) error {
	return RegisterHTTPBuiltinsWithConfig(registry, HTTPDriverConfig{})
}

func RegisterHTTPBuiltinsWithConfig(registry *Registry, configuration HTTPDriverConfig) error {
	if registry == nil {
		return errors.New("listener registry is required")
	}
	if err := registerHTTPCarriers(registry); err != nil {
		return err
	}
	return registry.RegisterDriver(NewHTTPDriverWithConfig(registry, configuration))
}
