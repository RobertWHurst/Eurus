package localtransport

import (
	"context"

	"github.com/coder/websocket"
)

// ClosedSocket broadcasts that a socket has been closed
func (t *LocalTransport) ClosedSocket(socketID string, status websocket.StatusCode, reason string) error {
	transportLocalMessageDebug.Tracef("Broadcasting socket closed: %s (status: %d, reason: %s)", socketID, status, reason)

	t.mu.RLock()
	handlers := make([]func(socketID string, status websocket.StatusCode, reason string), 0, len(t.socketClosedHandlers))
	for _, handler := range t.socketClosedHandlers {
		handlers = append(handlers, handler)
	}
	t.mu.RUnlock()

	for _, handler := range handlers {
		handler(socketID, status, reason)
	}

	return nil
}

// HandleSocketClosures handles socket closed events until ctx is canceled.
func (t *LocalTransport) HandleSocketClosures(ctx context.Context, ready chan<- struct{}, handler func(socketID string, status websocket.StatusCode, reason string)) error {
	transportLocalMessageDebug.Trace("Handling socket closures")
	t.mu.Lock()
	id := t.registerHandler()
	t.socketClosedHandlers[id] = handler
	t.mu.Unlock()
	signalReady(ready)

	<-ctx.Done()
	transportLocalMessageDebug.Trace("Unbinding socket closed handler")
	t.mu.Lock()
	delete(t.socketClosedHandlers, id)
	t.mu.Unlock()
	return nil
}
