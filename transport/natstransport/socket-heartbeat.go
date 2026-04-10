package natstransport

import (
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/telemetryos/go-debug/debug"
	"github.com/vmihailenco/msgpack/v5"
)

var (
	transportNatsSocketHeartbeatDebug = debug.Bind("eurus:transport:nats:socket-heartbeat")
)

const heartbeatRequestTimeout = 5 * time.Second

type SocketHeartbeatEnvelope struct {
	ServiceID string `msgpack:"serviceID"`
	SocketID  string `msgpack:"socketID"`
}

type SocketHeartbeatReply struct {
	Alive bool `msgpack:"alive"`
}

// HeartbeatSocket sends a heartbeat request from a service to a gateway for the given socket.
// Returns nil if the gateway confirms the socket is alive, or an error if the socket
// is not found, the gateway is unavailable, or the request times out.
func (t *NatsTransport) HeartbeatSocket(gatewayID string, serviceID string, socketID string) error {
	transportNatsSocketHeartbeatDebug.Tracef("Sending socket heartbeat from service %s to gateway %s for socket %s", serviceID, gatewayID, socketID)

	subject := namespace("gateway", gatewayID, "socket-heartbeat")

	envelope := &SocketHeartbeatEnvelope{
		ServiceID: serviceID,
		SocketID:  socketID,
	}

	envelopeBytes, err := msgpack.Marshal(envelope)
	if err != nil {
		transportNatsSocketHeartbeatDebug.Tracef("Failed to marshal envelope: %v", err)
		return err
	}

	replyMsg, err := t.NatsConnection.Request(subject, envelopeBytes, heartbeatRequestTimeout)
	if err != nil {
		transportNatsSocketHeartbeatDebug.Tracef("Heartbeat request failed for socket %s: %v", socketID, err)
		return err
	}

	reply := &SocketHeartbeatReply{}
	if err := msgpack.Unmarshal(replyMsg.Data, reply); err != nil {
		transportNatsSocketHeartbeatDebug.Tracef("Failed to unmarshal reply: %v", err)
		return err
	}

	if !reply.Alive {
		return fmt.Errorf("socket %s not found in gateway %s", socketID, gatewayID)
	}

	return nil
}

// BindSocketHeartbeat subscribes to heartbeat requests for a gateway.
// The handler should return true if the socket is alive, false if it is not.
func (t *NatsTransport) BindSocketHeartbeat(gatewayID string, handler func(serviceID string, socketID string) bool) error {
	transportNatsSocketHeartbeatDebug.Tracef("Binding socket heartbeat handler for gateway %s", gatewayID)

	subject := namespace("gateway", gatewayID, "socket-heartbeat")

	sub, err := t.NatsConnection.Subscribe(subject, func(msg *nats.Msg) {
		envelope := &SocketHeartbeatEnvelope{}
		if err := msgpack.Unmarshal(msg.Data, envelope); err != nil {
			transportNatsSocketHeartbeatDebug.Tracef("Failed to unmarshal envelope: %v", err)
			return
		}

		alive := handler(envelope.ServiceID, envelope.SocketID)

		replyBytes, err := msgpack.Marshal(&SocketHeartbeatReply{Alive: alive})
		if err != nil {
			transportNatsSocketHeartbeatDebug.Tracef("Failed to marshal reply: %v", err)
			return
		}

		if err := msg.Respond(replyBytes); err != nil {
			transportNatsSocketHeartbeatDebug.Tracef("Failed to send reply: %v", err)
		}
	})

	if err != nil {
		transportNatsSocketHeartbeatDebug.Tracef("Failed to subscribe: %v", err)
		return err
	}

	t.unbindSocketHeartbeat[gatewayID] = func() error {
		transportNatsSocketHeartbeatDebug.Tracef("Unbinding socket heartbeat handler for gateway %s", gatewayID)
		return sub.Unsubscribe()
	}

	transportNatsSocketHeartbeatDebug.Tracef("Socket heartbeat handler bound for gateway %s", gatewayID)
	return nil
}

// UnbindSocketHeartbeat unbinds the socket heartbeat handler for a gateway
func (t *NatsTransport) UnbindSocketHeartbeat(gatewayID string) error {
	if unbind, ok := t.unbindSocketHeartbeat[gatewayID]; ok {
		if err := unbind(); err != nil {
			return err
		}
		delete(t.unbindSocketHeartbeat, gatewayID)
	}
	return nil
}
