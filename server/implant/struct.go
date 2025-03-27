package implant

import "time"

/*
	ImplantMetadata has 15bytes of fixed size
*/
type ImplantMetadata struct {
	PID         uint32
	SessionID   uint32
	IP          uint32
	Port        uint16
	Arch        byte

	User		string
	Hostname	string
	Proc		string
}

type Implant struct {
	Name 	string
	UUID 	string
	key  	string
	Metadata ImplantMetadata

	Alive 		bool
	Sleep		int
	LastSeen 	time.Time
	FirstSeen 	time.Time

	Task []*Task
}

type Task struct {
	ID 			string
	Registered 	time.Time
	Code 		uint16
	Payload 	[]byte
}