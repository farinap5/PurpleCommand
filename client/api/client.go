package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"purpcmd/pkg/teamapi"
	"purpcmd/server/utils"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Config struct {
	URL         string
	Token       string
	Timeout     time.Duration
	InsecureTLS bool
}

type result struct {
	envelope teamapi.Envelope
	err      error
}

type Client struct {
	config       Config
	clientID     string
	connectMu    sync.Mutex
	mu           sync.Mutex
	connection   *websocket.Conn
	pending      map[string]chan result
	writeMu      sync.Mutex
	events       chan teamapi.Envelope
	closed       bool
	lastSequence uint64
}

func New(configuration Config) *Client {
	if configuration.URL == "" {
		configuration.URL = "http://127.0.0.1:8080"
	}
	if configuration.Timeout <= 0 {
		configuration.Timeout = 30 * time.Second
	}
	return &Client{
		config:   configuration,
		clientID: uuid.NewString(),
		pending:  make(map[string]chan result),
		events:   make(chan teamapi.Envelope, 256),
	}
}

func (client *Client) Events() <-chan teamapi.Envelope { return client.events }

func (client *Client) Connect(ctx context.Context) error {
	client.connectMu.Lock()
	defer client.connectMu.Unlock()
	client.mu.Lock()
	if client.closed {
		client.mu.Unlock()
		return errors.New("client is closed")
	}
	if client.connection != nil {
		client.mu.Unlock()
		return nil
	}
	client.mu.Unlock()

	endpoint, err := client.endpoint("/api/v1/ws", true)
	if err != nil {
		return err
	}
	dialer := websocket.Dialer{
		HandshakeTimeout: client.config.Timeout,
		Subprotocols:     []string{teamapi.Subprotocol},
		TLSClientConfig:  &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: client.config.InsecureTLS},
	}
	headers := http.Header{"Authorization": {"Bearer " + client.config.Token}}
	connection, response, err := dialer.DialContext(ctx, endpoint, headers)
	if err != nil {
		if response != nil {
			return fmt.Errorf("connect: %s: %w", response.Status, err)
		}
		return err
	}
	connection.SetReadLimit(teamapi.MaxControlMessage)
	client.mu.Lock()
	client.connection = connection
	client.mu.Unlock()
	go client.readLoop(connection)
	return nil
}

func (client *Client) Request(ctx context.Context, operation string, request, response any) error {
	data, err := teamapi.MarshalData(request)
	if err != nil {
		return err
	}
	envelope := teamapi.Envelope{
		Version:  teamapi.Version,
		Type:     operation,
		ID:       uuid.NewString(),
		ClientID: client.clientID,
		Time:     time.Now().UTC(),
		Data:     data,
	}
	var reply teamapi.Envelope
	for attempt := 0; attempt < 2; attempt++ {
		if err := client.Connect(ctx); err != nil {
			return err
		}
		reply, err = client.requestOnce(ctx, envelope)
		if err == nil {
			break
		}
		client.disconnectCurrent()
	}
	if err != nil {
		return err
	}
	if reply.Error != nil {
		return reply.Error
	}
	if !reply.OK {
		return errors.New("teamserver returned an unsuccessful reply")
	}
	if response == nil {
		return nil
	}
	return teamapi.DecodeData(reply, response)
}

func (client *Client) requestOnce(ctx context.Context, envelope teamapi.Envelope) (teamapi.Envelope, error) {
	waiter := make(chan result, 1)
	client.mu.Lock()
	connection := client.connection
	if connection == nil {
		client.mu.Unlock()
		return teamapi.Envelope{}, errors.New("not connected")
	}
	client.pending[envelope.ID] = waiter
	client.mu.Unlock()
	defer func() {
		client.mu.Lock()
		delete(client.pending, envelope.ID)
		client.mu.Unlock()
	}()

	data, err := json.Marshal(envelope)
	if err != nil {
		return teamapi.Envelope{}, err
	}
	client.writeMu.Lock()
	_ = connection.SetWriteDeadline(time.Now().Add(client.config.Timeout))
	err = connection.WriteMessage(websocket.TextMessage, data)
	client.writeMu.Unlock()
	if err != nil {
		return teamapi.Envelope{}, err
	}
	select {
	case <-ctx.Done():
		return teamapi.Envelope{}, ctx.Err()
	case reply := <-waiter:
		return reply.envelope, reply.err
	}
}

func (client *Client) readLoop(connection *websocket.Conn) {
	for {
		_, data, err := connection.ReadMessage()
		if err != nil {
			client.connectionFailed(connection, err)
			return
		}
		var envelope teamapi.Envelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			continue
		}
		if strings.HasPrefix(envelope.Type, "evt.") {
			client.acceptEvent(envelope)
			continue
		}
		client.mu.Lock()
		waiter := client.pending[envelope.ID]
		client.mu.Unlock()
		if waiter != nil {
			waiter <- result{envelope: envelope}
		}
	}
}

func (client *Client) connectionFailed(connection *websocket.Conn, err error) {
	client.mu.Lock()
	if client.connection != connection {
		client.mu.Unlock()
		return
	}
	client.connection = nil
	waiters := make([]chan result, 0, len(client.pending))
	for _, waiter := range client.pending {
		waiters = append(waiters, waiter)
	}
	client.mu.Unlock()
	for _, waiter := range waiters {
		select {
		case waiter <- result{err: err}:
		default:
		}
	}
	_ = connection.Close()
}

func (client *Client) disconnectCurrent() {
	client.mu.Lock()
	connection := client.connection
	client.connection = nil
	client.mu.Unlock()
	if connection != nil {
		_ = connection.Close()
	}
}

func (client *Client) Hello(ctx context.Context) (teamapi.HelloReply, error) {
	client.mu.Lock()
	last := client.lastSequence
	client.mu.Unlock()
	var reply teamapi.HelloReply
	if err := client.Request(ctx, teamapi.AskSystemHello, teamapi.HelloRequest{LastEventSequence: last}, &reply); err != nil {
		return teamapi.HelloReply{}, err
	}
	if reply.ResyncRequired {
		cursor := last
		for cursor < reply.EventSequence {
			var records []teamapi.EventRecord
			if err := client.Request(ctx, teamapi.AskEventReplay, teamapi.EventReplayRequest{After: cursor, Limit: 1000}, &records); err != nil {
				return teamapi.HelloReply{}, err
			}
			if len(records) == 0 {
				reply.HistoryTruncated = true
				break
			}
			if eventReplayPageHasGap(cursor, records) {
				reply.HistoryTruncated = true
			}
			for _, record := range records {
				client.acceptEvent(teamapi.Envelope{
					Version: teamapi.Version, Type: record.Type, Sequence: record.Sequence,
					Time: record.Time, OK: true, Data: record.Data,
				})
				cursor = record.Sequence
			}
			if len(records) < 1000 {
				break
			}
		}
		if cursor < reply.EventSequence {
			reply.HistoryTruncated = true
		}
	}
	return reply, nil
}

func eventReplayPageHasGap(after uint64, records []teamapi.EventRecord) bool {
	cursor := after
	for _, record := range records {
		if record.Sequence > cursor && record.Sequence-cursor > 1 {
			return true
		}
		if record.Sequence > cursor {
			cursor = record.Sequence
		}
	}
	return false
}

func (client *Client) acceptEvent(envelope teamapi.Envelope) {
	client.mu.Lock()
	if envelope.Sequence <= client.lastSequence {
		client.mu.Unlock()
		return
	}
	client.lastSequence = envelope.Sequence
	client.mu.Unlock()
	select {
	case client.events <- envelope:
	default:
	}
}

func (client *Client) Snapshot(ctx context.Context) (teamapi.Snapshot, error) {
	var snapshot teamapi.Snapshot
	err := client.Request(ctx, teamapi.AskSystemSnapshot, struct{}{}, &snapshot)
	if err == nil {
		client.mu.Lock()
		if snapshot.EventSequence > client.lastSequence {
			client.lastSequence = snapshot.EventSequence
		}
		client.mu.Unlock()
	}
	return snapshot, err
}

func (client *Client) Users(ctx context.Context) ([]teamapi.User, error) {
	var users []teamapi.User
	err := client.Request(ctx, teamapi.AskUserList, struct{}{}, &users)
	return users, err
}

func (client *Client) SendUserMessage(ctx context.Context, message string) (teamapi.UserMessage, error) {
	var broadcast teamapi.UserMessage
	err := client.Request(ctx, teamapi.AskUserMessage, teamapi.UserMessageRequest{Message: message}, &broadcast)
	return broadcast, err
}

func (client *Client) CreateUser(ctx context.Context, name string) (teamapi.UserCredentials, error) {
	var credentials teamapi.UserCredentials
	err := client.Request(ctx, teamapi.AskUserCreate, teamapi.UserCreateRequest{Name: name}, &credentials)
	return credentials, err
}

func (client *Client) RefreshUserToken(ctx context.Context, name string) (teamapi.UserCredentials, error) {
	var credentials teamapi.UserCredentials
	err := client.Request(ctx, teamapi.AskUserUpdate, teamapi.UserUpdateRequest{Name: name}, &credentials)
	return credentials, err
}

func (client *Client) UpdateUserToken(ctx context.Context, name string) (teamapi.UserCredentials, error) {
	return client.RefreshUserToken(ctx, name)
}

func (client *Client) DeleteUser(ctx context.Context, name string) (teamapi.User, error) {
	var user teamapi.User
	err := client.Request(ctx, teamapi.AskUserDelete, teamapi.NameRequest{Name: name}, &user)
	return user, err
}

func (client *Client) Builds(ctx context.Context) ([]teamapi.Build, error) {
	var builds []teamapi.Build
	err := client.Request(ctx, teamapi.AskBuildList, struct{}{}, &builds)
	return builds, err
}

func (client *Client) DeleteBuild(ctx context.Context, id string) (teamapi.Build, error) {
	var build teamapi.Build
	err := client.Request(ctx, teamapi.AskBuildDelete, teamapi.BuildDeleteRequest{ID: id}, &build)
	return build, err
}

func (client *Client) Download(ctx context.Context, remotePath, destination string) error {
	endpoint, err := client.endpoint(remotePath, false)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+client.config.Token)
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: client.config.InsecureTLS}}
	response, err := (&http.Client{Timeout: client.config.Timeout, Transport: transport}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("download: %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(absolute, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, response.Body)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func (client *Client) UploadAttachment(ctx context.Context, localPath string) (string, error) {
	info, err := os.Stat(localPath)
	if err != nil {
		return "", err
	}
	if info.Size() > 64<<20 {
		return "", errors.New("attachment exceeds 64 MiB")
	}
	file, err := os.Open(localPath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	endpoint, err := client.endpoint("/api/v1/files/upload", false)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, file)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+client.config.Token)
	request.Header.Set("Content-Type", "application/octet-stream")
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: client.config.InsecureTLS}}
	response, err := (&http.Client{Timeout: 10 * time.Minute, Transport: transport}).Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return "", fmt.Errorf("upload: %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	var result struct {
		UploadID string `json:"upload_id"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result); err != nil {
		return "", err
	}
	if result.UploadID == "" {
		return "", errors.New("teamserver returned an empty upload ID")
	}
	return result.UploadID, nil
}

func (client *Client) UploadScript(ctx context.Context, localPath string) (string, error) {
	info, err := os.Stat(localPath)
	if err != nil {
		return "", err
	}
	if info.Size() > 2<<20 {
		return "", errors.New("Lua script exceeds 2 MiB")
	}
	file, err := os.Open(localPath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	endpoint, err := client.endpoint("/api/v1/scripts/upload?name="+url.QueryEscape(filepath.Base(localPath)), false)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, file)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+client.config.Token)
	request.Header.Set("Content-Type", "application/octet-stream")
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: client.config.InsecureTLS}}
	response, err := (&http.Client{Timeout: client.config.Timeout, Transport: transport}).Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return "", fmt.Errorf("upload: %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	var result struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result); err != nil {
		return "", err
	}
	if result.Path == "" {
		return "", errors.New("teamserver returned an empty script path")
	}
	return result.Path, nil
}

func (client *Client) Interactive(ctx context.Context, remotePath string) (net.Conn, error) {
	endpoint, err := client.endpoint(remotePath, true)
	if err != nil {
		return nil, err
	}
	dialer := websocket.Dialer{
		HandshakeTimeout: client.config.Timeout,
		Subprotocols:     []string{teamapi.Subprotocol},
		TLSClientConfig:  &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: client.config.InsecureTLS},
	}
	connection, response, err := dialer.DialContext(ctx, endpoint, http.Header{"Authorization": {"Bearer " + client.config.Token}})
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf("interactive: %s: %w", response.Status, err)
		}
		return nil, err
	}
	return utils.New(connection), nil
}

func (client *Client) endpoint(resource string, websocketEndpoint bool) (string, error) {
	base, err := url.Parse(client.config.URL)
	if err != nil {
		return "", err
	}
	if websocketEndpoint {
		switch base.Scheme {
		case "http":
			base.Scheme = "ws"
		case "https":
			base.Scheme = "wss"
		case "ws", "wss":
		default:
			return "", fmt.Errorf("unsupported URL scheme %q", base.Scheme)
		}
	} else {
		if base.Scheme == "ws" {
			base.Scheme = "http"
		}
		if base.Scheme == "wss" {
			base.Scheme = "https"
		}
	}
	reference, err := url.Parse(resource)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(reference).String(), nil
}

func (client *Client) Close() error {
	client.mu.Lock()
	client.closed = true
	connection := client.connection
	client.connection = nil
	client.mu.Unlock()
	if connection != nil {
		return connection.Close()
	}
	return nil
}
