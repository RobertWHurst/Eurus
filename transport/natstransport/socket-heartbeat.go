package natstransport

import (
	"context"
	"time"

	"github.com/RobertWHurst/eurus"
	"github.com/nats-io/nats.go"
	"github.com/telemetryos/go-debug/debug"
	"github.com/vmihailenco/msgpack/v5"
)

var (
	transportNatsSocketHeartbeatDebug = debug.Bind("eurus:transport:nats:socket-heartbeat")
)

const heartbeatRequestTimeout = 5 * time.Second

// heartbeatMaxBatchSize bounds how many socket IDs travel in one heartbeat
// request so envelopes stay well under the NATS payload limit.
const heartbeatMaxBatchSize = 4096

type SocketHeartbeatEnvelope struct {
	ServiceID string   `msgpack:"serviceID"`
	SocketIDs []string `msgpack:"socketIDs"`
}

type SocketHeartbeatReply struct {
	DeadSocketIDs []string `msgpack:"deadSocketIDs"`
}

// HeartbeatSockets asks a gateway which of the given sockets are no longer
// alive, and returns their IDs. Large socket sets are split into batches; an
// error on any batch aborts and indicates the gateway is unreachable.
func (t *NatsTransport) HeartbeatSockets(gatewayID string, serviceID string, socketIDs []string) ([]string, error) {
	if len(socketIDs) == 0 {
		return nil, nil
	}
	transportNatsSocketHeartbeatDebug.Tracef("Heartbeating %d sockets from service %s to gateway %s", len(socketIDs), serviceID, gatewayID)

	subject := namespace("gateway", gatewayID, "socket-heartbeat")

	var dead []string
	for start := 0; start < len(socketIDs); start += heartbeatMaxBatchSize {
		end := min(start+heartbeatMaxBatchSize, len(socketIDs))

		envelopeBytes, err := msgpack.Marshal(&SocketHeartbeatEnvelope{
			ServiceID: serviceID,
			SocketIDs: socketIDs[start:end],
		})
		if err != nil {
			return nil, err
		}

		replyMsg, err := t.NatsConnection.Request(subject, envelopeBytes, heartbeatRequestTimeout)
		if err != nil {
			transportNatsSocketHeartbeatDebug.Tracef("Heartbeat request to gateway %s failed: %v", gatewayID, err)
			return nil, err
		}

		reply := &SocketHeartbeatReply{}
		if err := msgpack.Unmarshal(replyMsg.Data, reply); err != nil {
			return nil, err
		}
		dead = append(dead, reply.DeadSocketIDs...)
	}

	return dead, nil
}

// SubscribeSocketHeartbeats subscribes a gateway to heartbeat requests. The
// handler returns the subset of socketIDs that are no longer alive.
func (t *NatsTransport) SubscribeSocketHeartbeats(ctx context.Context, gatewayID string, handler func(serviceID string, socketIDs []string) []string) (eurus.Subscription, error) {
	transportNatsSocketHeartbeatDebug.Tracef("Subscribing to socket heartbeats for gateway %s", gatewayID)

	subject := namespace("gateway", gatewayID, "socket-heartbeat")
	return t.subscribeAsync(ctx, subject, func(msg *nats.Msg) {
		envelope := &SocketHeartbeatEnvelope{}
		if err := msgpack.Unmarshal(msg.Data, envelope); err != nil {
			transportNatsSocketHeartbeatDebug.Tracef("Discarding malformed heartbeat request: %v", err)
			return
		}

		dead := handler(envelope.ServiceID, envelope.SocketIDs)

		replyBytes, err := msgpack.Marshal(&SocketHeartbeatReply{DeadSocketIDs: dead})
		if err != nil {
			transportNatsSocketHeartbeatDebug.Tracef("Failed to marshal reply: %v", err)
			return
		}
		if err := msg.Respond(replyBytes); err != nil {
			transportNatsSocketHeartbeatDebug.Tracef("Failed to send reply: %v", err)
		}
	})
}
