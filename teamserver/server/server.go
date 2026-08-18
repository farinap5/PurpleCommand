package server

import (
	"context"
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

	"purpcmd/internal"
	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"
	"purpcmd/server/implant"
	"purpcmd/server/implantbuilder"
	"purpcmd/server/interactive"
	"purpcmd/server/listener"
	"purpcmd/server/loot"
	"purpcmd/server/lua"
	"purpcmd/server/uploads"
	"purpcmd/server/utils"
	"purpcmd/teamserver/builds"
	"purpcmd/teamserver/config"
	"purpcmd/teamserver/events"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Server struct {
	config   config.Config
	id       string
	events   *events.Bus
	builds   *builds.Manager
	http     *http.Server
	dedupeMu sync.Mutex
	upgrader websocket.Upgrader
}

func New(configuration config.Config, eventBus *events.Bus) *Server {
	server := &Server{
		config: configuration,
		id:     uuid.NewString(),
		events: eventBus,
		builds: builds.New(eventBus),
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
	mux.HandleFunc("/api/v1/loot/", server.lootFile)
	mux.HandleFunc("/api/v1/builds/", server.buildFile)
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
	return server.http.Shutdown(ctx)
}

func (server *Server) health(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true, "protocol": teamapi.Version})
}

func (server *Server) authorized(request *http.Request) bool {
	value := strings.TrimSpace(strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer "))
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
	if len(value) != len(server.config.Token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(value), []byte(server.config.Token)) == 1
}

func (server *Server) control(writer http.ResponseWriter, request *http.Request) {
	if !server.authorized(request) {
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

	for {
		_, data, err := connection.ReadMessage()
		if err != nil {
			return
		}
		response := server.handle(data)
		select {
		case outgoing <- response:
		case <-ctx.Done():
			return
		}
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

func (server *Server) handle(data []byte) []byte {
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
	if previous, found, err := db.DBRequestGet(request.ClientID, request.ID); err == nil && found {
		return previous
	}

	value, apiError := server.dispatch(request)
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
	_ = db.DBRequestSave(request.ClientID, request.ID, request.Type, encoded)
	return encoded
}

func (server *Server) dispatch(envelope teamapi.Envelope) (any, *teamapi.APIError) {
	fail := func(err error) (any, *teamapi.APIError) {
		return nil, &teamapi.APIError{Code: "request_failed", Message: err.Error()}
	}
	publish := func(eventType string, value any) {
		_, _ = server.events.Publish(eventType, value)
	}
	switch envelope.Type {
	case teamapi.AskSystemHello:
		var request teamapi.HelloRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		latest, err := server.events.LatestSequence()
		if err != nil {
			return fail(err)
		}
		return teamapi.HelloReply{ServerID: server.id, ServerVersion: "dev", Protocol: teamapi.Version, EventSequence: latest, ResyncRequired: request.LastEventSequence < latest}, nil
	case teamapi.AskSystemHealth:
		return map[string]any{"ok": true, "time": time.Now().UTC()}, nil
	case teamapi.AskSystemSnapshot:
		latest, err := server.events.LatestSequence()
		if err != nil {
			return fail(err)
		}
		return teamapi.Snapshot{
			Listeners: listener.APIList(), Sessions: implant.APIListSessions(),
			Scripts: lua.APIListScripts(), Profiles: implantbuilder.APIListProfiles(),
			Commands: lua.APIListCommands(""), EventSequence: latest,
		}, nil
	case teamapi.AskListenerList:
		return listener.APIList(), nil
	case teamapi.AskListenerGet:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := listener.APIGet(request.Name)
		if err != nil {
			return fail(err)
		}
		return item, nil
	case teamapi.AskListenerCreate:
		var request teamapi.ListenerCreateRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := listener.APICreate(request)
		if err != nil {
			return fail(err)
		}
		publish(teamapi.EventListenerCreated, item)
		return item, nil
	case teamapi.AskListenerUpdate:
		var request teamapi.ListenerUpdateRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := listener.APIUpdate(request)
		if err != nil {
			return fail(err)
		}
		return item, nil
	case teamapi.AskListenerStart:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := listener.APIStart(request.Name)
		if err != nil {
			return fail(err)
		}
		publish(teamapi.EventListenerStarted, item)
		return item, nil
	case teamapi.AskListenerStop:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := listener.APIStop(request.Name)
		if err != nil {
			return fail(err)
		}
		publish(teamapi.EventListenerStopped, item)
		return item, nil
	case teamapi.AskListenerRestart:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := listener.APIRestart(request.Name)
		if err != nil {
			return fail(err)
		}
		publish(teamapi.EventListenerStarted, item)
		return item, nil
	case teamapi.AskListenerDelete:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		if err := listener.APIDelete(request.Name); err != nil {
			return fail(err)
		}
		return map[string]string{"name": request.Name}, nil
	case teamapi.AskSessionList:
		return implant.APIListSessions(), nil
	case teamapi.AskSessionGet:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := implant.APIGetSession(request.Name)
		if err != nil {
			return fail(err)
		}
		return item, nil
	case teamapi.AskSessionTerminate:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := implant.APIRequestTermination(request.Name)
		if err != nil && !errors.Is(err, implant.ErrTerminationPending) {
			return fail(err)
		}
		return item, nil
	case teamapi.AskSessionDelete:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		if err := implant.APIDeleteSession(request.Name); err != nil {
			return fail(err)
		}
		publish(teamapi.EventSessionDeleted, map[string]string{"name": request.Name})
		return map[string]string{"name": request.Name}, nil
	case teamapi.AskCommandList:
		var request teamapi.CommandListRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		return lua.APIListCommands(request.PayloadType), nil
	case teamapi.AskCommandExecute:
		var request teamapi.CommandExecuteRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		result, err := lua.APIExecuteCommand(request)
		if err != nil {
			return fail(err)
		}
		return result, nil
	case teamapi.AskTaskCreate:
		var request teamapi.TaskCreateRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := implant.APICreateTask(request)
		if err != nil {
			return fail(err)
		}
		return item, nil
	case teamapi.AskTaskList:
		var request teamapi.TaskListRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		items, err := implant.APIListTasks(request.Session)
		if err != nil {
			return fail(err)
		}
		return items, nil
	case teamapi.AskTaskGet:
		var request teamapi.TaskGetRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := implant.APIGetTask(request.Session, request.TaskID)
		if err != nil {
			return fail(err)
		}
		return item, nil
	case teamapi.AskLootList:
		items, err := loot.APIList()
		if err != nil {
			return fail(err)
		}
		return items, nil
	case teamapi.AskLootGet:
		var request teamapi.LootRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := loot.APIGet(request.UUID)
		if err != nil {
			return fail(err)
		}
		return teamapi.LootGetReply{Loot: item, DownloadURL: "/api/v1/loot/" + item.UUID + "/content"}, nil
	case teamapi.AskLootDelete:
		var request teamapi.LootRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := loot.APIDelete(request.UUID)
		if err != nil {
			return fail(err)
		}
		return item, nil
	case teamapi.AskScriptList:
		return lua.APIListScripts(), nil
	case teamapi.AskScriptLoad:
		var request teamapi.ScriptLoadRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := lua.APILoadScript(request.Path)
		if err != nil {
			return fail(err)
		}
		publish(teamapi.EventScriptLoaded, item)
		return item, nil
	case teamapi.AskScriptUnload:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		if err := lua.APIUnloadScript(request.Name); err != nil {
			return fail(err)
		}
		publish(teamapi.EventScriptUnloaded, map[string]string{"path": request.Name})
		return map[string]string{"path": request.Name}, nil
	case teamapi.AskProfileList:
		return implantbuilder.APIListProfiles(), nil
	case teamapi.AskProfileGet:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := implantbuilder.APIGetProfile(request.Name)
		if err != nil {
			return fail(err)
		}
		return item, nil
	case teamapi.AskProfileCreate:
		var request teamapi.Profile
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := implantbuilder.APICreateProfile(request)
		if err != nil {
			return fail(err)
		}
		return item, nil
	case teamapi.AskProfileUpdate:
		var request teamapi.ProfileUpdateRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := implantbuilder.APIUpdateProfile(request)
		if err != nil {
			return fail(err)
		}
		return item, nil
	case teamapi.AskProfileDelete:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		if err := implantbuilder.APIDeleteProfile(request.Name); err != nil {
			return fail(err)
		}
		return map[string]string{"name": request.Name}, nil
	case teamapi.AskBuildCreate:
		var request teamapi.BuildRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := server.builds.Create(request.Profile)
		if err != nil {
			return fail(err)
		}
		return item, nil
	case teamapi.AskBuildGet:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := server.builds.Get(request.Name)
		if err != nil {
			return fail(err)
		}
		return item, nil
	case teamapi.AskBuildList:
		return server.builds.List(), nil
	case teamapi.AskInteractiveOpen:
		var request teamapi.InteractiveOpenRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		if _, err := implant.APIGetSession(request.Session); err != nil {
			return fail(err)
		}
		streamID, expires := interactive.Default.Open(request.Session)
		_, err := implant.APICreateTask(teamapi.TaskCreateRequest{Session: request.Session, Code: uint16(internal.SSH), Payload: []byte(streamID)})
		if err != nil {
			interactive.Default.Close(streamID)
			return fail(err)
		}
		return teamapi.InteractiveOpenReply{StreamID: streamID, URL: "/api/v1/interactive/" + streamID, Expires: expires}, nil
	case teamapi.AskInteractiveClose:
		var request teamapi.InteractiveCloseRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		interactive.Default.Close(request.StreamID)
		return map[string]string{"stream_id": request.StreamID}, nil
	case teamapi.AskEventReplay:
		var request teamapi.EventReplayRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		items, err := server.events.Replay(request.After, request.Limit)
		if err != nil {
			return fail(err)
		}
		return items, nil
	case teamapi.AskEventAck:
		return map[string]bool{"acknowledged": true}, nil
	default:
		return fail(fmt.Errorf("unknown operation %q", envelope.Type))
	}
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
