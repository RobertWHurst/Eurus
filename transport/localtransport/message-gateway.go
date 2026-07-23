package localtransport

import (
	"context"
	"fmt"

	"github.com/RobertWHurst/eurus"
	"github.com/RobertWHurst/velaros"
)

// MessageGateway sends a message from service to gateway
func (t *LocalTransport) MessageGateway(gatewayID string, socketID string, msg *velaros.SocketMessage) error {
	transportLocalMessageDebug.Tracef("Sending message to gateway %s for socket %s", gatewayID, socketID)

	t.mu.RLock()
	handler, ok := t.gatewayMessageHandlers[gatewayID]
	t.mu.RUnlock()

	if !ok {
		transportLocalMessageDebug.Tracef("No handler found for gateway %s", gatewayID)
		return fmt.Errorf("no handler found for gateway %s", gatewayID)
	}

	handler(socketID, msg)
	return nil
}

// SubscribeGatewayMessages subscribes to messages addressed to a gateway.
func (t *LocalTransport) SubscribeGatewayMessages(_ context.Context, gatewayID string, handler func(socketID string, msg *velaros.SocketMessage)) (eurus.Subscription, error) {
	transportLocalMessageDebug.Tracef("Subscribing to messages for gateway %s", gatewayID)
	t.mu.Lock()
	t.gatewayMessageHandlers[gatewayID] = handler
	t.mu.Unlock()

	return untilDone(func() {
		transportLocalMessageDebug.Tracef("Unsubscribing from messages for gateway %s", gatewayID)
		t.mu.Lock()
		delete(t.gatewayMessageHandlers, gatewayID)
		t.mu.Unlock()
	}), nil
}
