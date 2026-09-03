package listener

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Registry struct {
	mu       sync.RWMutex
	drivers  map[string]Driver
	carriers map[string]Carrier
}

func NewRegistry() *Registry {
	return &Registry{
		drivers:  make(map[string]Driver),
		carriers: make(map[string]Carrier),
	}
}

func (registry *Registry) RegisterDriver(driver Driver) error {
	if driver == nil {
		return errors.New("listener driver is required")
	}
	definition := driver.Definition()
	id := normalizeRegistryID(definition.ID)
	if id == "" {
		return errors.New("listener driver ID is required")
	}
	if id != definition.ID {
		return errors.New("listener driver ID must be normalized lowercase text")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.drivers[id]; exists {
		return fmt.Errorf("listener driver %q is already registered", id)
	}
	registry.drivers[id] = driver
	return nil
}

func (registry *Registry) Driver(id string) (Driver, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	driver, found := registry.drivers[normalizeRegistryID(id)]
	return driver, found
}

func (registry *Registry) DriverDefinitions() []DriverDefinition {
	registry.mu.RLock()
	definitions := make([]DriverDefinition, 0, len(registry.drivers))
	for _, driver := range registry.drivers {
		definitions = append(definitions, cloneDriverDefinition(driver.Definition()))
	}
	registry.mu.RUnlock()
	sort.Slice(definitions, func(left, right int) bool { return definitions[left].ID < definitions[right].ID })
	return definitions
}

func (registry *Registry) RegisterCarrier(carrier Carrier) error {
	if carrier == nil {
		return errors.New("listener carrier is required")
	}
	definition := carrier.Definition()
	id := normalizeRegistryID(definition.ID)
	if id == "" {
		return errors.New("listener carrier ID is required")
	}
	if id != definition.ID {
		return errors.New("listener carrier ID must be normalized lowercase text")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.carriers[id]; exists {
		return fmt.Errorf("listener carrier %q is already registered", id)
	}
	registry.carriers[id] = carrier
	return nil
}

func (registry *Registry) Carrier(id string) (Carrier, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	carrier, found := registry.carriers[normalizeRegistryID(id)]
	return carrier, found
}

func (registry *Registry) CarrierDefinitions() []CarrierDefinition {
	registry.mu.RLock()
	definitions := make([]CarrierDefinition, 0, len(registry.carriers))
	for _, carrier := range registry.carriers {
		definitions = append(definitions, cloneCarrierDefinition(carrier.Definition()))
	}
	registry.mu.RUnlock()
	sort.Slice(definitions, func(left, right int) bool { return definitions[left].ID < definitions[right].ID })
	return definitions
}

func normalizeRegistryID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func cloneDriverDefinition(source DriverDefinition) DriverDefinition {
	result := source
	result.Capabilities = append([]string(nil), source.Capabilities...)
	result.Options = cloneOptionDefinitions(source.Options)
	return result
}

func cloneCarrierDefinition(source CarrierDefinition) CarrierDefinition {
	result := source
	result.Options = cloneOptionDefinitions(source.Options)
	return result
}

func cloneOptionDefinitions(source []OptionDefinition) []OptionDefinition {
	result := make([]OptionDefinition, len(source))
	for index, option := range source {
		result[index] = option
		result[index].Default = append([]byte(nil), option.Default...)
	}
	return result
}

var DefaultRegistry = NewRegistry()
