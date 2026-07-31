package implant

// ImplantMetadata contains the 31-byte fixed network metadata block plus
// registration-only identity strings. Type is the payload family identifier
// used by the server to route Lua commands.
type ImplantMetadata struct {
	PID       uint32
	SessionID uint32
	Sleep     uint32
	IP        uint32
	Socket    string
	Port      uint16
	Arch      byte

	// One time secret
	OTS [12]byte

	User     string
	Hostname string
	Proc     string
	Type     string // Payload type; for example "impl" or "linux.impl".
}
