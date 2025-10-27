package localtransport

import (
	"github.com/coder/websocket"
)

// MessageGateway sends a message from service to gateway
func (t *LocalTransport) MessageGateway(gatewayID string, socketID string, msgType websocket.MessageType, msgData []byte) error {
	transportLocalMessageDebug.Tracef("Sending message to gateway %s for socket %s", gatewayID, socketID)

	t.mu.RLock()
	handler, ok := t.gatewayMessageHandlers[gatewayID]
	t.mu.RUnlock()

	if ok {
		transportLocalMessageDebug.Tracef("Found handler for gateway %s, calling handler", gatewayID)
		handler(socketID, msgType, msgData)
		transportLocalMessageDebug.Tracef("Handler for gateway %s completed", gatewayID)
	} else {
		transportLocalMessageDebug.Tracef("No handler found for gateway %s", gatewayID)
	}

	return nil
}

// BindMessageGateway binds handler for messages coming to a gateway
func (t *LocalTransport) BindMessageGateway(gatewayID string, handler func(socketID string, msgType websocket.MessageType, msgData []byte)) error {
	transportLocalMessageDebug.Tracef("Binding message handler for gateway %s", gatewayID)
	t.mu.Lock()
	t.gatewayMessageHandlers[gatewayID] = handler
	t.mu.Unlock()
	return nil
}

// UnbindMessageGateway unbinds the message handler for a gateway
func (t *LocalTransport) UnbindMessageGateway(gatewayID string) error {
	transportLocalMessageDebug.Tracef("Unbinding message handler for gateway %s", gatewayID)
	t.mu.Lock()
	delete(t.gatewayMessageHandlers, gatewayID)
	t.mu.Unlock()
	return nil
}
