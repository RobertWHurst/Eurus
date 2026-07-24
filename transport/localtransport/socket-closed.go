package localtransport

import (
	"context"

	"github.com/RobertWHurst/eurus/v2"
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

// SubscribeSocketClosures subscribes to socket closure broadcasts.
func (t *LocalTransport) SubscribeSocketClosures(_ context.Context, handler func(socketID string, status websocket.StatusCode, reason string)) (eurus.Subscription, error) {
	transportLocalMessageDebug.Trace("Subscribing to socket closures")
	t.mu.Lock()
	id := t.registerHandlerID()
	t.socketClosedHandlers[id] = handler
	t.mu.Unlock()

	return untilDone(func() {
		transportLocalMessageDebug.Trace("Unsubscribing from socket closures")
		t.mu.Lock()
		delete(t.socketClosedHandlers, id)
		t.mu.Unlock()
	}), nil
}
