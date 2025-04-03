package natstransport

import (
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/vmihailenco/msgpack/v5"
)

type SocketCloseMessage struct {
	Reason int `msgpack:"reason"`
}

func (t *NatsTransport) SocketClose(socketID string, reason int) error {
	closeSubject := namespace("close", socketID)

	closeMessageData, err := msgpack.Marshal(SocketCloseMessage{
		Reason: reason,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal close message: %w", err)
	}

	if err := t.NatsConnection.Publish(closeSubject, closeMessageData); err != nil {
		return fmt.Errorf("failed to publish close message: %w", err)
	}

	return nil
}

func (t *NatsTransport) BindSocketClose(socketID string, handler func(reason int) error) error {
	closeSubject := namespace("close", socketID)

	sub, err := t.NatsConnection.Subscribe(closeSubject, func(msg *nats.Msg) {
		message := &SocketCloseMessage{}
		if err := msgpack.Unmarshal(msg.Data, message); err != nil {
			fmt.Printf("failed to unmarshal close message: %v\n", err)
			return
		}

		if err := handler(message.Reason); err != nil {
			fmt.Printf("failed to handle close message: %v\n", err)
		}
	})

	if err != nil {
		return fmt.Errorf("failed to subscribe to close subject: %w", err)
	}

	if _, ok := t.unbindSocketClose[socketID]; !ok {
		t.unbindSocketClose[socketID] = []func() error{}
	}
	t.unbindSocketClose[socketID] = append(t.unbindSocketClose[socketID], sub.Unsubscribe)

	return nil
}

func (t *NatsTransport) UnbindSocketClose(socketID string) error {
	if unbind, ok := t.unbindSocketClose[socketID]; ok {
		for _, u := range unbind {
			if err := u(); err != nil {
				return fmt.Errorf("failed to unbind socket close: %w", err)
			}
		}
		delete(t.unbindSocketClose, socketID)
	}

	return nil
}
