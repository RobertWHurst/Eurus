package localtransport

import (
	"context"
	"fmt"
)

// HeartbeatSocket sends a heartbeat from a service to a gateway for the given socket.
// Returns nil if the gateway confirms the socket is alive, or an error if the socket
// is not found or the gateway is unavailable.
func (t *LocalTransport) HeartbeatSocket(gatewayID string, serviceID string, socketID string) error {
	transportLocalMessageDebug.Tracef("Sending socket heartbeat from service %s to gateway %s for socket %s", serviceID, gatewayID, socketID)

	t.mu.RLock()
	handler, ok := t.socketHeartbeatHandlers[gatewayID]
	t.mu.RUnlock()

	if !ok {
		return fmt.Errorf("gateway %s not available", gatewayID)
	}

	if !handler(serviceID, socketID) {
		return fmt.Errorf("socket %s not found in gateway %s", socketID, gatewayID)
	}

	return nil
}

// HandleSocketHeartbeats handles heartbeat requests targeted at a gateway.
// The handler should return true if the socket is alive, false if it is not.
func (t *LocalTransport) HandleSocketHeartbeats(ctx context.Context, ready chan<- struct{}, gatewayID string, handler func(serviceID string, socketID string) bool) error {
	transportLocalMessageDebug.Tracef("Handling socket heartbeats for gateway %s", gatewayID)
	t.mu.Lock()
	t.socketHeartbeatHandlers[gatewayID] = handler
	t.mu.Unlock()
	signalReady(ready)

	<-ctx.Done()
	transportLocalMessageDebug.Tracef("Unbinding socket heartbeat handler for gateway %s", gatewayID)
	t.mu.Lock()
	delete(t.socketHeartbeatHandlers, gatewayID)
	t.mu.Unlock()
	return nil
}
