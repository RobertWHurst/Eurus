package localtransport

import (
	"github.com/coder/websocket"
)

// ClosedSocket broadcasts that a socket has been closed
func (t *LocalTransport) ClosedSocket(socketID string, status websocket.StatusCode, reason string) error {
	transportLocalMessageDebug.Tracef("Broadcasting socket closed: %s (status: %d, reason: %s)", socketID, status, reason)

	t.mu.RLock()
	handlers := make([]func(socketID string, status websocket.StatusCode, reason string), len(t.socketClosedHandlers))
	copy(handlers, t.socketClosedHandlers)
	t.mu.RUnlock()

	for _, handler := range handlers {
		handler(socketID, status, reason)
	}

	return nil
}

// BindSocketClosed binds handler for socket closed events
func (t *LocalTransport) BindSocketClosed(handler func(socketID string, status websocket.StatusCode, reason string)) error {
	transportLocalMessageDebug.Trace("Binding socket closed handler")
	t.mu.Lock()
	t.socketClosedHandlers = append(t.socketClosedHandlers, handler)
	t.mu.Unlock()
	return nil
}

// UnbindSocketClosed unbinds the socket closed handler
func (t *LocalTransport) UnbindSocketClosed() error {
	transportLocalMessageDebug.Trace("Unbinding socket closed handler")
	t.mu.Lock()
	t.socketClosedHandlers = nil
	t.mu.Unlock()
	return nil
}
