package localtransport

// HeartbeatSocket broadcasts that a socket is alive
func (t *LocalTransport) HeartbeatSocket(socketID string) error {
	transportLocalMessageDebug.Tracef("Broadcasting heartbeat for socket: %s", socketID)

	t.mu.RLock()
	handlers := make([]func(socketID string), len(t.socketHeartbeatHandlers))
	copy(handlers, t.socketHeartbeatHandlers)
	t.mu.RUnlock()

	for _, handler := range handlers {
		handler(socketID)
	}

	return nil
}

// BindSocketHeartbeat binds handler for socket heartbeat events
func (t *LocalTransport) BindSocketHeartbeat(handler func(socketID string)) error {
	transportLocalMessageDebug.Trace("Binding socket heartbeat handler")
	t.mu.Lock()
	t.socketHeartbeatHandlers = append(t.socketHeartbeatHandlers, handler)
	t.mu.Unlock()
	return nil
}

// UnbindSocketHeartbeat unbinds the socket heartbeat handler
func (t *LocalTransport) UnbindSocketHeartbeat() error {
	transportLocalMessageDebug.Trace("Unbinding socket heartbeat handler")
	t.mu.Lock()
	t.socketHeartbeatHandlers = nil
	t.mu.Unlock()
	return nil
}
