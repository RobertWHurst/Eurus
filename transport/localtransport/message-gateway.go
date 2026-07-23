package localtransport

import (
	"context"
	"fmt"

	"github.com/RobertWHurst/velaros"
)

// MessageGateway sends a message from service to gateway
func (t *LocalTransport) MessageGateway(gatewayID string, socketID string, msg *velaros.SocketMessage) error {
	transportLocalMessageDebug.Tracef("Sending message to gateway %s for socket %s", gatewayID, socketID)

	t.mu.RLock()
	handler, ok := t.gatewayMessageHandlers[gatewayID]
	t.mu.RUnlock()

	if ok {
		transportLocalMessageDebug.Tracef("Found handler for gateway %s, calling handler", gatewayID)
		handler(socketID, msg)
		transportLocalMessageDebug.Tracef("Handler for gateway %s completed", gatewayID)
	} else {
		transportLocalMessageDebug.Tracef("No handler found for gateway %s", gatewayID)
		return fmt.Errorf("no handler found for gateway %s", gatewayID)
	}

	return nil
}

// HandleGatewayMessages handles messages coming to a gateway until ctx is canceled.
func (t *LocalTransport) HandleGatewayMessages(ctx context.Context, ready chan<- struct{}, gatewayID string, handler func(socketID string, msg *velaros.SocketMessage)) error {
	transportLocalMessageDebug.Tracef("Handling messages for gateway %s", gatewayID)
	t.mu.Lock()
	t.gatewayMessageHandlers[gatewayID] = handler
	t.mu.Unlock()
	signalReady(ready)

	<-ctx.Done()
	transportLocalMessageDebug.Tracef("Unbinding message handler for gateway %s", gatewayID)
	t.mu.Lock()
	delete(t.gatewayMessageHandlers, gatewayID)
	t.mu.Unlock()
	return nil
}
