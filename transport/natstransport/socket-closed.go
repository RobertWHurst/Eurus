package natstransport

import (
	"context"

	"github.com/RobertWHurst/eurus"
	"github.com/coder/websocket"
	"github.com/nats-io/nats.go"
	"github.com/telemetryos/go-debug/debug"
	"github.com/vmihailenco/msgpack/v5"
)

var (
	transportNatsSocketClosedDebug = debug.Bind("eurus:transport:nats:socket-closed")
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

	envelopeBytes, err := msgpack.Marshal(&SocketClosedEnvelope{
		SocketID: socketID,
		Status:   status,
		Reason:   reason,
	})
	if err != nil {
		transportNatsSocketClosedDebug.Tracef("Failed to marshal envelope: %v", err)
		return err
	}

	if err := t.NatsConnection.Publish(namespace("socket", "closed"), envelopeBytes); err != nil {
		transportNatsSocketClosedDebug.Tracef("Failed to publish: %v", err)
		return err
	}

	return nil
}

// SubscribeSocketClosures subscribes to socket closure broadcasts.
func (t *NatsTransport) SubscribeSocketClosures(ctx context.Context, handler func(socketID string, status websocket.StatusCode, reason string)) (eurus.Subscription, error) {
	transportNatsSocketClosedDebug.Trace("Subscribing to socket closures")

	return t.subscribeAsync(ctx, namespace("socket", "closed"), func(msg *nats.Msg) {
		envelope := &SocketClosedEnvelope{}
		if err := msgpack.Unmarshal(msg.Data, envelope); err != nil {
			transportNatsSocketClosedDebug.Tracef("Discarding malformed socket closure: %v", err)
			return
		}

		transportNatsSocketClosedDebug.Tracef("Received socket closed event for %s (status: %d, reason: %s)",
			envelope.SocketID, envelope.Status, envelope.Reason)
		handler(envelope.SocketID, envelope.Status, envelope.Reason)
	})
}
