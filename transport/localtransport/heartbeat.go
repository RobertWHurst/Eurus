package localtransport

// SendServiceHeartbeat calls stored service heartbeat handlers directly.
func (t *LocalTransport) SendServiceHeartbeat(serviceID string, gatewayIDs []string) error {
	transportLocalDebug.Tracef("Sending service heartbeat from %s to %d gateways", serviceID, len(gatewayIDs))

	t.mu.RLock()
	handlers := make([]func(serviceID string), len(t.serviceHeartbeatHandlers))
	copy(handlers, t.serviceHeartbeatHandlers)
	t.mu.RUnlock()

	for _, handler := range handlers {
		handler(serviceID)
	}

	return nil
}

// BindServiceHeartbeat stores a handler for service heartbeats.
func (t *LocalTransport) BindServiceHeartbeat(gatewayID string, handler func(serviceID string)) error {
	transportLocalDebug.Trace("Binding service heartbeat handler")
	t.mu.Lock()
	t.serviceHeartbeatHandlers = append(t.serviceHeartbeatHandlers, handler)
	t.mu.Unlock()
	return nil
}

// UnbindServiceHeartbeat clears service heartbeat handlers.
func (t *LocalTransport) UnbindServiceHeartbeat() error {
	transportLocalDebug.Trace("Unbinding service heartbeat handlers")
	t.mu.Lock()
	t.serviceHeartbeatHandlers = nil
	t.mu.Unlock()
	return nil
}

// SendGatewayHeartbeat calls stored gateway heartbeat handlers directly.
func (t *LocalTransport) SendGatewayHeartbeat(gatewayID string, serviceIDs []string) error {
	transportLocalDebug.Tracef("Sending gateway heartbeat from %s to %d services", gatewayID, len(serviceIDs))

	t.mu.RLock()
	handlers := make([]func(gatewayID string), len(t.gatewayHeartbeatHandlers))
	copy(handlers, t.gatewayHeartbeatHandlers)
	t.mu.RUnlock()

	for _, handler := range handlers {
		handler(gatewayID)
	}

	return nil
}

// BindGatewayHeartbeat stores a handler for gateway heartbeats.
func (t *LocalTransport) BindGatewayHeartbeat(serviceID string, handler func(gatewayID string)) error {
	transportLocalDebug.Trace("Binding gateway heartbeat handler")
	t.mu.Lock()
	t.gatewayHeartbeatHandlers = append(t.gatewayHeartbeatHandlers, handler)
	t.mu.Unlock()
	return nil
}

// UnbindGatewayHeartbeat clears gateway heartbeat handlers.
func (t *LocalTransport) UnbindGatewayHeartbeat() error {
	transportLocalDebug.Trace("Unbinding gateway heartbeat handlers")
	t.mu.Lock()
	t.gatewayHeartbeatHandlers = nil
	t.mu.Unlock()
	return nil
}
