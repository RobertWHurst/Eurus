package natstransport

import (
	"context"
	"errors"

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

// HandleSocketClosures handles socket closed events until ctx is canceled.
func (t *NatsTransport) HandleSocketClosures(ctx context.Context, ready chan<- struct{}, handler func(socketID string, status websocket.StatusCode, reason string)) error {
	transportNatsSocketClosedDebug.Trace("Handling socket closures")

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
	defer sub.Unsubscribe()

	if err := flushWithContext(ctx, t.NatsConnection); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}

	transportNatsSocketClosedDebug.Trace("Socket closed handler bound successfully")
	signalReady(ready)
	<-ctx.Done()
	return nil
}
