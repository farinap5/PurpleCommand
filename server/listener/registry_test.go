package listener

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type registryTestDriver struct{ definition DriverDefinition }

func (driver registryTestDriver) Definition() DriverDefinition     { return driver.definition }
func (registryTestDriver) Validate(json.RawMessage, []Route) error { return nil }
func (registryTestDriver) Start(context.Context, RuntimeConfig, ExchangeHandler) (Runtime, error) {
	return nil, errors.New("not implemented")
}

type registryTestCarrier struct{ definition CarrierDefinition }

func (carrier registryTestCarrier) Definition() CarrierDefinition { return carrier.definition }
func (registryTestCarrier) Validate(json.RawMessage) error        { return nil }
func (registryTestCarrier) Decode(CarrierEnvelope, json.RawMessage) (CarriedMessage, error) {
	return CarriedMessage{}, nil
}
func (registryTestCarrier) Encode(CarriedMessage, json.RawMessage) (CarrierEnvelope, error) {
	return CarrierEnvelope{}, nil
}

func TestRegistryRejectsInvalidAndDuplicateRegistrations(t *testing.T) {
	registry := NewRegistry()
	if err := registry.RegisterDriver(nil); err == nil {
		t.Fatal("nil driver was accepted")
	}
	if err := registry.RegisterDriver(registryTestDriver{definition: DriverDefinition{ID: "HTTP"}}); err == nil {
		t.Fatal("non-normalized driver ID was accepted")
	}
	driver := registryTestDriver{definition: DriverDefinition{ID: "http"}}
	if err := registry.RegisterDriver(driver); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterDriver(driver); err == nil {
		t.Fatal("duplicate driver was accepted")
	}

	if err := registry.RegisterCarrier(nil); err == nil {
		t.Fatal("nil carrier was accepted")
	}
	carrier := registryTestCarrier{definition: CarrierDefinition{ID: "raw"}}
	if err := registry.RegisterCarrier(carrier); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterCarrier(carrier); err == nil {
		t.Fatal("duplicate carrier was accepted")
	}
}

func TestRegistryReturnsSortedDefensiveDefinitions(t *testing.T) {
	registry := NewRegistry()
	first := registryTestDriver{definition: DriverDefinition{
		ID: "zeta", Capabilities: []string{"routes"},
		Options: []OptionDefinition{{Key: "bind.host", Default: json.RawMessage(`"127.0.0.1"`)}},
	}}
	second := registryTestDriver{definition: DriverDefinition{ID: "alpha"}}
	if err := registry.RegisterDriver(first); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterDriver(second); err != nil {
		t.Fatal(err)
	}
	definitions := registry.DriverDefinitions()
	if len(definitions) != 2 || definitions[0].ID != "alpha" || definitions[1].ID != "zeta" {
		t.Fatalf("definitions = %#v", definitions)
	}
	definitions[1].Capabilities[0] = "changed"
	definitions[1].Options[0].Default[0] = 'x'
	again := registry.DriverDefinitions()
	if again[1].Capabilities[0] != "routes" || string(again[1].Options[0].Default) != `"127.0.0.1"` {
		t.Fatalf("registry definition was mutated: %#v", again[1])
	}
}
