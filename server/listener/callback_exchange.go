package listener

import (
	"context"

	"purpcmd/server/callback"
)

// CallbackExchangeHandler adapts the listener-neutral exchange contract to the
// encrypted implant callback protocol.
func CallbackExchangeHandler(_ context.Context, exchange Exchange) (ExchangeResult, error) {
	messageType, payload, err := callback.ParseCallbackWithContext(exchange.Payload, callback.TransportContext{
		ListenerName: exchange.ListenerName, ListenerUUID: exchange.ListenerUUID,
		Transport: exchange.Transport, RemoteAddress: exchange.RemoteAddress,
	}, exchange.AuthenticatedSession)
	return ExchangeResult{MessageType: messageType, Payload: payload}, err
}
