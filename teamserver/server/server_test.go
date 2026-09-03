package server_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clientapi "purpcmd/client/api"
	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"
	"purpcmd/server/implantbuilder"
	"purpcmd/teamserver/config"
	"purpcmd/teamserver/events"
	teamserver "purpcmd/teamserver/server"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

func cleanupTeamserver(t *testing.T, instance *teamserver.Server) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = instance.Shutdown(ctx)
	})
}

func setupTeamserverDatabase(t *testing.T, name string) {
	t.Helper()
	previousPath := db.DatabasePath
	previousDatabase := db.DBMS
	db.DatabasePath = filepath.Join(t.TempDir(), name)
	if err := db.CheckDB(); err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureTeamserverSchema(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.DBMS.DBConn.Close()
		db.DBMS = previousDatabase
		db.DatabasePath = previousPath
	})
}

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
	cleanupTeamserver(t, instance)
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
	var driverTypes []teamapi.ListenerDriverDefinition
	if err := client.Request(ctx, teamapi.AskListenerTypeList, struct{}{}, &driverTypes); err != nil {
		t.Fatal(err)
	}
	if len(driverTypes) != 1 || driverTypes[0].ID != "http" || len(driverTypes[0].Options) == 0 {
		t.Fatalf("listener driver definitions = %#v", driverTypes)
	}
	var carrierTypes []teamapi.ListenerCarrierDefinition
	if err := client.Request(ctx, teamapi.AskCarrierTypeList, struct{}{}, &carrierTypes); err != nil {
		t.Fatal(err)
	}
	if len(carrierTypes) != 5 {
		t.Fatalf("listener carrier definitions = %#v", carrierTypes)
	}
	var current teamapi.Listener
	for _, item := range listeners {
		if item.Name == "integration" {
			current = item
			break
		}
	}
	var updated teamapi.Listener
	if err := client.Request(ctx, teamapi.AskListenerUpdate, teamapi.ListenerUpdateRequest{
		Name: "integration", ExpectedConfigVersion: current.ConfigVersion,
		Options: json.RawMessage(`{"bind":{"host":"127.0.0.1","port":"0"},"response_headers":{"X-Milestone":"six"}}`),
	}, &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ConfigVersion <= current.ConfigVersion {
		t.Fatalf("listener update did not advance version: before=%d after=%d", current.ConfigVersion, updated.ConfigVersion)
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
	var started teamapi.Listener
	if err := client.Request(ctx, teamapi.AskListenerStart, teamapi.NameRequest{Name: "integration"}, &started); err != nil {
		t.Fatal(err)
	}
	if !started.Running {
		t.Fatalf("started listener = %#v", started)
	}
	callbackResponse, err := http.Post("http://"+started.Address+"/", "application/octet-stream", strings.NewReader("not-base64"))
	if err != nil {
		t.Fatal(err)
	}
	_ = callbackResponse.Body.Close()
	if callbackResponse.StatusCode != http.StatusBadRequest || callbackResponse.Header.Get("X-Milestone") != "six" {
		t.Fatalf("configured listener response = %d %#v", callbackResponse.StatusCode, callbackResponse.Header)
	}
	var deleted teamapi.NameRequest
	if err := client.Request(ctx, teamapi.AskListenerDelete, teamapi.NameRequest{Name: "integration"}, &deleted); err != nil {
		t.Fatal(err)
	}
	if deleted.Name != "integration" {
		t.Fatalf("listener delete reply = %#v", deleted)
	}
	if _, err := db.DBListenerConfigGet("integration"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted listener remains persisted: %v", err)
	}
	if response, err := http.Post("http://"+started.Address+"/", "application/octet-stream", strings.NewReader("not-base64")); err == nil {
		_ = response.Body.Close()
		t.Fatal("deleted listener still accepts HTTP connections")
	}
	for {
		select {
		case event := <-replayClient.Events():
			if event.Type != teamapi.EventListenerDeleted {
				continue
			}
			var eventData teamapi.NameRequest
			if err := teamapi.DecodeData(event, &eventData); err != nil {
				t.Fatal(err)
			}
			if eventData.Name != "integration" {
				t.Fatalf("listener deleted event = %#v", eventData)
			}
			return
		case <-ctx.Done():
			t.Fatal("listener deleted event was not delivered")
		}
	}
}

func TestListenerControlContractAllOperationsAndEvents(t *testing.T) {
	setupTeamserverDatabase(t, "listener-contract.db")
	const token = "listener-contract-token-that-is-long-enough"
	instance := teamserver.New(config.Config{Token: token, ScriptDir: t.TempDir()}, events.New())
	cleanupTeamserver(t, instance)
	httpServer := httptest.NewServer(instance.Handler())
	defer httpServer.Close()

	client := clientapi.New(clientapi.Config{URL: httpServer.URL, Token: token, Timeout: 5 * time.Second})
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatal(err)
	}

	var driverTypes []teamapi.ListenerDriverDefinition
	if err := client.Request(ctx, teamapi.AskListenerTypeList, struct{}{}, &driverTypes); err != nil {
		t.Fatal(err)
	}
	if len(driverTypes) != 1 || driverTypes[0].ID != "http" || len(driverTypes[0].Options) == 0 {
		t.Fatalf("listener types = %#v", driverTypes)
	}
	var httpType teamapi.ListenerDriverDefinition
	if err := client.Request(ctx, teamapi.AskListenerTypeGet, teamapi.NameRequest{Name: " HTTP "}, &httpType); err != nil {
		t.Fatal(err)
	}
	if httpType.ID != "http" || len(httpType.Capabilities) == 0 || len(httpType.Options) == 0 {
		t.Fatalf("HTTP listener type = %#v", httpType)
	}
	var carrierTypes []teamapi.ListenerCarrierDefinition
	if err := client.Request(ctx, teamapi.AskCarrierTypeList, struct{}{}, &carrierTypes); err != nil {
		t.Fatal(err)
	}
	carrierIDs := make([]string, len(carrierTypes))
	for index, carrier := range carrierTypes {
		carrierIDs[index] = carrier.ID
	}
	if got, want := strings.Join(carrierIDs, ","), "body,cookie,header,image,query"; got != want {
		t.Fatalf("carrier IDs = %q, want %q", got, want)
	}

	var listed []teamapi.Listener
	if err := client.Request(ctx, teamapi.AskListenerList, struct{}{}, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("initial listener list = %#v", listed)
	}

	var created teamapi.Listener
	if err := client.Request(ctx, teamapi.AskListenerCreate, teamapi.ListenerCreateRequest{
		Name: "contract", Options: json.RawMessage(`{"bind":{"host":"127.0.0.1","port":"0"}}`),
	}, &created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "contract" || created.UUID == "" || created.Driver != "http" ||
		created.State != "stopped" || created.DesiredState != "stopped" || created.Running ||
		!created.Persistent || created.ConfigVersion < 1 || created.Host != "127.0.0.1" || created.Port != "0" {
		t.Fatalf("created listener = %#v", created)
	}

	var got teamapi.Listener
	if err := client.Request(ctx, teamapi.AskListenerGet, teamapi.NameRequest{Name: "contract"}, &got); err != nil {
		t.Fatal(err)
	}
	if got.UUID != created.UUID || got.ConfigVersion != created.ConfigVersion {
		t.Fatalf("listener get = %#v, created = %#v", got, created)
	}

	var updated teamapi.Listener
	if err := client.Request(ctx, teamapi.AskListenerUpdate, teamapi.ListenerUpdateRequest{
		Name: "contract", ExpectedConfigVersion: created.ConfigVersion,
		Options: json.RawMessage(`{"bind":{"host":"127.0.0.1","port":"0"},"response_headers":{"X-Contract":"yes"}}`),
	}, &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ConfigVersion <= created.ConfigVersion || !strings.Contains(string(updated.Options), "X-Contract") {
		t.Fatalf("updated listener = %#v", updated)
	}
	var ignored teamapi.Listener
	err := client.Request(ctx, teamapi.AskListenerUpdate, teamapi.ListenerUpdateRequest{
		Name: "contract", ExpectedConfigVersion: created.ConfigVersion,
		Options: json.RawMessage(`{"bind":{"host":"127.0.0.1","port":"1"}}`),
	}, &ignored)
	var apiError *teamapi.APIError
	if !errors.As(err, &apiError) || apiError.Code != "request_failed" || !strings.Contains(apiError.Message, "configuration changed") {
		t.Fatalf("stale listener update error = %#v", err)
	}

	var started teamapi.Listener
	if err := client.Request(ctx, teamapi.AskListenerStart, teamapi.NameRequest{Name: "contract"}, &started); err != nil {
		t.Fatal(err)
	}
	if !started.Running || started.State != "running" || started.Address == "" {
		t.Fatalf("started listener = %#v", started)
	}
	var restarted teamapi.Listener
	if err := client.Request(ctx, teamapi.AskListenerRestart, teamapi.NameRequest{Name: "contract"}, &restarted); err != nil {
		t.Fatal(err)
	}
	if !restarted.Running || restarted.State != "running" || restarted.Address == "" {
		t.Fatalf("restarted listener = %#v", restarted)
	}
	var stopped teamapi.Listener
	if err := client.Request(ctx, teamapi.AskListenerStop, teamapi.NameRequest{Name: "contract"}, &stopped); err != nil {
		t.Fatal(err)
	}
	if stopped.Running || stopped.State != "stopped" || stopped.DesiredState != "stopped" || stopped.Address != "" {
		t.Fatalf("stopped listener = %#v", stopped)
	}
	if err := client.Request(ctx, teamapi.AskListenerStart, teamapi.NameRequest{Name: "contract"}, &started); err != nil {
		t.Fatal(err)
	}
	var deleted teamapi.NameRequest
	if err := client.Request(ctx, teamapi.AskListenerDelete, teamapi.NameRequest{Name: "contract"}, &deleted); err != nil {
		t.Fatal(err)
	}
	if deleted.Name != "contract" {
		t.Fatalf("deleted listener reply = %#v", deleted)
	}
	if _, err := db.DBListenerConfigGet("contract"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted listener remains persisted: %v", err)
	}
	if err := client.Request(ctx, teamapi.AskListenerGet, teamapi.NameRequest{Name: "contract"}, &got); err == nil {
		t.Fatal("deleted listener remained reachable through ask.listener.get")
	}

	missingDirectory := t.TempDir()
	failureOptions, err := json.Marshal(map[string]any{
		"bind": map[string]string{"host": "127.0.0.1", "port": "0"},
		"tls": map[string]any{
			"enabled":   true,
			"cert_file": filepath.Join(missingDirectory, "missing.crt"),
			"key_file":  filepath.Join(missingDirectory, "missing.key"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var failure teamapi.Listener
	if err := client.Request(ctx, teamapi.AskListenerCreate, teamapi.ListenerCreateRequest{
		Name: "failure", Options: failureOptions,
	}, &failure); err != nil {
		t.Fatal(err)
	}
	err = client.Request(ctx, teamapi.AskListenerStart, teamapi.NameRequest{Name: "failure"}, &failure)
	apiError = nil
	if !errors.As(err, &apiError) || apiError.Code != "request_failed" {
		t.Fatalf("failed listener start error = %#v", err)
	}
	if err := client.Request(ctx, teamapi.AskListenerGet, teamapi.NameRequest{Name: "failure"}, &failure); err != nil {
		t.Fatal(err)
	}
	if failure.State != "failed" || failure.LastError == "" || failure.DesiredState != "running" {
		t.Fatalf("failed listener = %#v", failure)
	}
	if err := client.Request(ctx, teamapi.AskListenerDelete, teamapi.NameRequest{Name: "failure"}, &deleted); err != nil {
		t.Fatal(err)
	}

	expected := map[string][]string{
		"contract": {
			"evt.listener.created:stopped", "evt.listener.updated:stopped",
			"evt.listener.starting:starting", "evt.listener.started:running",
			"evt.listener.stopping:stopping", "evt.listener.stopped:stopped",
			"evt.listener.starting:starting", "evt.listener.started:running",
			"evt.listener.stopping:stopping", "evt.listener.stopped:stopped",
			"evt.listener.starting:starting", "evt.listener.started:running",
			"evt.listener.stopping:stopping", "evt.listener.stopped:stopped",
			"evt.listener.deleted",
		},
		"failure": {
			"evt.listener.created:stopped", "evt.listener.starting:starting",
			"evt.listener.failed:failed", "evt.listener.deleted",
		},
	}
	observed := map[string][]string{"contract": {}, "failure": {}}
	for len(observed["contract"]) < len(expected["contract"]) || len(observed["failure"]) < len(expected["failure"]) {
		select {
		case event := <-client.Events():
			name, state, listenerEvent, decodeErr := decodeListenerContractEvent(event)
			if decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if !listenerEvent || (name != "contract" && name != "failure") {
				continue
			}
			observation := event.Type
			if state != "" {
				observation += ":" + state
			}
			observed[name] = append(observed[name], observation)
		case <-ctx.Done():
			t.Fatalf("timed out collecting listener events: %#v", observed)
		}
	}
	for name, wanted := range expected {
		if got := strings.Join(observed[name], ","); got != strings.Join(wanted, ",") {
			t.Fatalf("%s events = %q, want %q", name, got, strings.Join(wanted, ","))
		}
	}
}

func decodeListenerContractEvent(event teamapi.Envelope) (name, state string, listenerEvent bool, err error) {
	switch event.Type {
	case teamapi.EventListenerCreated, teamapi.EventListenerUpdated,
		teamapi.EventListenerStarting, teamapi.EventListenerStarted,
		teamapi.EventListenerStopping, teamapi.EventListenerStopped,
		teamapi.EventListenerFailed:
		var item teamapi.Listener
		if err := teamapi.DecodeData(event, &item); err != nil {
			return "", "", true, err
		}
		return item.Name, item.State, true, nil
	case teamapi.EventListenerDeleted:
		var item teamapi.NameRequest
		if err := teamapi.DecodeData(event, &item); err != nil {
			return "", "", true, err
		}
		return item.Name, "", true, nil
	default:
		return "", "", false, nil
	}
}

func TestControlAcceptsBrowserAuthenticationSubprotocol(t *testing.T) {
	setupTeamserverDatabase(t, "browser-auth.db")
	const token = "browser-token-that-is-long-enough"
	instance := teamserver.New(config.Config{Token: token, ScriptDir: t.TempDir()}, events.New())
	cleanupTeamserver(t, instance)
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

func TestDownloadsSupportBrowserPreflightAndKeepAuthentication(t *testing.T) {
	setupTeamserverDatabase(t, "browser-downloads.db")
	const token = "browser-download-token-that-is-long-enough"
	instance := teamserver.New(config.Config{Token: token, ScriptDir: t.TempDir()}, events.New())
	cleanupTeamserver(t, instance)
	httpServer := httptest.NewServer(instance.Handler())
	defer httpServer.Close()

	tests := []struct {
		name string
		path string
	}{
		{name: "loot", path: "/api/v1/loot/test/content"},
		{name: "build", path: "/api/v1/builds/test/artifact"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			preflight, err := http.NewRequest(http.MethodOptions, httpServer.URL+test.path, nil)
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

			request, err := http.NewRequest(http.MethodGet, httpServer.URL+test.path, nil)
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
		})
	}
}

func TestBuildListingSnapshotAndDeletion(t *testing.T) {
	db.DatabasePath = t.TempDir() + "/builds.db"
	if err := db.CheckDB(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DBMS.DBConn.Close() })
	if err := db.EnsureTeamserverSchema(); err != nil {
		t.Fatal(err)
	}

	buildDirectory := t.TempDir()
	job := teamapi.Build{
		ID: uuid.NewString(), Profile: "linux", Status: "completed", ArtifactName: "agent",
		CreatedAt: time.Now().UTC(), CompletedAt: time.Now().UTC(),
	}
	if err := db.DBBuildSave(job); err != nil {
		t.Fatal(err)
	}
	artifactPath := filepath.Join(buildDirectory, job.ID)
	if err := os.WriteFile(artifactPath, []byte("artifact"), 0600); err != nil {
		t.Fatal(err)
	}

	const token = "build-management-token-that-is-long-enough"
	bus := events.New()
	records, unsubscribe := bus.Subscribe(32)
	defer unsubscribe()
	instance := teamserver.New(config.Config{Token: token, BuildDir: buildDirectory, ScriptDir: t.TempDir()}, bus)
	cleanupTeamserver(t, instance)
	httpServer := httptest.NewServer(instance.Handler())
	defer httpServer.Close()
	client := clientapi.New(clientapi.Config{URL: httpServer.URL, Token: token, Timeout: 5 * time.Second})
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const builderSource = "payload-builder-list-test.lua"
	implantbuilder.UnregisterPayloadBuilders(builderSource)
	if err := implantbuilder.RegisterPayloadBuilder(
		"linux-test-builder", "test payload builder", builderSource,
		func(string, implantbuilder.Profile) error { return nil },
	); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { implantbuilder.UnregisterPayloadBuilders(builderSource) })
	builders, err := client.PayloadBuilders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(builders) != 1 || builders[0].Name != "linux-test-builder" ||
		builders[0].Description != "test payload builder" || builders[0].Source != builderSource {
		t.Fatalf("payload builder list = %#v", builders)
	}

	builds, err := client.Builds(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(builds) != 1 || builds[0].ID != job.ID || builds[0].DownloadURL == "" {
		t.Fatalf("build list = %#v", builds)
	}
	snapshot, err := client.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Builds) != 1 || snapshot.Builds[0].ID != job.ID {
		t.Fatalf("snapshot builds = %#v", snapshot.Builds)
	}
	deleted, err := client.DeleteBuild(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.ID != job.ID {
		t.Fatalf("deleted build = %#v", deleted)
	}
	if _, err := os.Stat(artifactPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted artifact stat error = %v", err)
	}
	builds, err = client.Builds(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(builds) != 0 {
		t.Fatalf("build list after deletion = %#v", builds)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	waitForServerEvent(t, records, teamapi.EventUserLogout)
	httpServer.Close()
}

func TestBuildCreateAcceptsAndRoutesBuilderOverride(t *testing.T) {
	previousDatabasePath := db.DatabasePath
	previousDatabase := db.DBMS
	db.DatabasePath = filepath.Join(t.TempDir(), "builder-override.db")
	if err := db.CheckDB(); err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureTeamserverSchema(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.DBMS.DBConn.Close()
		db.DBMS = previousDatabase
		db.DatabasePath = previousDatabasePath
	})

	previousProfiles := implantbuilder.ProfileMap
	previousCurrentName := implantbuilder.CurrentName
	implantbuilder.ProfileMap = make(map[string]*implantbuilder.Profile)
	implantbuilder.CurrentName = ""
	t.Cleanup(func() {
		implantbuilder.ProfileMap = previousProfiles
		implantbuilder.CurrentName = previousCurrentName
	})

	const builderName = "request-override-builder"
	const builderSource = "request-override-builder.lua"
	implantbuilder.UnregisterPayloadBuilders(builderSource)
	if err := implantbuilder.RegisterPayloadBuilder(
		builderName, "request override test", builderSource,
		func(_ string, profile implantbuilder.Profile) error {
			if profile.Builder != builderName {
				return errors.New("builder override was not applied")
			}
			return os.WriteFile(profile.Output, []byte("override artifact"), 0600)
		},
	); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { implantbuilder.UnregisterPayloadBuilders(builderSource) })

	output := filepath.Join(t.TempDir(), "implant")
	implantbuilder.ProfileMap["linux-impl"] = &implantbuilder.Profile{
		Type: "impl", LHOST: "127.0.0.1:4444", OS: "linux", ARCH: "amd64",
		Template: t.TempDir(), Output: output,
	}

	const token = "build-override-token-that-is-long-enough"
	bus := events.New()
	records, unsubscribe := bus.Subscribe(32)
	defer unsubscribe()
	instance := teamserver.New(config.Config{Token: token, BuildDir: t.TempDir(), ScriptDir: t.TempDir()}, bus)
	cleanupTeamserver(t, instance)
	httpServer := httptest.NewServer(instance.Handler())
	defer httpServer.Close()
	client := clientapi.New(clientapi.Config{URL: httpServer.URL, Token: token, Timeout: 5 * time.Second})
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	queued, err := client.CreateBuild(ctx, "linux-impl", builderName)
	if err != nil {
		t.Fatal(err)
	}
	if queued.Profile != "linux-impl" || queued.Builder != builderName || queued.Status != "queued" {
		t.Fatalf("queued override build = %#v", queued)
	}

	var completed teamapi.Build
	for {
		completed, err = client.GetBuild(ctx, queued.ID)
		if err != nil {
			t.Fatal(err)
		}
		if completed.Status == "completed" || completed.Status == "failed" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if completed.Status != "completed" || completed.Builder != builderName || completed.DownloadURL == "" {
		t.Fatalf("completed override build = %#v", completed)
	}
	profile, err := implantbuilder.APIGetProfile("linux-impl")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Builder != "" {
		t.Fatalf("request override mutated profile builder to %q", profile.Builder)
	}
	if _, err := client.CreateBuild(ctx, "linux-impl", "missing-builder"); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("missing builder error = %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	waitForServerEvent(t, records, teamapi.EventUserLogout)
	httpServer.Close()
}

func waitForServerEvent(t *testing.T, records <-chan teamapi.EventRecord, wanted string) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case record := <-records:
			if record.Type == wanted {
				return
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %s", wanted)
		}
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
	cleanupTeamserver(t, instance)
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

	var spoofed teamapi.UserMessage
	if err := userClient.Request(ctx, teamapi.AskUserMessage, map[string]string{
		"user": "admin", "message": "spoofed",
	}, &spoofed); err == nil {
		t.Fatal("user message accepted a caller-selected sender")
	}
	broadcast, err := userClient.SendUserMessage(ctx, " hello team ")
	if err != nil {
		t.Fatal(err)
	}
	if broadcast.User != "alice" || broadcast.Message != "hello team" {
		t.Fatalf("message reply = %#v", broadcast)
	}
	assertUserMessageEvent(t, ctx, userClient.Events(), "alice", "hello team")
	assertUserMessageEvent(t, ctx, admin.Events(), "alice", "hello team")

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
	err = admin.Request(ctx, teamapi.AskUserUpdate, map[string]string{
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

func assertUserMessageEvent(t *testing.T, ctx context.Context, channel <-chan teamapi.Envelope, user, text string) {
	t.Helper()
	for {
		select {
		case event, ok := <-channel:
			if !ok {
				t.Fatal("event channel closed before user message")
			}
			if event.Type != teamapi.EventUserMessage {
				continue
			}
			var message teamapi.UserMessage
			if err := teamapi.DecodeData(event, &message); err != nil {
				t.Fatalf("decode user message: %v", err)
			}
			if message.User != user || message.Message != text {
				t.Fatalf("user message = %#v", message)
			}
			return
		case <-ctx.Done():
			t.Fatalf("timed out waiting for message from %s", user)
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
