package interactive

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrStreamNotFound = errors.New("interactive stream not found")
	ErrStreamExpired  = errors.New("interactive stream expired")
)

type stream struct {
	id        string
	session   string
	expires   time.Time
	implant   net.Conn
	client    net.Conn
	ready     chan struct{}
	readyOnce sync.Once
	serveOnce sync.Once
}

type Broker struct {
	mu      sync.Mutex
	streams map[string]*stream
	ttl     time.Duration
}

func New(ttl time.Duration) *Broker {
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	return &Broker{streams: make(map[string]*stream), ttl: ttl}
}

func (b *Broker) Open(session string) (string, time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked()
	id := uuid.NewString()
	expires := time.Now().Add(b.ttl)
	b.streams[id] = &stream{id: id, session: session, expires: expires, ready: make(chan struct{})}
	return id, expires
}

func (b *Broker) AttachImplant(id string, conn net.Conn) error {
	b.mu.Lock()
	b.pruneLocked()
	selected := b.streams[id]
	if selected == nil {
		b.mu.Unlock()
		return ErrStreamNotFound
	}
	if selected.implant != nil {
		b.mu.Unlock()
		return errors.New("interactive stream already has an implant")
	}
	selected.implant = conn
	b.signalReadyLocked(selected)
	b.mu.Unlock()
	go b.serve(selected)
	return nil
}

func (b *Broker) AttachClient(id string, conn net.Conn) error {
	b.mu.Lock()
	b.pruneLocked()
	selected := b.streams[id]
	if selected == nil {
		b.mu.Unlock()
		return ErrStreamNotFound
	}
	if time.Now().After(selected.expires) {
		delete(b.streams, id)
		b.mu.Unlock()
		return ErrStreamExpired
	}
	if selected.client != nil {
		b.mu.Unlock()
		return errors.New("interactive stream already has a client")
	}
	selected.client = conn
	b.signalReadyLocked(selected)
	b.mu.Unlock()
	go b.serve(selected)
	return nil
}

func (b *Broker) signalReadyLocked(selected *stream) {
	if selected.client != nil && selected.implant != nil {
		selected.readyOnce.Do(func() { close(selected.ready) })
	}
}

func (b *Broker) Close(id string) {
	b.mu.Lock()
	selected := b.streams[id]
	delete(b.streams, id)
	b.mu.Unlock()
	if selected != nil {
		if selected.client != nil {
			_ = selected.client.Close()
		}
		if selected.implant != nil {
			_ = selected.implant.Close()
		}
	}
}

func (b *Broker) serve(selected *stream) {
	selected.serveOnce.Do(func() {
		wait := time.Until(selected.expires)
		if wait < 0 {
			wait = 0
		}
		select {
		case <-selected.ready:
		case <-time.After(wait):
			b.Close(selected.id)
			return
		}
		done := make(chan struct{}, 2)
		copyHalf := func(dst, src net.Conn) {
			_, _ = io.Copy(dst, src)
			done <- struct{}{}
		}
		go copyHalf(selected.client, selected.implant)
		go copyHalf(selected.implant, selected.client)
		<-done
		b.Close(selected.id)
	})
}

func (b *Broker) pruneLocked() {
	now := time.Now()
	for id, selected := range b.streams {
		if now.After(selected.expires) {
			delete(b.streams, id)
			if selected.client != nil {
				_ = selected.client.Close()
			}
			if selected.implant != nil {
				_ = selected.implant.Close()
			}
		}
	}
}

var Default = New(2 * time.Minute)
