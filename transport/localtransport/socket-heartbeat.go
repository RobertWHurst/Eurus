package localtransport

import (
	"context"
	"fmt"

	"github.com/RobertWHurst/eurus/v2"
)

// HeartbeatSockets asks a gateway which of the given sockets are no longer
// alive, and returns their IDs. An error indicates the gateway is unavailable.
func (t *LocalTransport) HeartbeatSockets(gatewayID string, serviceID string, socketIDs []string) ([]string, error) {
	transportLocalMessageDebug.Tracef("Heartbeating %d sockets from service %s to gateway %s", len(socketIDs), serviceID, gatewayID)

	t.mu.RLock()
	handler, ok := t.socketHeartbeatHandlers[gatewayID]
	t.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("gateway %s not available", gatewayID)
	}

	return handler(serviceID, socketIDs), nil
}

// SubscribeSocketHeartbeats subscribes a gateway to heartbeat requests. The
// handler returns the subset of socketIDs that are no longer alive.
func (t *LocalTransport) SubscribeSocketHeartbeats(_ context.Context, gatewayID string, handler func(serviceID string, socketIDs []string) []string) (eurus.Subscription, error) {
	transportLocalMessageDebug.Tracef("Subscribing to socket heartbeats for gateway %s", gatewayID)
	t.mu.Lock()
	t.socketHeartbeatHandlers[gatewayID] = handler
	t.mu.Unlock()

	return untilDone(func() {
		transportLocalMessageDebug.Tracef("Unsubscribing from socket heartbeats for gateway %s", gatewayID)
		t.mu.Lock()
		delete(t.socketHeartbeatHandlers, gatewayID)
		t.mu.Unlock()
	}), nil
}
