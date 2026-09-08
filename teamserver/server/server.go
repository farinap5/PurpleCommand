package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"
	"purpcmd/server/interactive"
	"purpcmd/server/listener"
	"purpcmd/server/loot"
	"purpcmd/server/speaker"
	"purpcmd/server/uploads"
	"purpcmd/server/utils"
	"purpcmd/teamserver/builds"
	"purpcmd/teamserver/config"
	"purpcmd/teamserver/events"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Server struct {
	config             config.Config
	id                 string
	started            time.Time
	events             *events.Bus
	builds             *builds.Manager
	listeners          *listener.Manager
	speakers           *speaker.Manager
	profileListenerMu  sync.Mutex
	http               *http.Server
	dedupeMu           sync.Mutex
	connectionsMu      sync.RWMutex
	connections        map[string]map[*websocket.Conn]struct{}
	lastSeen           map[string]time.Time
	controlMu          sync.Mutex
	controlConnections map[*websocket.Conn]struct{}
	controlWG          sync.WaitGroup
	shuttingDown       bool
	upgrader           websocket.Upgrader
}

type principal struct {
	Name  string
	UUID  string
	Admin bool
}

const adminPrincipalID = "admin"

func New(configuration config.Config, eventBus *events.Bus) *Server {
	listenerManager, err := listener.NewHTTPManagerWithConfig(listener.DBStore{}, listener.TeamEventPublisher(func(eventType string, value any) {
		_, _ = eventBus.Publish(eventType, value)
	}), listener.HTTPDriverConfig{HostedRoot: configuration.HostedDir})
	if err != nil {
		panic(fmt.Sprintf("initialize HTTP listener manager: %v", err))
	}
	speakerManager := speaker.NewManager(speaker.DBStore{}, func(eventType string, value any) {
		_, _ = eventBus.Publish(eventType, value)
	})
	return NewWithManagers(configuration, eventBus, listenerManager, speakerManager)
}

func NewWithListenerManager(configuration config.Config, eventBus *events.Bus, listenerManager *listener.Manager) *Server {
	speakerManager := speaker.NewManager(speaker.DBStore{}, func(eventType string, value any) {
		_, _ = eventBus.Publish(eventType, value)
	})
	return NewWithManagers(configuration, eventBus, listenerManager, speakerManager)
}

func NewWithManagers(configuration config.Config, eventBus *events.Bus, listenerManager *listener.Manager, speakerManager *speaker.Manager) *Server {
	server := &Server{
		config:             configuration,
		id:                 uuid.NewString(),
		started:            time.Now().UTC(),
		events:             eventBus,
		builds:             builds.New(eventBus, configuration.BuildDir),
		listeners:          listenerManager,
		speakers:           speakerManager,
		connections:        make(map[string]map[*websocket.Conn]struct{}),
		lastSeen:           make(map[string]time.Time),
		controlConnections: make(map[*websocket.Conn]struct{}),
	}
	server.upgrader = websocket.Upgrader{
		Subprotocols: []string{teamapi.Subprotocol},
		CheckOrigin: func(request *http.Request) bool {
			origin := request.Header.Get("Origin")
			if origin == "" {
				return true
			}
			parsed, err := url.Parse(origin)
			if err == nil && parsed.Host == request.Host {
				return true
			}
			// Standalone browser clients cannot set an Authorization header on
			// WebSocket upgrades. A valid token-bearing subprotocol both
			// authenticates the request and permits its explicit origin.
			return server.authorized(request)
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", server.health)
	mux.HandleFunc("/api/v1/ws", server.control)
	mux.Handle("/api/v1/loot/", setCors(http.HandlerFunc(server.lootFile)))
	mux.Handle("/api/v1/builds/", setCors(http.HandlerFunc(server.buildFile)))
	mux.HandleFunc("/api/v1/scripts/upload", server.scriptUpload)
	mux.HandleFunc("/api/v1/files/upload", server.fileUpload)
	mux.HandleFunc("/api/v1/interactive/", server.interactive)
	server.http = &http.Server{
		Addr:              configuration.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	return server
}

func (server *Server) Handler() http.Handler {
	return server.http.Handler
}

func (server *Server) ListenAndServe() error {
	if server.config.TLS() {
		return server.http.ListenAndServeTLS(server.config.TLSCert, server.config.TLSKey)
	}
	return server.http.ListenAndServe()
}

func (server *Server) Shutdown(ctx context.Context) error {
	server.closeControlConnections()
	server.speakers.Shutdown()
	listenerErr := server.listeners.Shutdown(ctx)
	httpErr := server.http.Shutdown(ctx)
	controlErr := server.waitForControlConnections(ctx)
	return errors.Join(listenerErr, httpErr, controlErr)
}

func setCors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Access-Control-Allow-Origin", "*")
		writer.Header().Set("Access-Control-Allow-Headers", "Authorization")
		writer.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		writer.Header().Set("Access-Control-Expose-Headers", "Content-Disposition")
		if request.Method == http.MethodOptions {
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", "GET, OPTIONS")
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (server *Server) health(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true, "protocol": teamapi.Version})
}

func (server *Server) authorized(request *http.Request) bool {
	_, ok := server.authenticate(request)
	return ok
}

func (server *Server) authenticate(request *http.Request) (principal, bool) {
	value := ""
	authorization := strings.TrimSpace(request.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authorization), "bearer ") {
		value = strings.TrimSpace(authorization[len("Bearer "):])
	}
	if value == "" {
		for _, protocol := range websocket.Subprotocols(request) {
			if !strings.HasPrefix(protocol, teamapi.BrowserAuthPrefix) {
				continue
			}
			encoded := strings.TrimPrefix(protocol, teamapi.BrowserAuthPrefix)
			decoded, err := base64.RawURLEncoding.DecodeString(encoded)
			if err == nil {
				value = string(decoded)
			}
			break
		}
	}
	if tokenEqual(value, server.config.Token) {
		return principal{Name: "admin", UUID: adminPrincipalID, Admin: true}, true
	}
	user, found, err := db.UserGetByToken(value)
	if err != nil || !found {
		return principal{}, false
	}
	return principal{Name: user.Name, UUID: user.UUID}, true
}

func tokenEqual(left, right string) bool {
	if left == "" || len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func (server *Server) control(writer http.ResponseWriter, request *http.Request) {
	actor, ok := server.authenticate(request)
	if !ok {
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !contains(request.Header.Values("Sec-WebSocket-Protocol"), teamapi.Subprotocol) {
		http.Error(writer, "required WebSocket subprotocol is missing", http.StatusBadRequest)
		return
	}
	connection, err := server.upgrader.Upgrade(writer, request, nil)
	if err != nil {
		return
	}
	if !server.trackControlConnection(connection) {
		_ = connection.Close()
		return
	}
	defer server.releaseControlConnection(connection)
	defer connection.Close()
	connection.SetReadLimit(teamapi.MaxControlMessage)
	_ = connection.SetReadDeadline(time.Now().Add(90 * time.Second))
	connection.SetPongHandler(func(string) error {
		return connection.SetReadDeadline(time.Now().Add(90 * time.Second))
	})

	ctx, cancel := context.WithCancel(request.Context())
	defer cancel()
	outgoing := make(chan []byte, 256)
	go server.writePump(ctx, connection, outgoing)

	eventChannel, unsubscribe := server.events.Subscribe(256)
	defer unsubscribe()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-eventChannel:
				if !ok {
					cancel()
					return
				}
				envelope := teamapi.Envelope{
					Version: teamapi.Version, Type: event.Type, Sequence: event.Sequence,
					Time: event.Time, OK: true, Data: event.Data,
				}
				data, err := json.Marshal(envelope)
				if err != nil {
					continue
				}
				select {
				case outgoing <- data:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	// Subscribe before publishing the connection event so the newly connected
	// client receives its own login event. Register the disconnect defer after
	// the subscription cleanup so observers also see logout before teardown.
	server.userConnected(actor, connection)
	defer server.userDisconnected(actor, connection)

	for {
		_, data, err := connection.ReadMessage()
		if err != nil {
			return
		}
		response := server.handle(data, actor)
		select {
		case outgoing <- response:
		case <-ctx.Done():
			return
		}
	}
}

func (server *Server) trackControlConnection(connection *websocket.Conn) bool {
	server.controlMu.Lock()
	defer server.controlMu.Unlock()
	if server.shuttingDown {
		return false
	}
	server.controlConnections[connection] = struct{}{}
	server.controlWG.Add(1)
	return true
}

func (server *Server) releaseControlConnection(connection *websocket.Conn) {
	server.controlMu.Lock()
	delete(server.controlConnections, connection)
	server.controlMu.Unlock()
	server.controlWG.Done()
}

func (server *Server) closeControlConnections() {
	server.controlMu.Lock()
	server.shuttingDown = true
	connections := make([]*websocket.Conn, 0, len(server.controlConnections))
	for connection := range server.controlConnections {
		connections = append(connections, connection)
	}
	server.controlMu.Unlock()
	for _, connection := range connections {
		_ = connection.Close()
	}
}

func (server *Server) waitForControlConnections(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		server.controlWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (server *Server) writePump(ctx context.Context, connection *websocket.Conn, outgoing <-chan []byte) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-outgoing:
			_ = connection.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := connection.WriteMessage(websocket.TextMessage, data); err != nil {
				_ = connection.Close()
				return
			}
		case <-ticker.C:
			_ = connection.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := connection.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
				_ = connection.Close()
				return
			}
		}
	}
}

func (server *Server) handle(data []byte, actor principal) []byte {
	var request teamapi.Envelope
	if err := json.Unmarshal(data, &request); err != nil {
		return marshalError("", "", "bad_envelope", err)
	}
	replyType, err := teamapi.ReplyType(request.Type)
	if err != nil {
		return marshalError(request.ID, request.Type, "bad_operation", err)
	}
	if request.Version != teamapi.Version {
		return marshalError(request.ID, replyType, "version_mismatch", errors.New("unsupported protocol version"))
	}
	if request.ID == "" || request.ClientID == "" {
		return marshalError(request.ID, replyType, "bad_envelope", errors.New("id and client_id are required"))
	}

	server.dedupeMu.Lock()
	defer server.dedupeMu.Unlock()
	dedupeClientID := actor.UUID + "\x00" + request.ClientID
	if previous, found, err := db.DBRequestGet(dedupeClientID, request.ID); err == nil && found {
		return previous
	}

	value, apiError := server.dispatch(request, actor)
	reply := teamapi.Envelope{
		Version: teamapi.Version, Type: replyType, ID: request.ID,
		ClientID: request.ClientID, Time: time.Now().UTC(), OK: apiError == nil,
		Error: apiError,
	}
	if apiError == nil {
		reply.Data, err = teamapi.MarshalData(value)
		if err != nil {
			reply.OK = false
			reply.Error = &teamapi.APIError{Code: "encode_failed", Message: err.Error()}
		}
	}
	encoded, err := json.Marshal(reply)
	if err != nil {
		return marshalError(request.ID, replyType, "encode_failed", err)
	}
	_ = db.DBRequestSave(dedupeClientID, request.ID, request.Type, encoded)
	return encoded
}

func userMutation(operation string) bool {
	return operation == teamapi.AskUserCreate || operation == teamapi.AskUserUpdate || operation == teamapi.AskUserDelete
}

func newUserToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func (server *Server) userConnected(actor principal, connection *websocket.Conn) {
	now := time.Now().UTC()
	server.connectionsMu.Lock()
	connections := server.connections[actor.UUID]
	first := len(connections) == 0
	if connections == nil {
		connections = make(map[*websocket.Conn]struct{})
		server.connections[actor.UUID] = connections
	}
	connections[connection] = struct{}{}
	server.lastSeen[actor.UUID] = now
	server.connectionsMu.Unlock()
	_ = db.UserSetConnected(actor.UUID, true, now)
	if first {
		server.publishConnectionEvent(teamapi.EventUserLogin, server.userStatus(actor))
	}
}

func (server *Server) userDisconnected(actor principal, connection *websocket.Conn) {
	now := time.Now().UTC()
	server.connectionsMu.Lock()
	connections := server.connections[actor.UUID]
	if connections == nil {
		server.connectionsMu.Unlock()
		return
	}
	delete(connections, connection)
	disconnected := len(connections) == 0
	if disconnected {
		delete(server.connections, actor.UUID)
	}
	server.lastSeen[actor.UUID] = now
	server.connectionsMu.Unlock()
	_ = db.UserSetConnected(actor.UUID, !disconnected, now)
	if disconnected {
		server.publishConnectionEvent(teamapi.EventUserLogout, server.userStatus(actor))
	}
}

func (server *Server) publishConnectionEvent(eventType string, user teamapi.User) {
	// A handler can be constructed without a database in lightweight HTTP
	// tests. Production startup initializes the database before New.
	if server.events == nil || db.DBMS.DBConn == nil {
		return
	}
	_, _ = server.events.Publish(eventType, user)
}

func (server *Server) closeUserConnections(uuid string) bool {
	server.connectionsMu.Lock()
	active := server.connections[uuid]
	if len(active) == 0 {
		server.connectionsMu.Unlock()
		return false
	}
	connections := make([]*websocket.Conn, 0, len(active))
	for connection := range active {
		connections = append(connections, connection)
	}
	delete(server.connections, uuid)
	server.lastSeen[uuid] = time.Now().UTC()
	server.connectionsMu.Unlock()
	for _, connection := range connections {
		_ = connection.Close()
	}
	_ = db.UserSetConnected(uuid, false, time.Now().UTC())
	return true
}

func (server *Server) userStatus(actor principal) teamapi.User {
	if !actor.Admin {
		if user, err := db.UserGet(actor.Name); err == nil {
			server.connectionsMu.RLock()
			user.Connected = len(server.connections[actor.UUID]) > 0
			if lastSeen := server.lastSeen[actor.UUID]; !lastSeen.IsZero() {
				user.LastSeen = lastSeen
			}
			server.connectionsMu.RUnlock()
			return user
		}
	}
	server.connectionsMu.RLock()
	connected := len(server.connections[actor.UUID]) > 0
	lastSeen := server.lastSeen[actor.UUID]
	server.connectionsMu.RUnlock()
	return teamapi.User{
		Name: actor.Name, UUID: actor.UUID, Admin: actor.Admin,
		Connected: connected, Created: server.started, LastSeen: lastSeen,
	}
}

func (server *Server) userList() ([]teamapi.User, error) {
	users, err := db.UserList()
	if err != nil {
		return nil, err
	}
	server.connectionsMu.RLock()
	admin := teamapi.User{
		Name: "admin", UUID: adminPrincipalID, Admin: true,
		Connected: len(server.connections[adminPrincipalID]) > 0,
		Created:   server.started, LastSeen: server.lastSeen[adminPrincipalID],
	}
	for index := range users {
		users[index].Connected = len(server.connections[users[index].UUID]) > 0
		if lastSeen := server.lastSeen[users[index].UUID]; !lastSeen.IsZero() {
			users[index].LastSeen = lastSeen
		}
	}
	server.connectionsMu.RUnlock()
	return append([]teamapi.User{admin}, users...), nil
}

func (server *Server) lootFile(writer http.ResponseWriter, request *http.Request) {
	if !server.authorized(request) {
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}
	parts := strings.Split(strings.TrimPrefix(request.URL.Path, "/api/v1/loot/"), "/")
	if len(parts) != 2 || parts[1] != "content" {
		http.NotFound(writer, request)
		return
	}
	item, content, err := loot.APIContent(parts[0])
	if err != nil {
		http.Error(writer, err.Error(), http.StatusNotFound)
		return
	}
	writer.Header().Set("Content-Type", "application/octet-stream")
	writer.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", path.Base(item.FileName)))
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = writer.Write(content)
}

func (server *Server) buildFile(writer http.ResponseWriter, request *http.Request) {
	if !server.authorized(request) {
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}
	parts := strings.Split(strings.TrimPrefix(request.URL.Path, "/api/v1/builds/"), "/")
	if len(parts) != 2 || parts[1] != "artifact" {
		http.NotFound(writer, request)
		return
	}
	job, fileName, err := server.builds.Artifact(parts[0])
	if err != nil {
		http.Error(writer, err.Error(), http.StatusNotFound)
		return
	}
	writer.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", path.Base(job.ArtifactName)))
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(writer, request, fileName)
}

func (server *Server) interactive(writer http.ResponseWriter, request *http.Request) {
	if !server.authorized(request) {
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := strings.TrimPrefix(request.URL.Path, "/api/v1/interactive/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(writer, request)
		return
	}
	connection, err := server.upgrader.Upgrade(writer, request, nil)
	if err != nil {
		return
	}
	adapter := utils.New(connection)
	if err := interactive.Default.AttachClient(id, adapter); err != nil {
		_ = connection.WriteMessage(websocket.TextMessage, []byte(err.Error()))
		_ = connection.Close()
	}
}

func (server *Server) fileUpload(writer http.ResponseWriter, request *http.Request) {
	if !server.authorized(request) {
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, uploads.MaxSize+1)
	id, err := uploads.Default.Put(request.Body)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusRequestEntityTooLarge)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]string{"upload_id": id})
}

func (server *Server) scriptUpload(writer http.ResponseWriter, request *http.Request) {
	if !server.authorized(request) {
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := filepath.Base(strings.TrimSpace(request.URL.Query().Get("name")))
	if name == "." || name == "" || len(name) > 128 || !strings.HasSuffix(strings.ToLower(name), ".lua") {
		http.Error(writer, "a valid .lua filename is required", http.StatusBadRequest)
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 2<<20)
	content, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(writer, "script exceeds 2 MiB", http.StatusRequestEntityTooLarge)
		return
	}
	if err := os.MkdirAll(server.config.ScriptDir, 0700); err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	fileName := uuid.NewString() + "-" + name
	target := filepath.Join(server.config.ScriptDir, fileName)
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		_ = os.Remove(target)
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(target)
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	absolute, err := filepath.Abs(target)
	if err != nil {
		_ = os.Remove(target)
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]string{"path": absolute})
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if strings.TrimSpace(part) == expected {
				return true
			}
		}
	}
	return false
}

func marshalError(id, replyType, code string, err error) []byte {
	data, _ := json.Marshal(teamapi.Envelope{
		Version: teamapi.Version, Type: replyType, ID: id, Time: time.Now().UTC(),
		Error: &teamapi.APIError{Code: code, Message: err.Error()},
	})
	return data
}
