package listener

import (
	"context"
	"errors"
	"fmt"

	"purpcmd/pkg/teamapi"
	"purpcmd/server/callback"
)

// ErrInvalidImplantRequest marks an exchange failure that an HTTP driver may
// safely disguise with a configured hosted-file fallback.
var ErrInvalidImplantRequest = errors.New("invalid implant request")

// CallbackExchangeHandler adapts the listener-neutral exchange contract to the
// encrypted implant callback protocol.
func CallbackExchangeHandler(_ context.Context, exchange Exchange) (ExchangeResult, error) {
	messageType, payload, err := callback.ParseCallbackWithContext(exchange.Payload, callback.TransportContext{
		Kind: teamapi.SessionTransportListener, Name: exchange.ListenerName, UUID: exchange.ListenerUUID,
		Protocol: exchange.Transport, RemoteAddress: exchange.RemoteAddress,
	}, exchange.AuthenticatedSession)
	if errors.Is(err, callback.ErrMalformedPayload) {
		err = fmt.Errorf("%w: %v", ErrInvalidImplantRequest, err)
	}
	return ExchangeResult{MessageType: messageType, Payload: payload}, err
}
