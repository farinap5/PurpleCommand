package listener

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type managerTestRuntime struct {
	address string
	done    chan error
	once    sync.Once
}

type managerRuntimeDriver struct {
	id      string
	runtime Runtime
}

func (driver managerRuntimeDriver) Definition() DriverDefinition {
	return DriverDefinition{ID: driver.id}
}
func (managerRuntimeDriver) Validate(json.RawMessage, []Route) error { return nil }
func (driver managerRuntimeDriver) Start(context.Context, RuntimeConfig, ExchangeHandler) (Runtime, error) {
	return driver.runtime, nil
}

type retryStopRuntime struct {
	done   chan error
	once   sync.Once
	mu     sync.Mutex
	stops  int
	active bool
}

func (runtime *retryStopRuntime) Address() string    { return "test://retry-stop" }
func (runtime *retryStopRuntime) Done() <-chan error { return runtime.done }
func (runtime *retryStopRuntime) Stop(context.Context) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.stops++
	if runtime.stops == 1 {
		return errors.New("stop failed")
	}
	runtime.active = false
	runtime.once.Do(func() { close(runtime.done) })
	return nil
}

type contextStopRuntime struct {
	done chan error
}

func (runtime *contextStopRuntime) Address() string    { return "test://context-stop" }
func (runtime *contextStopRuntime) Done() <-chan error { return runtime.done }
func (runtime *contextStopRuntime) Stop(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func (runtime *managerTestRuntime) Address() string    { return runtime.address }
func (runtime *managerTestRuntime) Done() <-chan error { return runtime.done }
func (runtime *managerTestRuntime) Stop(context.Context) error {
	runtime.once.Do(func() { close(runtime.done) })
	return nil
}

type managerTestDriver struct {
	mu       sync.Mutex
	starts   int
	runtimes []*managerTestRuntime
	startErr error
}

func (*managerTestDriver) Definition() DriverDefinition {
	return DriverDefinition{ID: "test", Capabilities: []string{"routes"}}
}
func (*managerTestDriver) Validate(options json.RawMessage, _ []Route) error {
	if string(options) == `{"invalid":true}` {
		return errors.New("invalid test options")
	}
	return nil
}
func (driver *managerTestDriver) Start(context.Context, RuntimeConfig, ExchangeHandler) (Runtime, error) {
	driver.mu.Lock()
	defer driver.mu.Unlock()
	if driver.startErr != nil {
		return nil, driver.startErr
	}
	runtime := &managerTestRuntime{address: "test://bound", done: make(chan error, 1)}
	driver.starts++
	driver.runtimes = append(driver.runtimes, runtime)
	return runtime, nil
}

func newManagerTest(t *testing.T) (*Manager, *managerTestDriver, *[]ListenerEvent) {
	t.Helper()
	registry := NewRegistry()
	driver := &managerTestDriver{}
	if err := registry.RegisterDriver(driver); err != nil {
		t.Fatal(err)
	}
	events := make([]ListenerEvent, 0)
	var eventsMu sync.Mutex
	manager := NewManager(registry, nil, func(event ListenerEvent) {
		eventsMu.Lock()
		events = append(events, event)
		eventsMu.Unlock()
	})
	return manager, driver, &events
}

func TestManagerLifecycleAndDefensiveSnapshots(t *testing.T) {
	manager, driver, events := newManagerTest(t)
	created, err := manager.Create(ManagedListenerConfig{
		Name: "alpha", UUID: "listener-id", Driver: "test", Persistent: true,
		Options: json.RawMessage(`{"value":"one"}`), Routes: []Route{{ID: "callback", Match: json.RawMessage(`{"path":"/"}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	created.Config.Options[2] = 'X'
	created.Config.Routes[0].Match[2] = 'X'
	stored, err := manager.Get("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if string(stored.Config.Options) != `{"value":"one"}` || string(stored.Config.Routes[0].Match) != `{"path":"/"}` {
		t.Fatalf("manager state was mutated through a snapshot: %#v", stored)
	}

	started, err := manager.Start("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if started.Status.State != StateRunning || started.Status.Address != "test://bound" || started.Config.DesiredState != StateRunning {
		t.Fatalf("started = %#v", started)
	}
	if _, err := manager.Start("alpha"); !errors.Is(err, ErrManagedListenerRunning) {
		t.Fatalf("second start error = %v", err)
	}
	stopped, err := manager.Stop("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Status.State != StateStopped || stopped.Config.DesiredState != StateStopped {
		t.Fatalf("stopped = %#v", stopped)
	}
	restarted, err := manager.Restart("alpha")
	if err != nil || restarted.Status.State != StateRunning {
		t.Fatalf("restart = %#v, %v", restarted, err)
	}
	deleted, err := manager.Delete("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Status.State != StateStopped {
		t.Fatalf("deleted = %#v", deleted)
	}
	if _, err := manager.Get("alpha"); !errors.Is(err, ErrManagedListenerNotFound) {
		t.Fatalf("get deleted error = %v", err)
	}
	if driver.starts != 2 {
		t.Fatalf("driver starts = %d", driver.starts)
	}
	if len(*events) < 8 || (*events)[0].Type != "created" || (*events)[len(*events)-1].Type != "deleted" {
		t.Fatalf("events = %#v", *events)
	}
}

func TestManagerGetByUUID(t *testing.T) {
	manager, _, _ := newManagerTest(t)
	if _, err := manager.Create(ManagedListenerConfig{
		Name: "alpha", UUID: "stable-id", Driver: "test", Persistent: true,
		Options: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.GetByUUID(" stable-id ")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Config.Name != "alpha" || snapshot.Config.UUID != "stable-id" {
		t.Fatalf("resolved snapshot = %#v", snapshot)
	}
	snapshot.Config.Options[0] = '['
	stored, err := manager.GetByUUID("stable-id")
	if err != nil {
		t.Fatal(err)
	}
	if string(stored.Config.Options) != `{}` {
		t.Fatalf("manager returned a mutable snapshot: %s", stored.Config.Options)
	}
	if _, err := manager.GetByUUID(""); !errors.Is(err, ErrManagedListenerNotFound) {
		t.Fatalf("empty UUID error = %v", err)
	}
	if _, err := manager.GetByUUID("missing"); !errors.Is(err, ErrManagedListenerNotFound) {
		t.Fatalf("missing UUID error = %v", err)
	}
}

func TestManagerRejectsDuplicateAndNormalizesUUID(t *testing.T) {
	manager, _, _ := newManagerTest(t)
	created, err := manager.Create(ManagedListenerConfig{
		Name: "alpha", UUID: " stable-id ", Driver: "test", Persistent: false,
		Options: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Config.UUID != "stable-id" {
		t.Fatalf("normalized UUID = %q", created.Config.UUID)
	}
	if _, err := manager.Create(ManagedListenerConfig{
		Name: "beta", UUID: "stable-id", Driver: "test", Persistent: false,
		Options: json.RawMessage(`{}`),
	}); err == nil || !strings.Contains(err.Error(), "UUID already exists") {
		t.Fatalf("duplicate UUID error = %v", err)
	}
}

func TestManagerRecordsUnexpectedRuntimeFailure(t *testing.T) {
	manager, driver, _ := newManagerTest(t)
	if _, err := manager.Create(ManagedListenerConfig{Name: "failure", UUID: "id", Driver: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start("failure"); err != nil {
		t.Fatal(err)
	}
	driver.mu.Lock()
	runtime := driver.runtimes[0]
	driver.mu.Unlock()
	runtime.done <- errors.New("serve failed")
	close(runtime.done)
	deadline := time.Now().Add(time.Second)
	for {
		snapshot, err := manager.Get("failure")
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Status.State == StateFailed {
			if snapshot.Status.LastError != "serve failed" {
				t.Fatalf("failure = %#v", snapshot.Status)
			}
			if snapshot.Config.DesiredState != StateRunning {
				t.Fatalf("failed listener lost desired running state: %#v", snapshot.Config)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("runtime failure was not observed: %#v", snapshot.Status)
		}
		time.Sleep(time.Millisecond)
	}
	stopped, err := manager.Stop("failure")
	if err != nil || stopped.Status.State != StateStopped || stopped.Config.DesiredState != StateStopped {
		t.Fatalf("stop failed listener = %#v, %v", stopped, err)
	}
}

func TestManagerSerializesConcurrentStarts(t *testing.T) {
	manager, driver, _ := newManagerTest(t)
	if _, err := manager.Create(ManagedListenerConfig{Name: "concurrent", UUID: "id", Driver: "test"}); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 16)
	var workers sync.WaitGroup
	for index := 0; index < cap(results); index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := manager.Start("concurrent")
			results <- err
		}()
	}
	workers.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrManagedListenerRunning) {
			t.Fatalf("start error = %v", err)
		}
	}
	if successes != 1 || driver.starts != 1 {
		t.Fatalf("successes=%d starts=%d", successes, driver.starts)
	}
	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start("concurrent"); !errors.Is(err, ErrListenerManagerShutdown) {
		t.Fatalf("start after shutdown error = %v", err)
	}
	if _, err := manager.Create(ManagedListenerConfig{Name: "late", UUID: "late-id", Driver: "test"}); !errors.Is(err, ErrListenerManagerShutdown) {
		t.Fatalf("create after shutdown error = %v", err)
	}
}

func TestManagerDeleteRetriesFailedRuntimeStop(t *testing.T) {
	runtime := &retryStopRuntime{done: make(chan error), active: true}
	registry := NewRegistry()
	if err := registry.RegisterDriver(managerRuntimeDriver{id: "retry", runtime: runtime}); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(registry, nil, nil)
	if _, err := manager.Create(ManagedListenerConfig{Name: "retry", UUID: "retry-id", Driver: "retry"}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start("retry"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Stop("retry"); err == nil {
		t.Fatal("first stop unexpectedly succeeded")
	}
	if _, err := manager.Start("retry"); !errors.Is(err, ErrManagedListenerRunning) {
		t.Fatalf("start with retained runtime error = %v", err)
	}
	if _, err := manager.Delete("retry"); err != nil {
		t.Fatalf("delete did not retry runtime stop: %v", err)
	}
	if _, err := manager.Get("retry"); !errors.Is(err, ErrManagedListenerNotFound) {
		t.Fatalf("deleted listener lookup error = %v", err)
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.active || runtime.stops != 2 {
		t.Fatalf("runtime active=%t stops=%d", runtime.active, runtime.stops)
	}
}

func TestManagerDeleteUsesBoundedStopContext(t *testing.T) {
	runtime := &contextStopRuntime{done: make(chan error)}
	registry := NewRegistry()
	if err := registry.RegisterDriver(managerRuntimeDriver{id: "bounded", runtime: runtime}); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(registry, nil, nil)
	manager.stopTimeout = 25 * time.Millisecond
	if _, err := manager.Create(ManagedListenerConfig{Name: "bounded", UUID: "bounded-id", Driver: "bounded"}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start("bounded"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err := manager.Delete("bounded"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("delete error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("bounded delete took %s", elapsed)
	}
	if _, err := manager.Get("bounded"); err != nil {
		t.Fatalf("failed delete removed listener: %v", err)
	}
	close(runtime.done)
}
