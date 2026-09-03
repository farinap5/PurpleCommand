package listener

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"purpcmd/pkg/teamapi"
	serverimplant "purpcmd/server/implant"

	"github.com/google/uuid"
)

func NewHTTPManager(store ListenerStore, publish ListenerEventPublisher) (*Manager, error) {
	registry := NewRegistry()
	if err := RegisterHTTPBuiltins(registry); err != nil {
		return nil, err
	}
	return NewManagerWithStore(registry, CallbackExchangeHandler, publish, store), nil
}

func TeamEventPublisher(publish func(string, any)) ListenerEventPublisher {
	return func(event ListenerEvent) {
		if publish == nil {
			return
		}
		eventTypes := map[string]string{
			"created": teamapi.EventListenerCreated, "updated": teamapi.EventListenerUpdated,
			"starting": teamapi.EventListenerStarting, "started": teamapi.EventListenerStarted,
			"stopping": teamapi.EventListenerStopping, "stopped": teamapi.EventListenerStopped,
			"failed": teamapi.EventListenerFailed, "deleted": teamapi.EventListenerDeleted,
		}
		if eventType := eventTypes[event.Type]; eventType != "" {
			if event.Type == "deleted" {
				publish(eventType, map[string]string{"name": event.Listener.Config.Name})
				return
			}
			publish(eventType, ListenerSnapshotToAPI(event.Listener))
		}
	}
}

func ListenerSnapshotToAPI(snapshot ManagedListenerSnapshot) teamapi.Listener {
	host, port := listenerLegacyAddress(snapshot.Config.Options)
	return teamapi.Listener{
		Name: snapshot.Config.Name, UUID: snapshot.Config.UUID, Host: host, Port: port,
		Running: snapshot.Status.State == StateRunning, Persistent: snapshot.Config.Persistent,
		Associations: listenerAssociationCount(snapshot.Config.Name, snapshot.Config.UUID),
		Driver:       snapshot.Config.Driver, Options: append(json.RawMessage(nil), snapshot.Config.Options...),
		Routes: routesToAPI(snapshot.Config.Routes), State: string(snapshot.Status.State),
		DesiredState: string(snapshot.Config.DesiredState), Address: snapshot.Status.Address,
		LastError: snapshot.Status.LastError, ConfigVersion: snapshot.Config.ConfigVersion,
	}
}

func listenerAssociationCount(name, id string) int {
	count := 0
	for _, session := range serverimplant.APIListSessions() {
		if session.Transport != "" && session.Transport != teamapi.SessionTransportListener {
			continue
		}
		if session.ListenerUUID != "" {
			if id != "" && session.ListenerUUID == id {
				count++
			}
			continue
		}
		if session.Listener == name {
			count++
		}
	}
	return count
}

func ListenerCreateFromAPI(request teamapi.ListenerCreateRequest) (ManagedListenerConfig, error) {
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return ManagedListenerConfig{}, errors.New("listener name is required")
	}
	driver := normalizeRegistryID(request.Driver)
	if driver == "" {
		driver = "http"
	}
	options, err := listenerOptionsFromAPI(request.Options, request.Host, request.Port)
	if err != nil {
		return ManagedListenerConfig{}, err
	}
	persistent := true
	if request.Persistent != nil {
		persistent = *request.Persistent
	}
	return ManagedListenerConfig{
		Name: name, UUID: uuid.NewString(), Driver: driver, Options: options,
		Routes: routesFromAPI(request.Routes), Persistent: persistent,
		DesiredState: StateStopped, ConfigVersion: 1,
	}, nil
}

func ListenerUpdateFromAPI(current ManagedListenerSnapshot, request teamapi.ListenerUpdateRequest) (ManagedListenerConfig, error) {
	configuration := cloneManagedListenerConfig(current.Config)
	if request.Driver != "" {
		configuration.Driver = request.Driver
	}
	if len(request.Options) > 0 {
		options, err := listenerOptionsFromAPI(request.Options, "", "")
		if err != nil {
			return ManagedListenerConfig{}, err
		}
		configuration.Options = options
	}
	if request.Routes != nil {
		configuration.Routes = routesFromAPI(request.Routes)
	}
	if request.Persistent != nil {
		configuration.Persistent = *request.Persistent
	}
	if strings.TrimSpace(request.Key) != "" {
		if err := applyLegacyListenerUpdate(&configuration, request.Key, request.Value); err != nil {
			return ManagedListenerConfig{}, err
		}
	}
	return configuration, nil
}

func ListenerDriverDefinitionsToAPI(definitions []DriverDefinition) []teamapi.ListenerDriverDefinition {
	result := make([]teamapi.ListenerDriverDefinition, len(definitions))
	for index, definition := range definitions {
		result[index] = teamapi.ListenerDriverDefinition{
			ID: definition.ID, Description: definition.Description,
			Capabilities: append([]string(nil), definition.Capabilities...),
			Options:      optionDefinitionsToAPI(definition.Options),
		}
	}
	return result
}

func ListenerCarrierDefinitionsToAPI(definitions []CarrierDefinition) []teamapi.ListenerCarrierDefinition {
	result := make([]teamapi.ListenerCarrierDefinition, len(definitions))
	for index, definition := range definitions {
		result[index] = teamapi.ListenerCarrierDefinition{
			ID: definition.ID, Description: definition.Description,
			Options: optionDefinitionsToAPI(definition.Options),
		}
	}
	return result
}

func (manager *Manager) DriverDefinitions() []DriverDefinition {
	return manager.registry.DriverDefinitions()
}

func (manager *Manager) CarrierDefinitions() []CarrierDefinition {
	return manager.registry.CarrierDefinitions()
}

func listenerOptionsFromAPI(raw json.RawMessage, legacyHost, legacyPort string) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage(`{}`)
	}
	var options map[string]any
	if err := json.Unmarshal(raw, &options); err != nil || options == nil {
		return nil, errors.New("listener options must be a JSON object")
	}
	if legacyHost != "" || legacyPort != "" {
		bind, _ := options["bind"].(map[string]any)
		if bind == nil {
			bind = make(map[string]any)
		}
		if legacyHost != "" {
			bind["host"] = legacyHost
		}
		if legacyPort != "" {
			bind["port"] = legacyPort
		}
		options["bind"] = bind
	}
	encoded, err := json.Marshal(options)
	return json.RawMessage(encoded), err
}

func applyLegacyListenerUpdate(configuration *ManagedListenerConfig, key, value string) error {
	key = strings.ToLower(strings.TrimSpace(key))
	value = strings.TrimSpace(value)
	switch key {
	case "host", "port":
		var options map[string]any
		if err := json.Unmarshal(configuration.Options, &options); err != nil {
			return err
		}
		bind, _ := options["bind"].(map[string]any)
		if bind == nil {
			bind = make(map[string]any)
		}
		bind[key] = value
		options["bind"] = bind
		encoded, err := json.Marshal(options)
		if err != nil {
			return err
		}
		configuration.Options = encoded
	case "persist":
		persistent, err := parseBoolOption(value)
		if err != nil {
			return err
		}
		configuration.Persistent = persistent
	default:
		return fmt.Errorf("unknown listener option %q", key)
	}
	return nil
}

func listenerLegacyAddress(options json.RawMessage) (string, string) {
	resolved, err := resolveHTTPOptions(options)
	if err != nil {
		return "", ""
	}
	return resolved.Bind.Host, resolved.Bind.Port
}

func routesFromAPI(routes []teamapi.ListenerRoute) []Route {
	result := make([]Route, len(routes))
	for index, route := range routes {
		result[index] = Route{
			ID: route.ID, Purpose: route.Purpose, Priority: route.Priority,
			Match: append(json.RawMessage(nil), route.Match...), Options: append(json.RawMessage(nil), route.Options...),
			Inbound:  CarrierSpec{Type: route.Inbound.Type, Options: append(json.RawMessage(nil), route.Inbound.Options...)},
			Outbound: CarrierSpec{Type: route.Outbound.Type, Options: append(json.RawMessage(nil), route.Outbound.Options...)},
		}
	}
	return result
}

func routesToAPI(routes []Route) []teamapi.ListenerRoute {
	result := make([]teamapi.ListenerRoute, len(routes))
	for index, route := range routes {
		result[index] = teamapi.ListenerRoute{
			ID: route.ID, Purpose: route.Purpose, Priority: route.Priority,
			Match: append(json.RawMessage(nil), route.Match...), Options: append(json.RawMessage(nil), route.Options...),
			Inbound:  teamapi.ListenerCarrierSpec{Type: route.Inbound.Type, Options: append(json.RawMessage(nil), route.Inbound.Options...)},
			Outbound: teamapi.ListenerCarrierSpec{Type: route.Outbound.Type, Options: append(json.RawMessage(nil), route.Outbound.Options...)},
		}
	}
	return result
}

func optionDefinitionsToAPI(definitions []OptionDefinition) []teamapi.ListenerOptionDefinition {
	result := make([]teamapi.ListenerOptionDefinition, len(definitions))
	for index, definition := range definitions {
		result[index] = teamapi.ListenerOptionDefinition{
			Key: definition.Key, Type: string(definition.Type), Description: definition.Description,
			Required: definition.Required, Secret: definition.Secret,
			MutableWhileRunning: definition.MutableWhileRunning,
			Default:             append(json.RawMessage(nil), definition.Default...),
		}
	}
	return result
}
