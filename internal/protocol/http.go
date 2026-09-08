package protocol

// ExchangeHeader selects the HTTP transport operation. It is transport
// metadata only: the bodies remain the same REG, CHK, TASK, RSP, and CHU
// protocol frames used by reverse listeners.
const ExchangeHeader = "X-PurpleCommand-Exchange"

const (
	ExchangeRegistration = "registration"
	ExchangeHealthcheck  = "healthcheck"
	ExchangeTask         = "task"
)
