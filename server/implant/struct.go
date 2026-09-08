package implant

import (
	"purpcmd/implant"
	"purpcmd/internal/encrypt"
	"purpcmd/pkg/teamapi"
	"sync"
	"time"
)

type Implant struct {
	Name             string
	UUID             string
	Enc              encrypt.Encrypt
	Metadata         implant.ImplantMetadata
	Transport        string
	Speaker          string
	SpeakerUUID      string
	HealthMonitoring bool
	Listener         string
	ListenerUUID     string

	Alive       bool
	Terminating bool
	LastSeen    time.Time
	FirstSeen   time.Time

	Task      []*Task
	TaskMap   map[[8]byte]*Task
	taskMu    *sync.Mutex
	taskReady chan struct{}
	taskRetry time.Duration
}

func (i *Implant) sessionRoute() (transport, speaker, speakerUUID, listener, listenerUUID string, healthMonitoring bool) {
	mu := i.taskMutex()
	mu.Lock()
	defer mu.Unlock()
	transport = i.Transport
	if transport == "" {
		transport = teamapi.SessionTransportListener
	}
	return transport, i.Speaker, i.SpeakerUUID, i.Listener, i.ListenerUUID, i.HealthMonitoring
}

type Task struct {
	ID         [8]byte
	Sent       bool
	Done       bool
	Processing bool
	Attempts   uint32
	LastSent   time.Time
	Registered time.Time
	Code       uint16
	Payload    []byte

	ResponseTime time.Time
	Response     []byte // response payload
}
