package events

import (
	"encoding/json"
	"sync"
	"time"

	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"
)

type Bus struct {
	mu          sync.RWMutex
	subscribers map[uint64]chan teamapi.EventRecord
	nextID      uint64
}

func New() *Bus {
	return &Bus{subscribers: make(map[uint64]chan teamapi.EventRecord)}
}

func (bus *Bus) Publish(eventType string, value any) (teamapi.EventRecord, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return teamapi.EventRecord{}, err
	}
	event, err := db.DBEventInsert(eventType, data, time.Now())
	if err != nil {
		return teamapi.EventRecord{}, err
	}

	bus.mu.RLock()
	slow := make([]uint64, 0)
	for id, subscriber := range bus.subscribers {
		select {
		case subscriber <- event:
		default:
			slow = append(slow, id)
		}
	}
	bus.mu.RUnlock()

	if len(slow) > 0 {
		bus.mu.Lock()
		for _, id := range slow {
			if subscriber := bus.subscribers[id]; subscriber != nil {
				delete(bus.subscribers, id)
				close(subscriber)
			}
		}
		bus.mu.Unlock()
	}
	return event, nil
}

func (bus *Bus) Subscribe(buffer int) (<-chan teamapi.EventRecord, func()) {
	if buffer < 1 {
		buffer = 128
	}
	bus.mu.Lock()
	bus.nextID++
	id := bus.nextID
	channel := make(chan teamapi.EventRecord, buffer)
	bus.subscribers[id] = channel
	bus.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			bus.mu.Lock()
			if subscriber := bus.subscribers[id]; subscriber != nil {
				delete(bus.subscribers, id)
				close(subscriber)
			}
			bus.mu.Unlock()
		})
	}
	return channel, cancel
}

func (bus *Bus) Replay(after uint64, limit int) ([]teamapi.EventRecord, error) {
	return db.DBEventList(after, limit)
}

func (bus *Bus) LatestSequence() (uint64, error) {
	return db.DBEventLatestSequence()
}
