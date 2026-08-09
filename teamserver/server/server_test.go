package server_test

import (
	"context"
	"encoding/json"
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
	select {
	case event := <-replayClient.Events():
		if event.Type != teamapi.EventListenerCreated {
			t.Fatalf("replayed event type = %s", event.Type)
		}
	case <-ctx.Done():
		t.Fatal("replayed event was not delivered")
	}

	if err := listener.APIDelete("integration"); err != nil {
		t.Fatal(err)
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
