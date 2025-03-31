package listener

import (
	"net/http"
	"sync"
)

type Listener struct {
	Name	string
	UUID	string
	Host 	string
	Port 	string
	Association int

	Proto 	string
	Persistent bool
	TustXFF bool

	SC *ServerController
}

type ServerController struct {
	server   *http.Server
	wg       sync.WaitGroup
	stopChan chan struct{}
	running  bool
}

