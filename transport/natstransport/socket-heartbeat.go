package natstransport

import (
	"github.com/nats-io/nats.go"
	"github.com/telemetryos/go-debug/debug"
	"github.com/vmihailenco/msgpack/v5"
)

var (
	transportNatsSocketHeartbeatDebug = debug.Bind("eurus:transport:nats:socket-heartbeat")
)

type SocketHeartbeatEnvelope struct {
	SocketID string `msgpack:"socketID"`
}

// HeartbeatSocket broadcasts that a socket is alive
func (t *NatsTransport) HeartbeatSocket(socketID string) error {
	transportNatsSocketHeartbeatDebug.Tracef("Broadcasting heartbeat for socket: %s", socketID)

	subject := namespace("socket", "heartbeat")

	envelope := &SocketHeartbeatEnvelope{
		SocketID: socketID,
	}

	envelopeBytes, err := msgpack.Marshal(envelope)
	if err != nil {
		transportNatsSocketHeartbeatDebug.Tracef("Failed to marshal envelope: %v", err)
		return err
	}

	if err := t.NatsConnection.Publish(subject, envelopeBytes); err != nil {
		transportNatsSocketHeartbeatDebug.Tracef("Failed to publish: %v", err)
		return err
	}

	return nil
}

// BindSocketHeartbeat binds handler for socket heartbeat events
func (t *NatsTransport) BindSocketHeartbeat(handler func(socketID string)) error {
	transportNatsSocketHeartbeatDebug.Trace("Binding socket heartbeat handler")

	subject := namespace("socket", "heartbeat")

	sub, err := t.NatsConnection.Subscribe(subject, func(msg *nats.Msg) {
		envelope := &SocketHeartbeatEnvelope{}
		if err := msgpack.Unmarshal(msg.Data, envelope); err != nil {
			transportNatsSocketHeartbeatDebug.Tracef("Failed to unmarshal envelope: %v", err)
			return
		}

		handler(envelope.SocketID)
	})

	if err != nil {
		transportNatsSocketHeartbeatDebug.Tracef("Failed to subscribe: %v", err)
		return err
	}

	t.unbindSocketHeartbeat = func() error {
		transportNatsSocketHeartbeatDebug.Trace("Unbinding socket heartbeat handler")
		return sub.Unsubscribe()
	}

	transportNatsSocketHeartbeatDebug.Trace("Socket heartbeat handler bound successfully")
	return nil
}

// UnbindSocketHeartbeat unbinds the socket heartbeat handler
func (t *NatsTransport) UnbindSocketHeartbeat() error {
	if t.unbindSocketHeartbeat != nil {
		return t.unbindSocketHeartbeat()
	}
	return nil
}
