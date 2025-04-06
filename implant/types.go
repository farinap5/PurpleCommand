package implant



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
	
	// One time secret
	OTS		  [12]byte

	User     string
	Hostname string
	Proc     string
}