package speaker

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"
	serverimplant "purpcmd/server/implant"

	"github.com/google/uuid"
)

var (
	ErrNotFound        = errors.New("speaker not found")
	ErrAlreadyRunning  = errors.New("speaker is already running")
	ErrNotRunning      = errors.New("speaker is not running")
	ErrManagerClosed   = errors.New("speaker manager is shut down")
	ErrVersionConflict = errors.New("speaker configuration version conflict")
)

const (
	StateStopped           = "stopped"
	StateRunning           = "running"
	minimumConfiguredRetry = 100 * time.Millisecond
)

var speakerNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

type Store interface {
	Insert(teamapi.Speaker) error
	Update(previousName string, expectedVersion uint64, item teamapi.Speaker) error
	Delete(name, id string) error
	List() ([]teamapi.Speaker, error)
}

type DBStore struct{}

func (DBStore) Insert(item teamapi.Speaker) error { return db.DBSpeakerInsert(item) }
func (DBStore) Update(previousName string, expectedVersion uint64, item teamapi.Speaker) error {
	err := db.DBSpeakerUpdate(previousName, expectedVersion, item)
	if errors.Is(err, db.ErrSpeakerConfigVersionConflict) {
		return ErrVersionConflict
	}
	return err
}
func (DBStore) Delete(name, id string) error     { return db.DBSpeakerDelete(name, id) }
func (DBStore) List() ([]teamapi.Speaker, error) { return db.DBSpeakerList() }

type Publisher func(eventType string, value any)

type managedSpeaker struct {
	operationMu sync.Mutex
	mu          sync.RWMutex
	value       teamapi.Speaker
	worker      *Worker
	deleted     bool
}

type Manager struct {
	ctx    context.Context
	cancel context.CancelFunc
	store  Store
	emit   Publisher

	mu     sync.RWMutex
	byName map[string]*managedSpeaker
	byUUID map[string]*managedSpeaker
	closed bool
}

func NewManager(store Store, publisher Publisher) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		ctx: ctx, cancel: cancel, store: store, emit: publisher,
		byName: make(map[string]*managedSpeaker), byUUID: make(map[string]*managedSpeaker),
	}
}

func (manager *Manager) Create(request teamapi.SpeakerCreateRequest) (teamapi.Speaker, error) {
	name := strings.TrimSpace(request.Name)
	if err := validateSpeaker(name, request.Config); err != nil {
		return teamapi.Speaker{}, err
	}
	persistent := true
	if request.Persistent != nil {
		persistent = *request.Persistent
	}
	item := teamapi.Speaker{
		Name: name, UUID: uuid.NewString(), Persistent: persistent,
		State: StateStopped, DesiredState: StateStopped, ConfigVersion: 1,
		Config: cloneSpeakerConfig(request.Config),
	}
	instance := &managedSpeaker{value: item}

	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return teamapi.Speaker{}, ErrManagerClosed
	}
	if manager.byName[name] != nil {
		manager.mu.Unlock()
		return teamapi.Speaker{}, errors.New("speaker exists")
	}
	if item.Persistent && manager.store != nil {
		if err := manager.store.Insert(item); err != nil {
			manager.mu.Unlock()
			return teamapi.Speaker{}, err
		}
	}
	manager.byName[name] = instance
	manager.byUUID[item.UUID] = instance
	manager.mu.Unlock()
	result := instance.snapshot()
	manager.publish(teamapi.EventSpeakerCreated, result)
	return result, nil
}

func (manager *Manager) Get(name string) (teamapi.Speaker, error) {
	instance := manager.lookupName(name)
	if instance == nil {
		return teamapi.Speaker{}, ErrNotFound
	}
	return instance.snapshot(), nil
}

func (manager *Manager) GetByUUID(id string) (teamapi.Speaker, error) {
	manager.mu.RLock()
	instance := manager.byUUID[strings.TrimSpace(id)]
	manager.mu.RUnlock()
	if instance == nil {
		return teamapi.Speaker{}, ErrNotFound
	}
	return instance.snapshot(), nil
}

func (manager *Manager) List() []teamapi.Speaker {
	manager.mu.RLock()
	instances := make([]*managedSpeaker, 0, len(manager.byName))
	for _, instance := range manager.byName {
		instances = append(instances, instance)
	}
	manager.mu.RUnlock()
	result := make([]teamapi.Speaker, 0, len(instances))
	for _, instance := range instances {
		result = append(result, instance.snapshot())
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result
}

func (manager *Manager) Start(name string) (teamapi.Speaker, error) {
	instance := manager.lookupName(name)
	if instance == nil {
		return teamapi.Speaker{}, ErrNotFound
	}
	instance.operationMu.Lock()
	defer instance.operationMu.Unlock()
	return manager.startLocked(instance)
}

func (manager *Manager) startLocked(instance *managedSpeaker) (teamapi.Speaker, error) {
	manager.mu.RLock()
	closed := manager.closed
	manager.mu.RUnlock()
	if closed {
		return teamapi.Speaker{}, ErrManagerClosed
	}
	instance.mu.Lock()
	if instance.deleted {
		instance.mu.Unlock()
		return teamapi.Speaker{}, ErrNotFound
	}
	if instance.worker != nil {
		instance.mu.Unlock()
		return teamapi.Speaker{}, ErrAlreadyRunning
	}
	current := cloneSpeaker(instance.value)
	instance.mu.Unlock()

	updated, err := manager.persistChange(current, func(candidate *teamapi.Speaker) {
		candidate.DesiredState = StateRunning
	})
	if err != nil {
		return teamapi.Speaker{}, err
	}
	instance.mu.Lock()
	instance.value = updated
	instance.value.State = string(WorkerConnecting)
	instance.value.LastError = ""
	instance.mu.Unlock()
	manager.publish(teamapi.EventSpeakerConnecting, instance.snapshot())

	worker, err := StartWorker(manager.ctx, updated.Name, updated.UUID, updated.Config, func(snapshot WorkerSnapshot) {
		manager.observe(instance, snapshot)
	})
	if err != nil {
		instance.mu.Lock()
		instance.worker = nil
		instance.value.State = string(WorkerFailed)
		instance.value.LastError = err.Error()
		instance.mu.Unlock()
		manager.publish(teamapi.EventSpeakerFailed, instance.snapshot())
		return teamapi.Speaker{}, err
	}
	instance.mu.Lock()
	instance.worker = worker
	instance.value.Running = true
	instance.mu.Unlock()
	return instance.snapshot(), nil
}

func (manager *Manager) Stop(name string) (teamapi.Speaker, error) {
	instance := manager.lookupName(name)
	if instance == nil {
		return teamapi.Speaker{}, ErrNotFound
	}
	instance.operationMu.Lock()
	defer instance.operationMu.Unlock()
	return manager.stopLocked(instance, true)
}

func (manager *Manager) stopLocked(instance *managedSpeaker, persistDesired bool) (teamapi.Speaker, error) {
	instance.mu.Lock()
	if instance.deleted {
		instance.mu.Unlock()
		return teamapi.Speaker{}, ErrNotFound
	}
	current := cloneSpeaker(instance.value)
	worker := instance.worker
	if worker == nil && current.State == StateStopped {
		instance.mu.Unlock()
		return teamapi.Speaker{}, ErrNotRunning
	}
	instance.mu.Unlock()

	updated := current
	var err error
	if persistDesired {
		updated, err = manager.persistChange(current, func(candidate *teamapi.Speaker) {
			candidate.DesiredState = StateStopped
		})
		if err != nil {
			return teamapi.Speaker{}, err
		}
	}
	if worker != nil {
		worker.Stop()
	}
	instance.mu.Lock()
	instance.worker = nil
	instance.value = updated
	instance.value.Running = false
	instance.value.InFlight = false
	instance.value.State = StateStopped
	instance.mu.Unlock()
	result := instance.snapshot()
	if worker == nil {
		manager.publish(teamapi.EventSpeakerStopped, result)
	}
	return result, nil
}

func (manager *Manager) Restart(name string) (teamapi.Speaker, error) {
	instance := manager.lookupName(name)
	if instance == nil {
		return teamapi.Speaker{}, ErrNotFound
	}
	instance.operationMu.Lock()
	defer instance.operationMu.Unlock()
	instance.mu.RLock()
	running := instance.worker != nil || instance.value.State != StateStopped
	instance.mu.RUnlock()
	if running {
		if _, err := manager.stopLocked(instance, false); err != nil {
			return teamapi.Speaker{}, err
		}
	}
	return manager.startLocked(instance)
}

func (manager *Manager) Update(currentName string, request teamapi.SpeakerUpdateRequest) (teamapi.Speaker, error) {
	instance := manager.lookupName(currentName)
	if instance == nil {
		return teamapi.Speaker{}, ErrNotFound
	}
	instance.operationMu.Lock()
	defer instance.operationMu.Unlock()
	instance.mu.RLock()
	current := cloneSpeaker(instance.value)
	running := instance.worker != nil
	instance.mu.RUnlock()
	if running {
		return teamapi.Speaker{}, ErrAlreadyRunning
	}
	if request.ExpectedConfigVersion != nil && *request.ExpectedConfigVersion != current.ConfigVersion {
		return teamapi.Speaker{}, ErrVersionConflict
	}
	candidate := cloneSpeaker(current)
	if strings.TrimSpace(request.NewName) != "" {
		candidate.Name = strings.TrimSpace(request.NewName)
	}
	if request.Persistent != nil {
		candidate.Persistent = *request.Persistent
	}
	if request.Config != nil {
		candidate.Config = cloneSpeakerConfig(*request.Config)
	}
	if err := validateSpeaker(candidate.Name, candidate.Config); err != nil {
		return teamapi.Speaker{}, err
	}
	if candidate.Name != current.Name {
		manager.mu.RLock()
		collision := manager.byName[candidate.Name]
		manager.mu.RUnlock()
		if collision != nil {
			return teamapi.Speaker{}, errors.New("speaker exists")
		}
	}
	updated, err := manager.persistReplacement(current, candidate)
	if err != nil {
		return teamapi.Speaker{}, err
	}
	instance.mu.Lock()
	instance.value = updated
	instance.mu.Unlock()
	if current.Name != updated.Name {
		manager.mu.Lock()
		delete(manager.byName, current.Name)
		manager.byName[updated.Name] = instance
		manager.mu.Unlock()
		serverimplant.ImplantRenameSpeaker(updated.UUID, current.Name, updated.Name)
	}
	result := instance.snapshot()
	manager.publish(teamapi.EventSpeakerUpdated, result)
	return result, nil
}

func (manager *Manager) Delete(name string) (teamapi.Speaker, error) {
	instance := manager.lookupName(name)
	if instance == nil {
		return teamapi.Speaker{}, ErrNotFound
	}
	instance.operationMu.Lock()
	defer instance.operationMu.Unlock()
	instance.mu.Lock()
	if instance.worker != nil {
		instance.mu.Unlock()
		return teamapi.Speaker{}, ErrAlreadyRunning
	}
	current := cloneSpeaker(instance.value)
	instance.mu.Unlock()
	if current.Persistent && manager.store != nil {
		if err := manager.store.Delete(current.Name, current.UUID); err != nil {
			return teamapi.Speaker{}, err
		}
	}
	manager.mu.Lock()
	delete(manager.byName, current.Name)
	delete(manager.byUUID, current.UUID)
	manager.mu.Unlock()
	instance.mu.Lock()
	instance.deleted = true
	instance.mu.Unlock()
	manager.publish(teamapi.EventSpeakerDeleted, map[string]string{"name": current.Name, "uuid": current.UUID})
	return current, nil
}

func (manager *Manager) Restore() error {
	if manager.store == nil {
		return nil
	}
	stored, err := manager.store.List()
	if err != nil {
		return err
	}
	toStart := make([]*managedSpeaker, 0)
	manager.mu.Lock()
	for _, item := range stored {
		if err := validateSpeaker(item.Name, item.Config); err != nil {
			manager.mu.Unlock()
			return fmt.Errorf("restore speaker %s: %w", item.Name, err)
		}
		item.State = StateStopped
		item.Running = false
		item.InFlight = false
		instance := &managedSpeaker{value: cloneSpeaker(item)}
		if manager.byName[item.Name] != nil || manager.byUUID[item.UUID] != nil {
			manager.mu.Unlock()
			return fmt.Errorf("restore duplicate speaker %s", item.Name)
		}
		manager.byName[item.Name] = instance
		manager.byUUID[item.UUID] = instance
		if item.DesiredState == StateRunning {
			toStart = append(toStart, instance)
		}
	}
	manager.mu.Unlock()
	for _, instance := range toStart {
		instance.operationMu.Lock()
		_, startErr := manager.startLocked(instance)
		instance.operationMu.Unlock()
		if startErr != nil {
			// Keep the desired running state and failed status for operator
			// inspection; another speaker can still be restored.
			continue
		}
	}
	return nil
}

func (manager *Manager) Shutdown() {
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return
	}
	manager.closed = true
	instances := make([]*managedSpeaker, 0, len(manager.byName))
	for _, instance := range manager.byName {
		instances = append(instances, instance)
	}
	manager.mu.Unlock()
	manager.cancel()
	for _, instance := range instances {
		instance.operationMu.Lock()
		_, _ = manager.stopLocked(instance, false)
		instance.operationMu.Unlock()
	}
}

func (manager *Manager) observe(instance *managedSpeaker, snapshot WorkerSnapshot) {
	instance.mu.Lock()
	previous := instance.value.State
	instance.value.State = string(snapshot.State)
	instance.value.Running = snapshot.State == WorkerConnecting || snapshot.State == WorkerConnected || snapshot.State == WorkerDisconnected
	instance.value.InFlight = snapshot.InFlight
	instance.value.Session = snapshot.Session
	instance.value.LastAttemptAt = snapshot.LastAttemptAt
	instance.value.LastSuccessAt = snapshot.LastSuccessAt
	instance.value.LastError = snapshot.LastError
	instance.mu.Unlock()
	if previous == string(snapshot.State) {
		return
	}
	switch snapshot.State {
	case WorkerConnected:
		manager.publish(teamapi.EventSpeakerConnected, instance.snapshot())
	case WorkerDisconnected:
		manager.publish(teamapi.EventSpeakerDisconnected, instance.snapshot())
	case WorkerFailed:
		manager.publish(teamapi.EventSpeakerFailed, instance.snapshot())
	case WorkerStopped:
		manager.publish(teamapi.EventSpeakerStopped, instance.snapshot())
	}
}

func (manager *Manager) persistChange(current teamapi.Speaker, mutate func(*teamapi.Speaker)) (teamapi.Speaker, error) {
	candidate := cloneSpeaker(current)
	mutate(&candidate)
	if candidate.DesiredState == current.DesiredState && candidate.Persistent == current.Persistent {
		return candidate, nil
	}
	return manager.persistReplacement(current, candidate)
}

func (manager *Manager) persistReplacement(current, candidate teamapi.Speaker) (teamapi.Speaker, error) {
	candidate.ConfigVersion = current.ConfigVersion + 1
	if manager.store == nil {
		return candidate, nil
	}
	switch {
	case current.Persistent && candidate.Persistent:
		if err := manager.store.Update(current.Name, current.ConfigVersion, candidate); err != nil {
			return teamapi.Speaker{}, err
		}
	case current.Persistent && !candidate.Persistent:
		if err := manager.store.Delete(current.Name, current.UUID); err != nil {
			return teamapi.Speaker{}, err
		}
	case !current.Persistent && candidate.Persistent:
		if err := manager.store.Insert(candidate); err != nil {
			return teamapi.Speaker{}, err
		}
	}
	return candidate, nil
}

func (manager *Manager) lookupName(name string) *managedSpeaker {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.byName[strings.TrimSpace(name)]
}

func (instance *managedSpeaker) snapshot() teamapi.Speaker {
	instance.mu.RLock()
	result := cloneSpeaker(instance.value)
	instance.mu.RUnlock()
	result.Associations = speakerAssociationCount(result.Name, result.UUID)
	return result
}

func speakerAssociationCount(name, id string) int {
	count := 0
	for _, session := range serverimplant.APIListSessions() {
		if session.Transport == teamapi.SessionTransportSpeaker &&
			((id != "" && session.SpeakerUUID == id) || (session.SpeakerUUID == "" && session.Speaker == name)) {
			count++
		}
	}
	return count
}

func validateSpeaker(name string, configuration teamapi.SpeakerConfig) error {
	if !speakerNamePattern.MatchString(name) {
		return errors.New("speaker name must be 1-64 characters using letters, numbers, '.', '_', or '-'")
	}
	engine, err := NewRequestEngine(configuration.Client)
	if err != nil {
		return err
	}
	engine.CloseIdleConnections()
	if configuration.Healthcheck != nil {
		if configuration.Healthcheck.Interval < 0 {
			return errors.New("healthcheck interval cannot be negative")
		}
		if configuration.Healthcheck.FailureThreshold < 0 || configuration.Healthcheck.FailureThreshold > 100 {
			return errors.New("healthcheck failure threshold must be between 1 and 100")
		}
	}
	if configuration.Retry.Interval < 0 {
		return errors.New("retry interval cannot be negative")
	}
	if configuration.Retry.Interval > 0 && configuration.Retry.Interval < minimumConfiguredRetry {
		return fmt.Errorf("retry interval must be at least %s", minimumConfiguredRetry)
	}
	return nil
}

func cloneSpeaker(item teamapi.Speaker) teamapi.Speaker {
	result := item
	result.Config = cloneSpeakerConfig(item.Config)
	return result
}

func (manager *Manager) publish(eventType string, value any) {
	if manager.emit != nil {
		if item, ok := value.(teamapi.Speaker); ok {
			value = Redact(item)
		}
		manager.emit(eventType, value)
	}
}
