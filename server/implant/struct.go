package implant

import (
	"purpcmd/implant"
	"purpcmd/internal/encrypt"
	"time"
)

type Implant struct {
	Name     string
	UUID     string
	enc 	 encrypt.Encrypt
	Metadata implant.ImplantMetadata

	Alive     bool
	LastSeen  time.Time
	FirstSeen time.Time

	Task []*Task
	TaskMap map[[8]byte]*Task
}

type Task struct {
	ID         [8]byte
	Sent 	   bool
	Done	   bool
	Registered time.Time
	Code       uint16
	Payload    []byte

	ResponseTime time.Time
	Response   []byte // response payload
}
