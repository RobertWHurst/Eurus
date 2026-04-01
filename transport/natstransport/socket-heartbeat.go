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
	SocketIDs []string `msgpack:"socketIDs"`
}

// HeartbeatSocketService sends a batched heartbeat to a specific service instance
func (t *NatsTransport) HeartbeatSocketService(serviceID string, socketIDs []string) error {
	transportNatsSocketHeartbeatDebug.Tracef("Sending batched heartbeat to service %s with %d sockets", serviceID, len(socketIDs))

	subject := namespace("service", serviceID, "socket-heartbeat")

	envelope := &SocketHeartbeatEnvelope{
		SocketIDs: socketIDs,
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

// BindSocketHeartbeatService binds handler for batched socket heartbeat events targeted at a service
func (t *NatsTransport) BindSocketHeartbeatService(serviceID string, handler func(socketIDs []string)) error {
	transportNatsSocketHeartbeatDebug.Tracef("Binding socket heartbeat handler for service %s", serviceID)

	subject := namespace("service", serviceID, "socket-heartbeat")

	sub, err := t.NatsConnection.Subscribe(subject, func(msg *nats.Msg) {
		envelope := &SocketHeartbeatEnvelope{}
		if err := msgpack.Unmarshal(msg.Data, envelope); err != nil {
			transportNatsSocketHeartbeatDebug.Tracef("Failed to unmarshal envelope: %v", err)
			return
		}

		handler(envelope.SocketIDs)
	})

	if err != nil {
		transportNatsSocketHeartbeatDebug.Tracef("Failed to subscribe: %v", err)
		return err
	}

	t.unbindSocketHeartbeatService[serviceID] = func() error {
		transportNatsSocketHeartbeatDebug.Tracef("Unbinding socket heartbeat handler for service %s", serviceID)
		return sub.Unsubscribe()
	}

	transportNatsSocketHeartbeatDebug.Tracef("Socket heartbeat handler bound for service %s", serviceID)
	return nil
}

// UnbindSocketHeartbeatService unbinds the socket heartbeat handler for a service
func (t *NatsTransport) UnbindSocketHeartbeatService(serviceID string) error {
	if unbind, ok := t.unbindSocketHeartbeatService[serviceID]; ok {
		if err := unbind(); err != nil {
			return err
		}
		delete(t.unbindSocketHeartbeatService, serviceID)
	}
	return nil
}
