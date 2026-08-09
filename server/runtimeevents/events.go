package runtimeevents

import "sync"

var (
	mu        sync.RWMutex
	publisher func(string, any)
)

func SetPublisher(fn func(string, any)) {
	mu.Lock()
	publisher = fn
	mu.Unlock()
}

func Publish(eventType string, value any) {
	mu.RLock()
	fn := publisher
	mu.RUnlock()
	if fn != nil {
		fn(eventType, value)
	}
}
