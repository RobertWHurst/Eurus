package natstransport

import (
	"context"
	"net/http"

	"github.com/RobertWHurst/eurus"
	"github.com/RobertWHurst/velaros"
	"github.com/coder/websocket"
	"github.com/nats-io/nats.go"
	"github.com/vmihailenco/msgpack/v5"
)

// ServiceMessageEnvelope carries a gateway-to-service message. Payload holds
// the marshaled ServiceMessagePayload when it fits in a single NATS message;
// otherwise Streamed is true and the payload arrives over the chunk protocol.
type ServiceMessageEnvelope struct {
	GatewayID   string                `msgpack:"gatewayID"`
	SocketID    string                `msgpack:"socketID"`
	Headers     map[string][]string   `msgpack:"headers"`
	RemoteAddr  string                `msgpack:"remoteAddr"`
	MessageType websocket.MessageType `msgpack:"messageType"`
	Payload     []byte                `msgpack:"payload,omitempty"`
	Streamed    bool                  `msgpack:"streamed,omitempty"`
}

type ServiceMessagePayload struct {
	RawData []byte         `msgpack:"rawData"`
	Data    []byte         `msgpack:"data"`
	Meta    map[string]any `msgpack:"meta"`
}

// MessageService sends a message from gateway to service.
func (t *NatsTransport) MessageService(serviceID, gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) error {
	transportNatsMessageDebug.Tracef("Sending message to service %s from gateway %s socket %s", serviceID, gatewayID, socketID)

	payloadBytes, err := msgpack.Marshal(&ServiceMessagePayload{
		RawData: msg.RawData,
		Data:    msg.Data,
		Meta:    msg.Meta,
	})
	if err != nil {
		return err
	}

	subject := namespace("service", serviceID, "message")
	return t.sendEnvelope(subject, func(inlinePayload []byte, streamed bool) any {
		return &ServiceMessageEnvelope{
			GatewayID:   gatewayID,
			SocketID:    socketID,
			Headers:     map[string][]string(connInfo.Headers),
			RemoteAddr:  connInfo.RemoteAddr,
			MessageType: msg.Type,
			Payload:     inlinePayload,
			Streamed:    streamed,
		}
	}, payloadBytes)
}

// SubscribeServiceMessages subscribes to messages addressed to a service
// instance.
func (t *NatsTransport) SubscribeServiceMessages(ctx context.Context, serviceID string, handler func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage)) (eurus.Subscription, error) {
	transportNatsMessageDebug.Tracef("Subscribing to messages for service %s", serviceID)

	subject := namespace("service", serviceID, "message")
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
			envelope := &ServiceMessageEnvelope{}
			if err := msgpack.Unmarshal(msg.Data, envelope); err != nil {
				return "", false, nil, err
			}
			deliver := func(ctx context.Context) {
				t.deliverServiceMessage(ctx, msg, envelope, handler)
			}
			return envelope.SocketID, envelope.Streamed, deliver, nil
		})
	}), nil
}

func (t *NatsTransport) deliverServiceMessage(ctx context.Context, msg *nats.Msg, envelope *ServiceMessageEnvelope, handler func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage)) {
	payloadBytes := envelope.Payload
	if envelope.Streamed {
		var err error
		payloadBytes, err = t.receiveChunks(ctx, msg.Reply)
		if err != nil {
			transportNatsMessageDebug.Tracef("Failed to receive streamed payload for socket %s: %v", envelope.SocketID, err)
			return
		}
	}

	payload := &ServiceMessagePayload{}
	if err := msgpack.Unmarshal(payloadBytes, payload); err != nil {
		transportNatsMessageDebug.Tracef("Failed to unmarshal payload: %v", err)
		return
	}

	connInfo := &velaros.ConnectionInfo{
		Headers:    http.Header(envelope.Headers),
		RemoteAddr: envelope.RemoteAddr,
	}
	handler(envelope.GatewayID, envelope.SocketID, connInfo, &velaros.SocketMessage{
		Type:    envelope.MessageType,
		RawData: payload.RawData,
		Data:    payload.Data,
		Meta:    payload.Meta,
	})
}
