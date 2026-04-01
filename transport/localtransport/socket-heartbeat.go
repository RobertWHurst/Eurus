package localtransport

// HeartbeatSocketService sends a batched heartbeat to a specific service instance
func (t *LocalTransport) HeartbeatSocketService(serviceID string, socketIDs []string) error {
	transportLocalMessageDebug.Tracef("Sending batched heartbeat to service %s with %d sockets", serviceID, len(socketIDs))

	t.mu.RLock()
	handler, ok := t.socketHeartbeatServiceHandlers[serviceID]
	t.mu.RUnlock()

	if ok {
		handler(socketIDs)
	}

	return nil
}

// BindSocketHeartbeatService binds handler for batched socket heartbeat events targeted at a service
func (t *LocalTransport) BindSocketHeartbeatService(serviceID string, handler func(socketIDs []string)) error {
	transportLocalMessageDebug.Tracef("Binding socket heartbeat handler for service %s", serviceID)
	t.mu.Lock()
	t.socketHeartbeatServiceHandlers[serviceID] = handler
	t.mu.Unlock()
	return nil
}

// UnbindSocketHeartbeatService unbinds the socket heartbeat handler for a service
func (t *LocalTransport) UnbindSocketHeartbeatService(serviceID string) error {
	transportLocalMessageDebug.Tracef("Unbinding socket heartbeat handler for service %s", serviceID)
	t.mu.Lock()
	delete(t.socketHeartbeatServiceHandlers, serviceID)
	t.mu.Unlock()
	return nil
}
