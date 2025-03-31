package implant

import "time"

/*
ImplantMetadata has 15bytes of fixed size
*/
type ImplantMetadata struct {
	PID       uint32
	SessionID uint32
	Sleep     uint32
	IP        uint32
	Socket    string
	Port      uint16
	Arch      byte

	User     string
	Hostname string
	Proc     string
}

type Implant struct {
	Name     string
	UUID     string
	key      string
	Metadata ImplantMetadata

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
