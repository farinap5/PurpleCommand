package listener

import (
	"encoding/json"
	"testing"

	"purpcmd/pkg/teamapi"
)

func TestListenerCreateAndLegacyUpdateAPIConversion(t *testing.T) {
	persistent := false
	configuration, err := ListenerCreateFromAPI(teamapi.ListenerCreateRequest{
		Name: " api ", Host: "127.0.0.1", Port: "8080", Persistent: &persistent,
		Options: json.RawMessage(`{
			"response_headers":{"X-Test":"yes"},
			"hosted_files":{"/":{"source_path":"site/index.html"}},
			"not_found_page":{"source_path":"site/404.html"}
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Name != "api" || configuration.Driver != "http" || configuration.Persistent {
		t.Fatalf("configuration = %#v", configuration)
	}
	var options map[string]json.RawMessage
	if err := json.Unmarshal(configuration.Options, &options); err != nil {
		t.Fatal(err)
	}
	if len(options["hosted_files"]) == 0 || len(options["not_found_page"]) == 0 {
		t.Fatalf("hosted options were not preserved: %s", configuration.Options)
	}
	dto := ListenerSnapshotToAPI(ManagedListenerSnapshot{
		Config: configuration,
		Status: Status{State: StateStopped},
	})
	if dto.Host != "127.0.0.1" || dto.Port != "8080" || dto.Driver != "http" {
		t.Fatalf("listener DTO = %#v", dto)
	}
	updated, err := ListenerUpdateFromAPI(ManagedListenerSnapshot{Config: configuration}, teamapi.ListenerUpdateRequest{
		Name: "api", Key: "port", Value: "9090",
	})
	if err != nil {
		t.Fatal(err)
	}
	updatedDTO := ListenerSnapshotToAPI(ManagedListenerSnapshot{Config: updated, Status: Status{State: StateStopped}})
	if updatedDTO.Port != "9090" || updatedDTO.Host != "127.0.0.1" {
		t.Fatalf("legacy update DTO = %#v", updatedDTO)
	}
}

func TestTeamEventPublisherMapsEveryListenerEvent(t *testing.T) {
	expected := map[string]string{
		"created":  teamapi.EventListenerCreated,
		"updated":  teamapi.EventListenerUpdated,
		"starting": teamapi.EventListenerStarting,
		"started":  teamapi.EventListenerStarted,
		"stopping": teamapi.EventListenerStopping,
		"stopped":  teamapi.EventListenerStopped,
		"failed":   teamapi.EventListenerFailed,
		"deleted":  teamapi.EventListenerDeleted,
		"hosted":   teamapi.EventListenerHostedUpdated,
	}
	for managerType, expectedType := range expected {
		t.Run(managerType, func(t *testing.T) {
			var eventType string
			var value any
			publisher := TeamEventPublisher(func(gotType string, gotValue any) {
				eventType, value = gotType, gotValue
			})
			publisher(ListenerEvent{Type: managerType, Listener: ManagedListenerSnapshot{
				Config: ManagedListenerConfig{Name: "contract", UUID: "contract-id", Driver: "http", Options: json.RawMessage(`{}`)},
				Status: Status{State: StateStopped},
			}})
			if eventType != expectedType {
				t.Fatalf("event type = %q, want %q", eventType, expectedType)
			}
			if managerType == "hosted" {
				hosted, ok := value.(teamapi.ListenerHostedConfiguration)
				if !ok || hosted.Name != "contract" || hosted.ListenerUUID != "contract-id" || hosted.HostedFiles == nil {
					t.Fatalf("hosted event value = %#v", value)
				}
				return
			}
			if managerType == "deleted" {
				deleted, ok := value.(map[string]string)
				if !ok || deleted["name"] != "contract" || len(deleted) != 1 {
					t.Fatalf("delete event value = %#v", value)
				}
				return
			}
			listener, ok := value.(teamapi.Listener)
			if !ok || listener.Name != "contract" || listener.UUID != "contract-id" || listener.State != string(StateStopped) {
				t.Fatalf("listener event value = %#v", value)
			}
		})
	}
}
