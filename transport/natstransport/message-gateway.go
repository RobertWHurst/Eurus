package natstransport

import (
	"context"

	"github.com/RobertWHurst/eurus"
	"github.com/RobertWHurst/velaros"
	"github.com/coder/websocket"
	"github.com/nats-io/nats.go"
	"github.com/vmihailenco/msgpack/v5"
)

// GatewayMessageEnvelope carries a service-to-gateway message. Payload holds
// the marshaled GatewayMessagePayload when it fits in a single NATS message;
// otherwise Streamed is true and the payload arrives over the chunk protocol.
type GatewayMessageEnvelope struct {
	SocketID    string                `msgpack:"socketID"`
	MessageType websocket.MessageType `msgpack:"messageType"`
	Payload     []byte                `msgpack:"payload,omitempty"`
	Streamed    bool                  `msgpack:"streamed,omitempty"`
}

type GatewayMessagePayload struct {
	RawData []byte         `msgpack:"rawData"`
	Data    []byte         `msgpack:"data"`
	Meta    map[string]any `msgpack:"meta"`
}

// MessageGateway sends a message from service to gateway.
func (t *NatsTransport) MessageGateway(gatewayID string, socketID string, msg *velaros.SocketMessage) error {
	transportNatsMessageDebug.Tracef("Sending message to gateway %s for socket %s", gatewayID, socketID)

	payloadBytes, err := msgpack.Marshal(&GatewayMessagePayload{
		RawData: msg.RawData,
		Data:    msg.Data,
		Meta:    msg.Meta,
	})
	if err != nil {
		return err
	}

	subject := namespace("gateway", gatewayID, "message")
	return t.sendEnvelope(subject, func(inlinePayload []byte, streamed bool) any {
		return &GatewayMessageEnvelope{
			SocketID:    socketID,
			MessageType: msg.Type,
			Payload:     inlinePayload,
			Streamed:    streamed,
		}
	}, payloadBytes)
}

// SubscribeGatewayMessages subscribes to messages addressed to a gateway.
func (t *NatsTransport) SubscribeGatewayMessages(ctx context.Context, gatewayID string, handler func(socketID string, msg *velaros.SocketMessage)) (eurus.Subscription, error) {
	transportNatsMessageDebug.Tracef("Subscribing to messages for gateway %s", gatewayID)

	subject := namespace("gateway", gatewayID, "message")
	sub, err := t.NatsConnection.SubscribeSync(subject)
	if err != nil {
		return nil, err
	}
	if err := t.flush(ctx); err != nil {
		_ = sub.Unsubscribe()
		return nil, err
	}

	return eurus.SubscriptionFunc(func(ctx context.Context) error {
		return t.serveMessages(ctx, sub, subject, func(msg *nats.Msg) (string, bool, func(context.Context), error) {
			envelope := &GatewayMessageEnvelope{}
			if err := msgpack.Unmarshal(msg.Data, envelope); err != nil {
				return "", false, nil, err
			}
			deliver := func(ctx context.Context) {
				t.deliverGatewayMessage(ctx, msg, envelope, handler)
			}
			return envelope.SocketID, envelope.Streamed, deliver, nil
		})
	}), nil
}

func (t *NatsTransport) deliverGatewayMessage(ctx context.Context, msg *nats.Msg, envelope *GatewayMessageEnvelope, handler func(socketID string, msg *velaros.SocketMessage)) {
	payloadBytes := envelope.Payload
	if envelope.Streamed {
		var err error
		payloadBytes, err = t.receiveChunks(ctx, msg.Reply)
		if err != nil {
			transportNatsMessageDebug.Tracef("Failed to receive streamed payload for socket %s: %v", envelope.SocketID, err)
			return
		}
	}

	payload := &GatewayMessagePayload{}
	if err := msgpack.Unmarshal(payloadBytes, payload); err != nil {
		transportNatsMessageDebug.Tracef("Failed to unmarshal payload: %v", err)
		return
	}

	handler(envelope.SocketID, &velaros.SocketMessage{
		Type:    envelope.MessageType,
		RawData: payload.RawData,
		Data:    payload.Data,
		Meta:    payload.Meta,
	})
}
