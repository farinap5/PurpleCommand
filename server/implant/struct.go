package implant

import (
	"purpcmd/implant"
	"purpcmd/internal/encrypt"
	"sync"
	"time"
)

type Implant struct {
	Name     string
	UUID     string
	Enc      encrypt.Encrypt
	Metadata implant.ImplantMetadata

	Alive       bool
	Terminating bool
	LastSeen    time.Time
	FirstSeen   time.Time

	Task    []*Task
	TaskMap map[[8]byte]*Task
	taskMu  *sync.Mutex
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
