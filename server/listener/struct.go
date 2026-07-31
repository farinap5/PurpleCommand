package listener

import (
	"net/http"
	"sync"
)

type Listener struct {
	Name        string
	UUID        string
	Host        string
	Port        string
	Association int

	Proto      string
	Persistent bool
	TustXFF    bool

	SC *ServerController
}

type ServerController struct {
	server  *http.Server
	done    chan struct{}
	mu      sync.Mutex
	running bool
}

func (sc *ServerController) isRunning() bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.running
}

func (l *Listener) state() (persistent, running bool) {
	l.SC.mu.Lock()
	defer l.SC.mu.Unlock()
	return l.Persistent, l.SC.running
}

func (l *Listener) setPersistent(persistent bool) {
	l.SC.mu.Lock()
	l.Persistent = persistent
	l.SC.mu.Unlock()
}
