package implant


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

type ImplantData struct {
	Metadata ImplantMetadata
}