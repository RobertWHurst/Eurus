package localtransport

import (
	"context"
	"fmt"

	"github.com/RobertWHurst/eurus/v2"
	"github.com/RobertWHurst/velaros"
)

// MessageService sends a message from gateway to service
func (t *LocalTransport) MessageService(serviceID, gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) error {
	transportLocalMessageDebug.Tracef("Sending message to service %s from gateway %s socket %s", serviceID, gatewayID, socketID)

	t.mu.RLock()
	handler, ok := t.serviceMessageHandlers[serviceID]
	t.mu.RUnlock()

	if !ok {
		transportLocalMessageDebug.Tracef("No handler found for service %s", serviceID)
		return fmt.Errorf("no handler found for service %s", serviceID)
	}

	handler(gatewayID, socketID, connInfo, msg)
	return nil
}

// SubscribeServiceMessages subscribes to messages addressed to a service
// instance.
func (t *LocalTransport) SubscribeServiceMessages(_ context.Context, serviceID string, handler func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage)) (eurus.Subscription, error) {
	transportLocalMessageDebug.Tracef("Subscribing to messages for service %s", serviceID)
	t.mu.Lock()
	t.serviceMessageHandlers[serviceID] = handler
	t.mu.Unlock()

	return untilDone(func() {
		transportLocalMessageDebug.Tracef("Unsubscribing from messages for service %s", serviceID)
		t.mu.Lock()
		delete(t.serviceMessageHandlers, serviceID)
		t.mu.Unlock()
	}), nil
}
