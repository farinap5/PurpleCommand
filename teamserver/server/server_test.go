package server_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	clientapi "purpcmd/client/api"
	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"
	"purpcmd/server/listener"
	"purpcmd/teamserver/config"
	"purpcmd/teamserver/events"
	teamserver "purpcmd/teamserver/server"

	"github.com/gorilla/websocket"
)

func TestControlAuthenticationCorrelationDeduplicationAndReplay(t *testing.T) {
	db.DatabasePath = t.TempDir() + "/teamserver.db"
	if err := db.CheckDB(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DBMS.DBConn.Close() })
	if err := db.EnsureTeamserverSchema(); err != nil {
		t.Fatal(err)
	}

	const token = "test-token-that-is-long-enough"
	eventBus := events.New()
	configuration := config.Config{Token: token, ScriptDir: t.TempDir()}
	instance := teamserver.New(configuration, eventBus)
	httpServer := httptest.NewServer(instance.Handler())
	defer httpServer.Close()

	response, err := http.Get(httpServer.URL + "/api/v1/ws")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", response.StatusCode)
	}

	endpoint := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/api/v1/ws"
	connection, _, err := (&websocket.Dialer{Subprotocols: []string{teamapi.Subprotocol}}).Dial(
		endpoint, http.Header{"Authorization": {"Bearer " + token}},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()

	requestData, _ := teamapi.MarshalData(teamapi.ListenerCreateRequest{Name: "integration", Host: "127.0.0.1", Port: "0"})
	request := teamapi.Envelope{
		Version: teamapi.Version, Type: teamapi.AskListenerCreate,
		ID: "same-request-id", ClientID: "integration-client", Data: requestData,
	}
	first := writeAndReadReply(t, connection, request)
	if !first.OK || first.Type != "rpy.listener.create" {
		t.Fatalf("first reply = %#v", first)
	}
	second := writeAndReadReply(t, connection, request)
	if !second.OK || string(first.Data) != string(second.Data) {
		t.Fatalf("deduplicated reply differs: first=%s second=%s", first.Data, second.Data)
	}

	client := clientapi.New(clientapi.Config{URL: httpServer.URL, Token: token, Timeout: 5 * time.Second})
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var listeners []teamapi.Listener
	if err := client.Request(ctx, teamapi.AskListenerList, struct{}{}, &listeners); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, item := range listeners {
		if item.Name == "integration" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("deduplicated create produced %d listeners", count)
	}

	replayClient := clientapi.New(clientapi.Config{URL: httpServer.URL, Token: token, Timeout: 5 * time.Second})
	defer replayClient.Close()
	hello, err := replayClient.Hello(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !hello.ResyncRequired || hello.EventSequence == 0 {
		t.Fatalf("hello did not require replay: %#v", hello)
	}
replayLoop:
	for {
		select {
		case event := <-replayClient.Events():
			if event.Type == teamapi.EventListenerCreated {
				break replayLoop
			}
		case <-ctx.Done():
			t.Fatal("replayed listener event was not delivered")
		}
	}
	if err := listener.APIDelete("integration"); err != nil {
		t.Fatal(err)
	}
}

func TestControlAcceptsBrowserAuthenticationSubprotocol(t *testing.T) {
	const token = "browser-token-that-is-long-enough"
	instance := teamserver.New(config.Config{Token: token, ScriptDir: t.TempDir()}, events.New())
	httpServer := httptest.NewServer(instance.Handler())
	defer httpServer.Close()

	endpoint := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/api/v1/ws"
	authProtocol := teamapi.BrowserAuthPrefix + base64.RawURLEncoding.EncodeToString([]byte(token))
	dialer := websocket.Dialer{Subprotocols: []string{teamapi.Subprotocol, authProtocol}}
	requestHeader := http.Header{"Origin": {"http://127.0.0.1:5173"}}
	connection, response, err := dialer.Dial(endpoint, requestHeader)
	if err != nil {
		if response != nil {
			t.Fatalf("browser websocket: %s: %v", response.Status, err)
		}
		t.Fatal(err)
	}
	defer connection.Close()
	if connection.Subprotocol() != teamapi.Subprotocol {
		t.Fatalf("selected subprotocol = %q", connection.Subprotocol())
	}
}

func TestLootDownloadSupportsBrowserPreflightAndKeepsAuthentication(t *testing.T) {
	const token = "browser-download-token-that-is-long-enough"
	instance := teamserver.New(config.Config{Token: token, ScriptDir: t.TempDir()}, events.New())
	httpServer := httptest.NewServer(instance.Handler())
	defer httpServer.Close()

	preflight, err := http.NewRequest(http.MethodOptions, httpServer.URL+"/api/v1/loot/test/content", nil)
	if err != nil {
		t.Fatal(err)
	}
	preflight.Header.Set("Origin", "http://127.0.0.1:5173")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodGet)
	preflight.Header.Set("Access-Control-Request-Headers", "authorization")
	response, err := http.DefaultClient.Do(preflight)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight status = %d", response.StatusCode)
	}
	if response.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("allow origin = %q", response.Header.Get("Access-Control-Allow-Origin"))
	}
	if !strings.Contains(response.Header.Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Fatalf("allow headers = %q", response.Header.Get("Access-Control-Allow-Headers"))
	}

	request, err := http.NewRequest(http.MethodGet, httpServer.URL+"/api/v1/loot/test/content", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", "http://127.0.0.1:5173")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated download status = %d", response.StatusCode)
	}
	if response.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("download allow origin = %q", response.Header.Get("Access-Control-Allow-Origin"))
	}
}

func TestUserManagementAuthorizationTokensAndConnections(t *testing.T) {
	db.DatabasePath = t.TempDir() + "/users.db"
	if err := db.CheckDB(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DBMS.DBConn.Close() })
	if err := db.EnsureTeamserverSchema(); err != nil {
		t.Fatal(err)
	}

	const adminToken = "admin-token-that-is-long-enough"
	instance := teamserver.New(config.Config{Token: adminToken, ScriptDir: t.TempDir()}, events.New())
	httpServer := httptest.NewServer(instance.Handler())
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	admin := clientapi.New(clientapi.Config{URL: httpServer.URL, Token: adminToken, Timeout: 5 * time.Second})
	defer admin.Close()
	var created teamapi.UserCredentials
	if err := admin.Request(ctx, teamapi.AskUserCreate, teamapi.UserCreateRequest{Name: "alice"}, &created); err != nil {
		t.Fatal(err)
	}
	if created.User.Name != "alice" || len(created.Token) < 20 || created.User.Connected {
		t.Fatalf("created credentials = %#v", created)
	}
	assertUserEvent(t, ctx, admin.Events(), teamapi.EventUserLogin, "admin")
	assertUserEvent(t, ctx, admin.Events(), teamapi.EventUserCreated, "alice")

	userClient := clientapi.New(clientapi.Config{URL: httpServer.URL, Token: created.Token, Timeout: 5 * time.Second})
	defer userClient.Close()
	var users []teamapi.User
	if err := userClient.Request(ctx, teamapi.AskUserList, struct{}{}, &users); err != nil {
		t.Fatal(err)
	}
	assertConnected(t, users, "admin", true)
	assertConnected(t, users, "alice", true)
	assertUserEvent(t, ctx, userClient.Events(), teamapi.EventUserLogin, "alice")
	assertUserEvent(t, ctx, admin.Events(), teamapi.EventUserLogin, "alice")

	for _, test := range []struct {
		operation string
		request   any
	}{
		{teamapi.AskUserCreate, teamapi.UserCreateRequest{Name: "bob"}},
		{teamapi.AskUserUpdate, teamapi.UserUpdateRequest{Name: "alice"}},
		{teamapi.AskUserDelete, teamapi.NameRequest{Name: "alice"}},
	} {
		var response any
		err := userClient.Request(ctx, test.operation, test.request, &response)
		var apiError *teamapi.APIError
		if !errors.As(err, &apiError) || apiError.Code != "forbidden" {
			t.Fatalf("%s error = %#v", test.operation, err)
		}
	}

	var rejected teamapi.UserCredentials
	err := admin.Request(ctx, teamapi.AskUserUpdate, map[string]string{
		"name": "alice", "token": "caller-selected-token",
	}, &rejected)
	if err == nil {
		t.Fatal("user update accepted a caller-selected token")
	}

	var refreshed teamapi.UserCredentials
	if err := admin.Request(ctx, teamapi.AskUserUpdate, teamapi.UserUpdateRequest{Name: "alice"}, &refreshed); err != nil {
		t.Fatal(err)
	}
	if refreshed.Token == created.Token || len(refreshed.Token) < 20 {
		t.Fatalf("refreshed token = %q", refreshed.Token)
	}
	if refreshed.User.Connected {
		t.Fatal("token refresh did not revoke the existing websocket")
	}
	assertUserEvent(t, ctx, admin.Events(), teamapi.EventUserLogout, "alice")
	assertUserEvent(t, ctx, admin.Events(), teamapi.EventUserUpdated, "alice")
	assertWebsocketAuthentication(t, httpServer.URL, created.Token, http.StatusUnauthorized)

	refreshedClient := clientapi.New(clientapi.Config{URL: httpServer.URL, Token: refreshed.Token, Timeout: 5 * time.Second})
	defer refreshedClient.Close()
	users = nil
	if err := refreshedClient.Request(ctx, teamapi.AskUserList, struct{}{}, &users); err != nil {
		t.Fatal(err)
	}
	assertConnected(t, users, "alice", true)
	assertUserEvent(t, ctx, refreshedClient.Events(), teamapi.EventUserLogin, "alice")
	assertUserEvent(t, ctx, admin.Events(), teamapi.EventUserLogin, "alice")

	var deleted teamapi.User
	if err := admin.Request(ctx, teamapi.AskUserDelete, teamapi.NameRequest{Name: "alice"}, &deleted); err != nil {
		t.Fatal(err)
	}
	if deleted.Name != "alice" || deleted.Connected {
		t.Fatalf("deleted user = %#v", deleted)
	}
	assertUserEvent(t, ctx, admin.Events(), teamapi.EventUserDeleted, "alice")
	assertWebsocketAuthentication(t, httpServer.URL, refreshed.Token, http.StatusUnauthorized)

	users = nil
	if err := admin.Request(ctx, teamapi.AskUserList, struct{}{}, &users); err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].Name != "admin" || !users[0].Admin {
		t.Fatalf("users after deletion = %#v", users)
	}
}

func assertUserEvent(t *testing.T, ctx context.Context, channel <-chan teamapi.Envelope, eventType, name string) teamapi.User {
	t.Helper()
	for {
		select {
		case event, ok := <-channel:
			if !ok {
				t.Fatalf("event channel closed before %s for %s", eventType, name)
			}
			if event.Type != eventType {
				continue
			}
			var user teamapi.User
			if err := teamapi.DecodeData(event, &user); err != nil {
				t.Fatalf("decode %s: %v", eventType, err)
			}
			if user.Name == name {
				return user
			}
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s for %s", eventType, name)
		}
	}
}

func assertConnected(t *testing.T, users []teamapi.User, name string, expected bool) {
	t.Helper()
	for _, user := range users {
		if user.Name == name {
			if user.Connected != expected {
				t.Fatalf("user %q connected = %t, want %t", name, user.Connected, expected)
			}
			return
		}
	}
	t.Fatalf("user %q not found in %#v", name, users)
}

func assertWebsocketAuthentication(t *testing.T, serverURL, token string, expectedStatus int) {
	t.Helper()
	endpoint := "ws" + strings.TrimPrefix(serverURL, "http") + "/api/v1/ws"
	connection, response, err := (&websocket.Dialer{Subprotocols: []string{teamapi.Subprotocol}}).Dial(
		endpoint, http.Header{"Authorization": {"Bearer " + token}},
	)
	if connection != nil {
		_ = connection.Close()
	}
	if err == nil {
		t.Fatalf("token unexpectedly authenticated")
	}
	if response == nil {
		t.Fatalf("authentication failed without HTTP response: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != expectedStatus {
		t.Fatalf("authentication status = %d, want %d", response.StatusCode, expectedStatus)
	}
}

func writeAndReadReply(t *testing.T, connection *websocket.Conn, request teamapi.Envelope) teamapi.Envelope {
	t.Helper()
	if err := connection.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
	for {
		_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, data, err := connection.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var reply teamapi.Envelope
		if err := json.Unmarshal(data, &reply); err != nil {
			t.Fatal(err)
		}
		if reply.ID == request.ID {
			return reply
		}
	}
}
