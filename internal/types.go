package internal


// default commands
const (
	NILCMD = uint16(iota)
	PING
)

const (
	NILARCH = uint8(iota)
	AMD64
)

const (
	NIL = uint16(iota) // Nothing
	REG // Register - Used by the implant to register itself
	CHK // Check (Health check) - Used by the implant to check for new tasks
	RSP // Response - Used by the implant to post a response
)