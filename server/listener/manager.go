package listener

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrManagedListenerNotFound = errors.New("listener not found")
	ErrManagedListenerRunning  = errors.New("listener is already running")
	ErrManagedListenerStopped  = errors.New("listener is not running")
	ErrManagedListenerDeleted  = errors.New("listener was deleted")
	ErrListenerManagerShutdown = errors.New("listener manager is shut down")
)

const defaultRuntimeStopTimeout = 5 * time.Second

type ListenerEvent struct {
	Type     string
	Listener ManagedListenerSnapshot
}

type ListenerEventPublisher func(ListenerEvent)

type managedListener struct {
	operationMu sync.Mutex
	mu          sync.RWMutex
	config      ManagedListenerConfig
	status      Status
	runtime     Runtime
	cancel      context.CancelFunc
	generation  uint64
	deleted     bool
}

type Manager struct {
	registry    *Registry
	handler     ExchangeHandler
	publish     ListenerEventPublisher
	store       ListenerStore
	ctx         context.Context
	cancel      context.CancelFunc
	stopTimeout time.Duration

	mu        sync.RWMutex
	listeners map[string]*managedListener
	closed    bool
}

func NewManager(registry *Registry, handler ExchangeHandler, publish ListenerEventPublisher) *Manager {
	return NewManagerWithStore(registry, handler, publish, nil)
}

func NewManagerWithStore(registry *Registry, handler ExchangeHandler, publish ListenerEventPublisher, store ListenerStore) *Manager {
	if registry == nil {
		registry = DefaultRegistry
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		registry: registry, handler: handler, publish: publish, store: store,
		ctx: ctx, cancel: cancel, stopTimeout: defaultRuntimeStopTimeout,
		listeners: make(map[string]*managedListener),
	}
}

func (manager *Manager) Create(configuration ManagedListenerConfig) (ManagedListenerSnapshot, error) {
	configuration, driver, err := manager.validateConfiguration(configuration)
	if err != nil {
		return ManagedListenerSnapshot{}, err
	}
	if err := driver.Validate(configuration.Options, configuration.Routes); err != nil {
		return ManagedListenerSnapshot{}, err
	}
	instance := &managedListener{
		config: configuration,
		status: Status{State: StateStopped, ChangedAt: time.Now().UTC()},
	}
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return ManagedListenerSnapshot{}, ErrListenerManagerShutdown
	}
	if manager.listeners[configuration.Name] != nil {
		manager.mu.Unlock()
		return ManagedListenerSnapshot{}, errors.New("listener exists")
	}
	if configuration.Persistent && manager.store != nil {
		configuration, err = manager.store.Insert(configuration)
		if err != nil {
			manager.mu.Unlock()
			return ManagedListenerSnapshot{}, err
		}
		instance.config = configuration
	}
	manager.listeners[configuration.Name] = instance
	manager.mu.Unlock()
	snapshot := instance.snapshot()
	manager.emit("created", snapshot)
	return snapshot, nil
}

func (manager *Manager) Get(name string) (ManagedListenerSnapshot, error) {
	instance := manager.lookup(name)
	if instance == nil {
		return ManagedListenerSnapshot{}, ErrManagedListenerNotFound
	}
	return instance.snapshot(), nil
}

func (manager *Manager) List() []ManagedListenerSnapshot {
	manager.mu.RLock()
	instances := make([]*managedListener, 0, len(manager.listeners))
	for _, instance := range manager.listeners {
		instances = append(instances, instance)
	}
	manager.mu.RUnlock()
	result := make([]ManagedListenerSnapshot, 0, len(instances))
	for _, instance := range instances {
		result = append(result, instance.snapshot())
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].Config.Name < result[right].Config.Name
	})
	return result
}

func (manager *Manager) Start(name string) (ManagedListenerSnapshot, error) {
	instance := manager.lookup(name)
	if instance == nil {
		return ManagedListenerSnapshot{}, ErrManagedListenerNotFound
	}
	instance.operationMu.Lock()
	defer instance.operationMu.Unlock()
	return manager.startLocked(instance)
}

func (manager *Manager) startLocked(instance *managedListener) (ManagedListenerSnapshot, error) {
	manager.mu.RLock()
	closed := manager.closed
	manager.mu.RUnlock()
	if closed {
		return ManagedListenerSnapshot{}, ErrListenerManagerShutdown
	}
	instance.mu.Lock()
	if instance.deleted {
		instance.mu.Unlock()
		return ManagedListenerSnapshot{}, ErrManagedListenerDeleted
	}
	if instance.runtime != nil || instance.status.State == StateRunning || instance.status.State == StateStarting {
		instance.mu.Unlock()
		return ManagedListenerSnapshot{}, ErrManagedListenerRunning
	}
	current := cloneManagedListenerConfig(instance.config)
	instance.mu.Unlock()
	configuration, err := manager.persistTransition(current, func(candidate *ManagedListenerConfig) {
		candidate.DesiredState = StateRunning
	})
	if err != nil {
		return ManagedListenerSnapshot{}, err
	}
	instance.mu.Lock()
	instance.config = configuration
	instance.status = Status{State: StateStarting, ChangedAt: time.Now().UTC()}
	instance.generation++
	generation := instance.generation
	instance.mu.Unlock()
	manager.emit("starting", instance.snapshot())

	driver, found := manager.registry.Driver(configuration.Driver)
	if !found {
		err := fmt.Errorf("listener driver %q is not registered", configuration.Driver)
		manager.failStart(instance, err)
		return ManagedListenerSnapshot{}, err
	}
	if err := driver.Validate(configuration.Options, configuration.Routes); err != nil {
		manager.failStart(instance, err)
		return ManagedListenerSnapshot{}, err
	}
	startContext, cancel := context.WithCancel(manager.ctx)
	runtime, err := driver.Start(startContext, RuntimeConfig{
		Name: configuration.Name, UUID: configuration.UUID, Driver: configuration.Driver,
		Options: append(json.RawMessage(nil), configuration.Options...), Routes: cloneRoutes(configuration.Routes),
	}, manager.handler)
	if err != nil {
		cancel()
		manager.failStart(instance, err)
		return ManagedListenerSnapshot{}, err
	}
	if runtime == nil {
		cancel()
		err = errors.New("listener driver returned a nil runtime")
		manager.failStart(instance, err)
		return ManagedListenerSnapshot{}, err
	}

	instance.mu.Lock()
	instance.runtime = runtime
	instance.cancel = cancel
	instance.config.DesiredState = StateRunning
	instance.status = Status{State: StateRunning, Address: runtime.Address(), ChangedAt: time.Now().UTC()}
	instance.mu.Unlock()
	snapshot := instance.snapshot()
	manager.emit("started", snapshot)
	if done := runtime.Done(); done != nil {
		go manager.watch(instance, runtime, generation, done)
	}
	return snapshot, nil
}

func (manager *Manager) failStart(instance *managedListener, err error) {
	instance.mu.Lock()
	instance.runtime = nil
	instance.cancel = nil
	instance.status = Status{State: StateFailed, LastError: err.Error(), ChangedAt: time.Now().UTC()}
	instance.mu.Unlock()
	manager.emit("failed", instance.snapshot())
}

func (manager *Manager) Stop(name string) (ManagedListenerSnapshot, error) {
	instance := manager.lookup(name)
	if instance == nil {
		return ManagedListenerSnapshot{}, ErrManagedListenerNotFound
	}
	instance.operationMu.Lock()
	defer instance.operationMu.Unlock()
	ctx, cancel := manager.runtimeStopContext()
	defer cancel()
	return manager.stopLocked(ctx, instance, true)
}

func (manager *Manager) stopLocked(ctx context.Context, instance *managedListener, persistDesired bool) (ManagedListenerSnapshot, error) {
	instance.mu.Lock()
	if instance.deleted {
		instance.mu.Unlock()
		return ManagedListenerSnapshot{}, ErrManagedListenerDeleted
	}
	if instance.status.State == StateFailed && instance.runtime == nil {
		current := cloneManagedListenerConfig(instance.config)
		instance.mu.Unlock()
		configuration := current
		var err error
		if persistDesired {
			configuration, err = manager.persistTransition(current, func(candidate *ManagedListenerConfig) {
				candidate.DesiredState = StateStopped
			})
			if err != nil {
				return ManagedListenerSnapshot{}, err
			}
		}
		instance.mu.Lock()
		instance.config = configuration
		instance.status = Status{State: StateStopped, ChangedAt: time.Now().UTC()}
		instance.mu.Unlock()
		snapshot := instance.snapshot()
		manager.emit("stopped", snapshot)
		return snapshot, nil
	}
	if instance.runtime == nil {
		instance.mu.Unlock()
		return ManagedListenerSnapshot{}, ErrManagedListenerStopped
	}
	runtime := instance.runtime
	cancel := instance.cancel
	current := cloneManagedListenerConfig(instance.config)
	instance.mu.Unlock()
	configuration := current
	if persistDesired {
		var err error
		configuration, err = manager.persistTransition(current, func(candidate *ManagedListenerConfig) {
			candidate.DesiredState = StateStopped
		})
		if err != nil {
			return ManagedListenerSnapshot{}, err
		}
	}
	instance.mu.Lock()
	instance.config = configuration
	instance.status.State = StateStopping
	instance.status.ChangedAt = time.Now().UTC()
	instance.mu.Unlock()
	manager.emit("stopping", instance.snapshot())
	if cancel != nil {
		cancel()
	}
	if err := runtime.Stop(ctx); err != nil {
		instance.mu.Lock()
		instance.status = Status{State: StateFailed, Address: runtime.Address(), LastError: err.Error(), ChangedAt: time.Now().UTC()}
		instance.mu.Unlock()
		manager.emit("failed", instance.snapshot())
		return ManagedListenerSnapshot{}, err
	}
	instance.mu.Lock()
	instance.runtime = nil
	instance.cancel = nil
	instance.status = Status{State: StateStopped, ChangedAt: time.Now().UTC()}
	instance.mu.Unlock()
	snapshot := instance.snapshot()
	manager.emit("stopped", snapshot)
	return snapshot, nil
}

func (manager *Manager) Restart(name string) (ManagedListenerSnapshot, error) {
	instance := manager.lookup(name)
	if instance == nil {
		return ManagedListenerSnapshot{}, ErrManagedListenerNotFound
	}
	instance.operationMu.Lock()
	defer instance.operationMu.Unlock()
	instance.mu.RLock()
	hasRuntime := instance.runtime != nil
	instance.mu.RUnlock()
	if hasRuntime {
		ctx, cancel := manager.runtimeStopContext()
		defer cancel()
		if _, err := manager.stopLocked(ctx, instance, true); err != nil {
			return ManagedListenerSnapshot{}, err
		}
	}
	return manager.startLocked(instance)
}

func (manager *Manager) Delete(name string) (ManagedListenerSnapshot, error) {
	instance := manager.lookup(name)
	if instance == nil {
		return ManagedListenerSnapshot{}, ErrManagedListenerNotFound
	}
	instance.operationMu.Lock()
	defer instance.operationMu.Unlock()
	instance.mu.RLock()
	hasRuntime := instance.runtime != nil
	instance.mu.RUnlock()
	if hasRuntime {
		ctx, cancel := manager.runtimeStopContext()
		defer cancel()
		if _, err := manager.stopLocked(ctx, instance, true); err != nil {
			return ManagedListenerSnapshot{}, err
		}
	}
	instance.mu.Lock()
	instance.deleted = true
	snapshot := ManagedListenerSnapshot{Config: cloneManagedListenerConfig(instance.config), Status: instance.status}
	instance.mu.Unlock()
	if manager.store != nil {
		if err := manager.store.Delete(name); err != nil {
			instance.mu.Lock()
			instance.deleted = false
			instance.mu.Unlock()
			return ManagedListenerSnapshot{}, err
		}
	}
	manager.mu.Lock()
	if manager.listeners[name] == instance {
		delete(manager.listeners, name)
	}
	manager.mu.Unlock()
	manager.emit("deleted", snapshot)
	return snapshot, nil
}

func (manager *Manager) Shutdown(ctx context.Context) error {
	manager.mu.Lock()
	manager.closed = true
	instances := make([]*managedListener, 0, len(manager.listeners))
	for _, instance := range manager.listeners {
		instances = append(instances, instance)
	}
	manager.mu.Unlock()
	manager.cancel()
	var failures []error
	for _, instance := range instances {
		instance.operationMu.Lock()
		instance.mu.RLock()
		hasRuntime := instance.runtime != nil
		instance.mu.RUnlock()
		if hasRuntime {
			if _, err := manager.stopLocked(ctx, instance, false); err != nil {
				failures = append(failures, err)
			}
		}
		instance.operationMu.Unlock()
	}
	return errors.Join(failures...)
}

func (manager *Manager) watch(instance *managedListener, runtime Runtime, generation uint64, done <-chan error) {
	err, ok := <-done
	if !ok {
		err = nil
	}
	instance.operationMu.Lock()
	defer instance.operationMu.Unlock()
	instance.mu.Lock()
	if instance.runtime != runtime || instance.generation != generation {
		instance.mu.Unlock()
		return
	}
	previous := instance.status.State
	cancel := instance.cancel
	instance.runtime = nil
	instance.cancel = nil
	if previous == StateStopping {
		instance.status = Status{State: StateStopped, ChangedAt: time.Now().UTC()}
	} else if err != nil {
		instance.status = Status{State: StateFailed, LastError: err.Error(), ChangedAt: time.Now().UTC()}
	} else {
		current := cloneManagedListenerConfig(instance.config)
		instance.mu.Unlock()
		configuration, persistErr := manager.persistTransition(current, func(candidate *ManagedListenerConfig) {
			candidate.DesiredState = StateStopped
		})
		instance.mu.Lock()
		if persistErr != nil {
			instance.status = Status{State: StateFailed, LastError: persistErr.Error(), ChangedAt: time.Now().UTC()}
		} else {
			instance.config = configuration
			instance.status = Status{State: StateStopped, ChangedAt: time.Now().UTC()}
		}
	}
	snapshot := ManagedListenerSnapshot{Config: cloneManagedListenerConfig(instance.config), Status: instance.status}
	instance.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if snapshot.Status.State == StateFailed {
		manager.emit("failed", snapshot)
	} else if previous != StateStopping {
		manager.emit("stopped", snapshot)
	}
}

// Update atomically replaces driver-owned configuration. Runtime settings may
// only be changed while the listener is stopped or failed.
func (manager *Manager) Update(configuration ManagedListenerConfig, expectedVersion int) (ManagedListenerSnapshot, error) {
	instance := manager.lookup(configuration.Name)
	if instance == nil {
		return ManagedListenerSnapshot{}, ErrManagedListenerNotFound
	}
	instance.operationMu.Lock()
	defer instance.operationMu.Unlock()
	instance.mu.RLock()
	current := cloneManagedListenerConfig(instance.config)
	state := instance.status.State
	hasRuntime := instance.runtime != nil
	deleted := instance.deleted
	instance.mu.RUnlock()
	if deleted {
		return ManagedListenerSnapshot{}, ErrManagedListenerDeleted
	}
	if hasRuntime || state == StateRunning || state == StateStarting || state == StateStopping {
		return ManagedListenerSnapshot{}, errors.New("stop listener before changing its configuration")
	}
	if expectedVersion > 0 && expectedVersion != current.ConfigVersion {
		return ManagedListenerSnapshot{}, errors.New("listener configuration changed")
	}
	configuration.Name = current.Name
	configuration.UUID = current.UUID
	configuration.DesiredState = current.DesiredState
	configuration.ConfigVersion = current.ConfigVersion
	configuration, driver, err := manager.validateConfiguration(configuration)
	if err != nil {
		return ManagedListenerSnapshot{}, err
	}
	if err := driver.Validate(configuration.Options, configuration.Routes); err != nil {
		return ManagedListenerSnapshot{}, err
	}
	saved, err := manager.persistReplacement(current, configuration)
	if err != nil {
		return ManagedListenerSnapshot{}, err
	}
	instance.mu.Lock()
	instance.config = saved
	instance.mu.Unlock()
	snapshot := instance.snapshot()
	manager.emit("updated", snapshot)
	return snapshot, nil
}

// Restore loads persistent listeners and reconciles desired-running state.
func (manager *Manager) Restore() error {
	if manager.store == nil {
		return nil
	}
	configurations, err := manager.store.List()
	if err != nil {
		return err
	}
	toStart := make([]string, 0)
	validated := make([]ManagedListenerConfig, 0, len(configurations))
	for _, configuration := range configurations {
		configuration, driver, validationErr := manager.validateConfiguration(configuration)
		if validationErr != nil {
			return fmt.Errorf("restore listener %q: %w", configuration.Name, validationErr)
		}
		if err := driver.Validate(configuration.Options, configuration.Routes); err != nil {
			return fmt.Errorf("restore listener %q: %w", configuration.Name, err)
		}
		validated = append(validated, configuration)
	}
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return ErrListenerManagerShutdown
	}
	for _, configuration := range validated {
		if manager.listeners[configuration.Name] != nil {
			manager.mu.Unlock()
			return fmt.Errorf("restore listener %q: listener exists", configuration.Name)
		}
	}
	for _, configuration := range validated {
		instance := &managedListener{
			config: configuration,
			status: Status{State: StateStopped, ChangedAt: time.Now().UTC()},
		}
		manager.listeners[configuration.Name] = instance
		if configuration.DesiredState == StateRunning {
			toStart = append(toStart, configuration.Name)
		}
	}
	manager.mu.Unlock()
	for _, name := range toStart {
		if _, err := manager.Start(name); err != nil {
			return fmt.Errorf("start restored listener %q: %w", name, err)
		}
	}
	return nil
}

func (manager *Manager) persistTransition(current ManagedListenerConfig, mutate func(*ManagedListenerConfig)) (ManagedListenerConfig, error) {
	candidate := cloneManagedListenerConfig(current)
	mutate(&candidate)
	if candidate.DesiredState == current.DesiredState && candidate.Persistent == current.Persistent {
		return candidate, nil
	}
	return manager.persistReplacement(current, candidate)
}

func (manager *Manager) persistReplacement(current, candidate ManagedListenerConfig) (ManagedListenerConfig, error) {
	if manager.store == nil {
		candidate.ConfigVersion = current.ConfigVersion + 1
		return candidate, nil
	}
	switch {
	case current.Persistent && candidate.Persistent:
		return manager.store.Update(candidate)
	case current.Persistent && !candidate.Persistent:
		if err := manager.store.Delete(current.Name); err != nil {
			return ManagedListenerConfig{}, err
		}
		candidate.ConfigVersion = current.ConfigVersion + 1
		return candidate, nil
	case !current.Persistent && candidate.Persistent:
		candidate.ConfigVersion = current.ConfigVersion + 1
		return manager.store.Insert(candidate)
	default:
		candidate.ConfigVersion = current.ConfigVersion + 1
		return candidate, nil
	}
}

func (manager *Manager) validateConfiguration(configuration ManagedListenerConfig) (ManagedListenerConfig, Driver, error) {
	configuration.Name = strings.TrimSpace(configuration.Name)
	configuration.Driver = normalizeRegistryID(configuration.Driver)
	if configuration.Name == "" {
		return ManagedListenerConfig{}, nil, errors.New("listener name is required")
	}
	if configuration.UUID == "" {
		return ManagedListenerConfig{}, nil, errors.New("listener UUID is required")
	}
	if configuration.Driver == "" {
		return ManagedListenerConfig{}, nil, errors.New("listener driver is required")
	}
	driver, found := manager.registry.Driver(configuration.Driver)
	if !found {
		return ManagedListenerConfig{}, nil, fmt.Errorf("listener driver %q is not registered", configuration.Driver)
	}
	if len(configuration.Options) == 0 || string(configuration.Options) == "null" {
		configuration.Options = json.RawMessage(`{}`)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(configuration.Options, &object); err != nil || object == nil {
		return ManagedListenerConfig{}, nil, errors.New("listener options must be a JSON object")
	}
	if configuration.DesiredState == "" {
		configuration.DesiredState = StateStopped
	}
	if configuration.DesiredState != StateStopped && configuration.DesiredState != StateRunning {
		return ManagedListenerConfig{}, nil, errors.New("listener desired state must be stopped or running")
	}
	if configuration.ConfigVersion < 1 {
		configuration.ConfigVersion = 1
	}
	configuration.Options = append(json.RawMessage(nil), configuration.Options...)
	configuration.Routes = cloneRoutes(configuration.Routes)
	return configuration, driver, nil
}

func (manager *Manager) lookup(name string) *managedListener {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.listeners[name]
}

func (manager *Manager) runtimeStopContext() (context.Context, context.CancelFunc) {
	timeout := manager.stopTimeout
	if timeout <= 0 {
		timeout = defaultRuntimeStopTimeout
	}
	return context.WithTimeout(context.Background(), timeout)
}

func (manager *Manager) emit(eventType string, snapshot ManagedListenerSnapshot) {
	if manager.publish != nil {
		manager.publish(ListenerEvent{Type: eventType, Listener: snapshot})
	}
}

func (instance *managedListener) snapshot() ManagedListenerSnapshot {
	instance.mu.RLock()
	defer instance.mu.RUnlock()
	return ManagedListenerSnapshot{Config: cloneManagedListenerConfig(instance.config), Status: instance.status}
}

func cloneManagedListenerConfig(source ManagedListenerConfig) ManagedListenerConfig {
	result := source
	result.Options = append(json.RawMessage(nil), source.Options...)
	result.Routes = cloneRoutes(source.Routes)
	return result
}

func cloneRoutes(source []Route) []Route {
	result := make([]Route, len(source))
	for index, route := range source {
		result[index] = route
		result[index].Match = append(json.RawMessage(nil), route.Match...)
		result[index].Options = append(json.RawMessage(nil), route.Options...)
		result[index].Inbound.Options = append(json.RawMessage(nil), route.Inbound.Options...)
		result[index].Outbound.Options = append(json.RawMessage(nil), route.Outbound.Options...)
	}
	return result
}
