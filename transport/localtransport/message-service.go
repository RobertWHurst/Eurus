package localtransport

import (
	"net/http"

	"github.com/coder/websocket"
)

// MessageService sends a message from gateway to service
func (t *LocalTransport) MessageService(serviceID, gatewayID, socketID string, headers http.Header, msgType websocket.MessageType, msgData []byte) error {
	transportLocalMessageDebug.Tracef("Sending message to service %s from gateway %s socket %s", serviceID, gatewayID, socketID)

	t.mu.RLock()
	handler, ok := t.serviceMessageHandlers[serviceID]
	t.mu.RUnlock()

	if ok {
		transportLocalMessageDebug.Tracef("Found handler for service %s, calling handler", serviceID)
		handler(gatewayID, socketID, headers, msgType, msgData)
		transportLocalMessageDebug.Tracef("Handler for service %s completed", serviceID)
	} else {
		transportLocalMessageDebug.Tracef("No handler found for service %s", serviceID)
	}

	return nil
}

// BindMessageService binds handler for messages coming to a service
func (t *LocalTransport) BindMessageService(serviceID string, handler func(gatewayID, socketID string, headers http.Header, msgType websocket.MessageType, msgData []byte)) error {
	transportLocalMessageDebug.Tracef("Binding message handler for service %s", serviceID)
	t.mu.Lock()
	t.serviceMessageHandlers[serviceID] = handler
	t.mu.Unlock()
	return nil
}

// UnbindMessageService unbinds the message handler for a service
func (t *LocalTransport) UnbindMessageService(serviceID string) error {
	transportLocalMessageDebug.Tracef("Unbinding message handler for service %s", serviceID)
	t.mu.Lock()
	delete(t.serviceMessageHandlers, serviceID)
	t.mu.Unlock()
	return nil
}
