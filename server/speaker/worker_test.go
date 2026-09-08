package speaker

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"purpcmd/implant/core"
	"purpcmd/internal"
	"purpcmd/internal/encrypt"
	"purpcmd/internal/protocol"
	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"
	serverimplant "purpcmd/server/implant"
)

func TestWorkerFirstBloodTaskDeliveryAndDisabledHealthcheck(t *testing.T) {
	bind := newTestBindServer(t)
	var registrations, healthchecks, tasks atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Header.Get(protocol.ExchangeHeader) {
		case protocol.ExchangeRegistration:
			registrations.Add(1)
		case protocol.ExchangeHealthcheck:
			healthchecks.Add(1)
		case protocol.ExchangeTask:
			tasks.Add(1)
		}
		bind.ServeHTTP(writer, request)
	}))
	defer remote.Close()

	disabled := false
	configuration := teamapi.SpeakerConfig{
		Client: teamapi.SpeakerHTTPClientConfig{BaseURL: remote.URL},
		Request: teamapi.SpeakerHTTPRequestConfig{
			Method: http.MethodPost,
		},
		Healthcheck: &teamapi.SpeakerHealthcheckConfig{Enabled: &disabled, Interval: 5 * time.Millisecond},
	}
	worker, err := StartWorker(context.Background(), "bind-http", "speaker-uuid", configuration, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	if registrations.Load() != 1 {
		t.Fatalf("registration exchanges = %d", registrations.Load())
	}
	if healthchecks.Load() != 0 {
		t.Fatalf("healthchecks before a task = %d", healthchecks.Load())
	}

	sessionName := worker.Session()
	session, err := serverimplant.APIGetSession(sessionName)
	if err != nil {
		t.Fatal(err)
	}
	if session.Transport != teamapi.SessionTransportSpeaker || session.Speaker != "bind-http" || session.SpeakerUUID != "speaker-uuid" {
		t.Fatalf("session route = %#v", session)
	}

	first, err := serverimplant.APICreateTask(teamapi.TaskCreateRequest{Session: sessionName, Code: internal.PING, Payload: []byte("one")})
	if err != nil {
		t.Fatal(err)
	}
	second, err := serverimplant.APICreateTask(teamapi.TaskCreateRequest{Session: sessionName, Code: internal.PING, Payload: []byte("two")})
	if err != nil {
		t.Fatal(err)
	}
	waitForTaskResponse(t, sessionName, first.ID, "one pong")
	waitForTaskResponse(t, sessionName, second.ID, "two pong")
	if tasks.Load() != 2 {
		t.Fatalf("task exchanges = %d", tasks.Load())
	}
	if healthchecks.Load() != 2 {
		t.Fatalf("task-triggered healthchecks = %d", healthchecks.Load())
	}
	time.Sleep(40 * time.Millisecond)
	if healthchecks.Load() != 2 {
		t.Fatalf("disabled background healthcheck made %d requests", healthchecks.Load())
	}

	worker.Stop()
	restarted, err := StartWorker(context.Background(), "bind-http", "speaker-uuid", configuration, nil)
	if err != nil {
		t.Fatalf("same speaker could not repeat first blood: %v", err)
	}
	restarted.Stop()
	if restarted.Session() != sessionName {
		t.Fatalf("restart session = %q, want %q", restarted.Session(), sessionName)
	}
	if _, err := StartWorker(context.Background(), "bind-http-other", "different-uuid", configuration, nil); err == nil {
		t.Fatal("different speaker took ownership of an active session")
	}
}

func TestWorkerPerformsConfiguredBackgroundHealthchecks(t *testing.T) {
	bind := newTestBindServer(t)
	var healthchecks atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get(protocol.ExchangeHeader) == protocol.ExchangeHealthcheck {
			healthchecks.Add(1)
		}
		bind.ServeHTTP(writer, request)
	}))
	defer remote.Close()

	enabled := true
	configuration := teamapi.SpeakerConfig{
		Client:  teamapi.SpeakerHTTPClientConfig{BaseURL: remote.URL},
		Request: teamapi.SpeakerHTTPRequestConfig{Method: http.MethodPost},
		Healthcheck: &teamapi.SpeakerHealthcheckConfig{
			Enabled: &enabled, Interval: 10 * time.Millisecond,
		},
	}
	worker, err := StartWorker(context.Background(), "periodic-bind", "periodic-uuid", configuration, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	deadline := time.Now().Add(time.Second)
	for healthchecks.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if healthchecks.Load() < 2 {
		t.Fatalf("background healthchecks = %d", healthchecks.Load())
	}
}

func TestWorkerMarksMonitoredSessionUnavailableAfterThreshold(t *testing.T) {
	bind := newTestBindServer(t)
	remote := httptest.NewServer(http.HandlerFunc(bind.ServeHTTP))
	enabled := true
	configuration := teamapi.SpeakerConfig{
		Client:  teamapi.SpeakerHTTPClientConfig{BaseURL: remote.URL},
		Request: teamapi.SpeakerHTTPRequestConfig{Method: http.MethodPost},
		Healthcheck: &teamapi.SpeakerHealthcheckConfig{
			Enabled: &enabled, Interval: 5 * time.Millisecond, FailureThreshold: 1,
		},
	}
	worker, err := StartWorker(context.Background(), "failing-bind", "failing-uuid", configuration, nil)
	if err != nil {
		remote.Close()
		t.Fatal(err)
	}
	defer worker.Stop()
	remote.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		session, getErr := serverimplant.APIGetSession(worker.Session())
		if getErr == nil && session.Liveness == "unavailable" && !session.Alive {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("session did not become unavailable after the configured failure threshold")
}

func TestWorkerRetriesDroppedTaskResponseWithSameTaskID(t *testing.T) {
	bind := newTestBindServer(t)
	var taskRequests atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get(protocol.ExchangeHeader) == protocol.ExchangeTask && taskRequests.Add(1) == 1 {
			recorder := httptest.NewRecorder()
			bind.ServeHTTP(recorder, request)
			http.Error(writer, "simulated dropped response", http.StatusBadGateway)
			return
		}
		bind.ServeHTTP(writer, request)
	}))
	defer remote.Close()
	disabled := false
	worker, err := StartWorker(context.Background(), "retry-bind", "retry-uuid", teamapi.SpeakerConfig{
		Client:      teamapi.SpeakerHTTPClientConfig{BaseURL: remote.URL},
		Request:     teamapi.SpeakerHTTPRequestConfig{Method: http.MethodPost},
		Healthcheck: &teamapi.SpeakerHealthcheckConfig{Enabled: &disabled},
		Retry:       teamapi.SpeakerRetryConfig{Interval: minimumConfiguredRetry},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	task, err := serverimplant.APICreateTask(teamapi.TaskCreateRequest{Session: worker.Session(), Code: internal.PING, Payload: []byte("retry")})
	if err != nil {
		t.Fatal(err)
	}
	waitForTaskResponse(t, worker.Session(), task.ID, "retry pong")
	if taskRequests.Load() != 2 {
		t.Fatalf("task attempts = %d, want 2", taskRequests.Load())
	}
}

func TestWorkerConsumesProfileOneTimeSecretOnFirstBlood(t *testing.T) {
	previousPath, previousDatabase := db.DatabasePath, db.DBMS
	db.DatabasePath = t.TempDir() + "/speaker-ots.db"
	if err := db.CheckDB(); err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureTeamserverSchema(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.DBMS.DBConn.Close()
		db.DatabasePath = previousPath
		db.DBMS = previousDatabase
	})
	if err := db.DBImplantProfileInsert(db.ImplantProfile{
		Name: "speaker-ots", Type: "bind.impl", Mode: "bind", OS: "linux", ARCH: "amd64",
		Output: "implant", Template: "./template", PublicKey: "server.pub",
	}); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("speaker registration secret"))
	if err := db.DBImplantDefinitionUpsert(db.ImplantDefinition{
		Name: "speaker-ots", Protocol: "http", PayloadType: "bind.impl", OptionsJSON: `{}`, OTSHash: digest[:],
	}); err != nil {
		t.Fatal(err)
	}
	if err := core.SetOneTimeSecret(digest[:12]); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.SetOneTimeSecret(make([]byte, 12)) })
	bind := newTestBindServer(t, "bind.impl")
	remote := httptest.NewServer(http.HandlerFunc(bind.ServeHTTP))
	defer remote.Close()
	disabled := false
	worker, err := StartWorker(context.Background(), "ots-bind", "ots-uuid", teamapi.SpeakerConfig{
		Profile:     "speaker-ots",
		Client:      teamapi.SpeakerHTTPClientConfig{BaseURL: remote.URL},
		Request:     teamapi.SpeakerHTTPRequestConfig{Method: http.MethodPost},
		Healthcheck: &teamapi.SpeakerHealthcheckConfig{Enabled: &disabled},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	worker.Stop()
	definition, err := db.DBImplantDefinitionGet("speaker-ots")
	if err != nil || definition.OTSUsedAt == nil {
		t.Fatalf("OTS was not consumed: %#v, %v", definition, err)
	}
	restarted, err := StartWorker(context.Background(), "ots-bind", "ots-uuid", teamapi.SpeakerConfig{
		Profile:     "speaker-ots",
		Client:      teamapi.SpeakerHTTPClientConfig{BaseURL: remote.URL},
		Request:     teamapi.SpeakerHTTPRequestConfig{Method: http.MethodPost},
		Healthcheck: &teamapi.SpeakerHealthcheckConfig{Enabled: &disabled},
	}, nil)
	if err != nil {
		t.Fatalf("stable speaker re-registration consumed OTS twice: %v", err)
	}
	restarted.Stop()
}

func newTestBindServer(t *testing.T, payloadTypes ...string) *core.BindServer {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if err := encrypt.LoadServerRSAKeyBytes(x509.MarshalPKCS1PrivateKey(privateKey)); err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := core.SetPublicKeyDER(publicDER); err != nil {
		t.Fatal(err)
	}
	configuration := core.BindConfig{}
	if len(payloadTypes) > 0 {
		configuration.PayloadType = payloadTypes[0]
	}
	bind, err := core.NewBindServer(configuration)
	if err != nil {
		t.Fatal(err)
	}
	return bind
}

func waitForTaskResponse(t *testing.T, session, id, expected string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := serverimplant.APIGetTask(session, id)
		if err == nil && task.Status == "completed" {
			if string(task.Response) != expected {
				t.Fatalf("task %s response = %q", id, task.Response)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("task %s did not complete", id)
}
