package speaker

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"purpcmd/pkg/teamapi"
	serverimplant "purpcmd/server/implant"
)

func TestManagerLifecycleVersioningAndLiveness(t *testing.T) {
	bind := newTestBindServer(t)
	remote := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		bind.ServeHTTP(writer, request)
	}))
	defer remote.Close()

	events := make([]string, 0)
	manager := NewManager(nil, func(eventType string, _ any) { events = append(events, eventType) })
	defer manager.Shutdown()
	disabled := false
	persistent := false
	created, err := manager.Create(teamapi.SpeakerCreateRequest{
		Name: "managed-bind", Persistent: &persistent,
		Config: teamapi.SpeakerConfig{
			Client:      teamapi.SpeakerHTTPClientConfig{BaseURL: remote.URL},
			Request:     teamapi.SpeakerHTTPRequestConfig{Method: http.MethodPost},
			Healthcheck: &teamapi.SpeakerHealthcheckConfig{Enabled: &disabled},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ConfigVersion != 1 || created.State != StateStopped || created.DesiredState != StateStopped {
		t.Fatalf("created speaker = %#v", created)
	}

	started, err := manager.Start(created.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !started.Running || started.State != string(WorkerConnected) || started.DesiredState != StateRunning || started.ConfigVersion != 2 || started.Session == "" {
		t.Fatalf("started speaker = %#v", started)
	}
	session, err := serverimplant.APIGetSession(started.Session)
	if err != nil {
		t.Fatal(err)
	}
	if session.Liveness != "unknown" || session.HealthMonitoring {
		t.Fatalf("health-disabled session liveness = %#v", session)
	}

	stopped, err := manager.Stop(created.Name)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Running || stopped.State != StateStopped || stopped.DesiredState != StateStopped || stopped.ConfigVersion != 3 {
		t.Fatalf("stopped speaker = %#v", stopped)
	}
	stale := uint64(2)
	if _, err := manager.Update(created.Name, teamapi.SpeakerUpdateRequest{ExpectedConfigVersion: &stale}); err != ErrVersionConflict {
		t.Fatalf("stale update error = %v", err)
	}
	expected := stopped.ConfigVersion
	updated, err := manager.Update(created.Name, teamapi.SpeakerUpdateRequest{Name: created.Name, NewName: "managed-bind-renamed", ExpectedConfigVersion: &expected})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "managed-bind-renamed" || updated.UUID != created.UUID || updated.ConfigVersion != 4 {
		t.Fatalf("updated speaker = %#v", updated)
	}
	renamedSession, err := serverimplant.APIGetSession(started.Session)
	if err != nil || renamedSession.Speaker != updated.Name || renamedSession.SpeakerUUID != updated.UUID {
		t.Fatalf("renamed session route = %#v, %v", renamedSession, err)
	}
	if _, err := manager.Get(created.Name); err != ErrNotFound {
		t.Fatalf("old name lookup error = %v", err)
	}
	if _, err := manager.Delete(updated.Name); err != nil {
		t.Fatal(err)
	}
	if len(manager.List()) != 0 {
		t.Fatal("deleted speaker remained in manager")
	}
	if len(events) == 0 || events[0] != teamapi.EventSpeakerCreated {
		t.Fatalf("events = %#v", events)
	}
}

func TestManagerRestoresDesiredRunningSpeakerWithStableIdentity(t *testing.T) {
	bind := newTestBindServer(t)
	remote := httptest.NewServer(http.HandlerFunc(bind.ServeHTTP))
	defer remote.Close()
	store := &memorySpeakerStore{}
	disabled := false

	firstManager := NewManager(store, nil)
	created, err := firstManager.Create(teamapi.SpeakerCreateRequest{
		Name: "restored-bind",
		Config: teamapi.SpeakerConfig{
			Client:      teamapi.SpeakerHTTPClientConfig{BaseURL: remote.URL},
			Request:     teamapi.SpeakerHTTPRequestConfig{Method: http.MethodPost},
			Healthcheck: &teamapi.SpeakerHealthcheckConfig{Enabled: &disabled},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	started, err := firstManager.Start(created.Name)
	if err != nil {
		t.Fatal(err)
	}
	firstManager.Shutdown()

	secondManager := NewManager(store, nil)
	defer secondManager.Shutdown()
	if err := secondManager.Restore(); err != nil {
		t.Fatal(err)
	}
	restored, err := secondManager.Get(created.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !restored.Running || restored.State != string(WorkerConnected) || restored.UUID != created.UUID || restored.Session != started.Session || restored.DesiredState != StateRunning {
		t.Fatalf("restored speaker = %#v", restored)
	}
}

type memorySpeakerStore struct {
	mu    sync.Mutex
	items map[string]teamapi.Speaker
}

func (store *memorySpeakerStore) Insert(item teamapi.Speaker) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.items == nil {
		store.items = make(map[string]teamapi.Speaker)
	}
	if _, exists := store.items[item.Name]; exists {
		return errors.New("exists")
	}
	store.items[item.Name] = cloneSpeaker(item)
	return nil
}

func (store *memorySpeakerStore) Update(previousName string, expectedVersion uint64, item teamapi.Speaker) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	current, exists := store.items[previousName]
	if !exists || current.ConfigVersion != expectedVersion {
		return ErrVersionConflict
	}
	delete(store.items, previousName)
	store.items[item.Name] = cloneSpeaker(item)
	return nil
}

func (store *memorySpeakerStore) Delete(name, _ string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.items, name)
	return nil
}

func (store *memorySpeakerStore) List() ([]teamapi.Speaker, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	result := make([]teamapi.Speaker, 0, len(store.items))
	for _, item := range store.items {
		result = append(result, cloneSpeaker(item))
	}
	return result, nil
}
