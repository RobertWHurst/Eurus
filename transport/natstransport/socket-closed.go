package natstransport

import (
	"github.com/coder/websocket"
	"github.com/nats-io/nats.go"
	"github.com/telemetrytv/trace"
	"github.com/vmihailenco/msgpack/v5"
)

var (
	transportNatsSocketClosedDebug = trace.Bind("eurus:transport:nats:socket-closed")
)

type SocketClosedEnvelope struct {
	SocketID string               `msgpack:"socketID"`
	Status   websocket.StatusCode `msgpack:"status"`
	Reason   string               `msgpack:"reason"`
}

// ClosedSocket broadcasts that a socket has been closed
func (t *NatsTransport) ClosedSocket(socketID string, status websocket.StatusCode, reason string) error {
	transportNatsSocketClosedDebug.Tracef("Broadcasting socket closed: %s (status: %d, reason: %s)",
		socketID, status, reason)

	subject := namespace("socket", "closed")

	envelope := &SocketClosedEnvelope{
		SocketID: socketID,
		Status:   status,
		Reason:   reason,
	}

	envelopeBytes, err := msgpack.Marshal(envelope)
	if err != nil {
		transportNatsSocketClosedDebug.Tracef("Failed to marshal envelope: %v", err)
		return err
	}

	if err := t.NatsConnection.Publish(subject, envelopeBytes); err != nil {
		transportNatsSocketClosedDebug.Tracef("Failed to publish: %v", err)
		return err
	}

	transportNatsSocketClosedDebug.Trace("Socket closed broadcast sent successfully")
	return nil
}

// BindSocketClosed binds handler for socket closed events
func (t *NatsTransport) BindSocketClosed(handler func(socketID string, status websocket.StatusCode, reason string)) error {
	transportNatsSocketClosedDebug.Trace("Binding socket closed handler")

	subject := namespace("socket", "closed")

	sub, err := t.NatsConnection.Subscribe(subject, func(msg *nats.Msg) {
		envelope := &SocketClosedEnvelope{}
		if err := msgpack.Unmarshal(msg.Data, envelope); err != nil {
			transportNatsSocketClosedDebug.Tracef("Failed to unmarshal envelope: %v", err)
			return
		}

		transportNatsSocketClosedDebug.Tracef("Received socket closed event for %s (status: %d, reason: %s)",
			envelope.SocketID, envelope.Status, envelope.Reason)

		handler(envelope.SocketID, envelope.Status, envelope.Reason)
	})

	if err != nil {
		transportNatsSocketClosedDebug.Tracef("Failed to subscribe: %v", err)
		return err
	}

	t.unbindSocketClosed = func() error {
		transportNatsSocketClosedDebug.Trace("Unbinding socket closed handler")
		return sub.Unsubscribe()
	}

	transportNatsSocketClosedDebug.Trace("Socket closed handler bound successfully")
	return nil
}

// UnbindSocketClosed unbinds the socket closed handler
func (t *NatsTransport) UnbindSocketClosed() error {
	if t.unbindSocketClosed != nil {
		return t.unbindSocketClosed()
	}
	return nil
}
