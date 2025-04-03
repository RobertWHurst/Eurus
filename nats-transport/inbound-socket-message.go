package natstransport

import (
	"bytes"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/vmihailenco/msgpack/v5"
)

const InboundSocketMessageTimeout = 30 * time.Second
const InboundMessageChunkSize = 1024 * 16

type InboundMessageHeader struct {
	SocketID   string `msgpack:"socketId"`
	AckSubject string `msgpack:"ackSubject"`
}

type InboundMessageAck struct {
	ChunkSubject string `msgpack:"chunkSubject"`
}

type InboundMessageChunk struct {
	Index int    `msgpack:"index"`
	Data  []byte `msgpack:"data"`
	Error string `msgpack:"error"`
	IsEOF bool   `msgpack:"end"`
}

func (t *NatsTransport) InboundSocketMessage(serviceName string, socketID string, messageData []byte) error {
	messageHeaderSubject := namespace("service", serviceName)

	ackSubject := nats.NewInbox()

	messageHeaderData, err := msgpack.Marshal(InboundMessageHeader{
		SocketID:   socketID,
		AckSubject: ackSubject,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal message header: %w", err)
	}

	ackSub, err := t.NatsConnection.SubscribeSync(ackSubject)
	if err != nil {
		return fmt.Errorf("failed to subscribe to ack subject: %w", err)
	}

	if err := t.NatsConnection.Publish(messageHeaderSubject, messageHeaderData); err != nil {
		return fmt.Errorf("failed to publish message header: %w", err)
	}

	ack, err := ackSub.NextMsg(InboundSocketMessageTimeout)
	if err != nil {
		return fmt.Errorf("failed to receive ack message: %w", err)
	}
	if err := ackSub.Unsubscribe(); err != nil {
		return fmt.Errorf("failed to unsubscribe from ack subject: %w", err)
	}

	messageAck := &InboundMessageAck{}
	if err := msgpack.Unmarshal(ack.Data, messageAck); err != nil {
		return fmt.Errorf("failed to unmarshal ack message: %w", err)
	}

	for i := 0; ; i += 1 {
		chunkInnerDataLen := min(len(messageData), InboundMessageChunkSize)
		chunkInnerData := messageData[:chunkInnerDataLen]
		messageData = messageData[chunkInnerDataLen:]
		isEOF := len(messageData) == 0

		chunkData, err := msgpack.Marshal(InboundMessageChunk{
			Index: i,
			Data:  chunkInnerData,
			IsEOF: isEOF,
		})
		if err != nil {
			return fmt.Errorf("failed to marshal message chunk: %w", err)
		}

		if err := t.NatsConnection.Publish(messageAck.ChunkSubject, chunkData); err != nil {
			return fmt.Errorf("failed to publish message chunk: %w", err)
		}

		if isEOF {
			break
		}
	}

	return nil
}

func (t *NatsTransport) BindInboundSocketMessage(serviceName string, handler func(socketID string, messageData []byte) error) error {
	messageHeaderSubject := namespace("service", serviceName)

	sub, err := t.NatsConnection.QueueSubscribe(messageHeaderSubject, messageHeaderSubject, func(msg *nats.Msg) {
		if err := t.handleInboundSocketMessage(msg, handler); err != nil {
			panic(err)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to bind inbound socket message: %w", err)
	}

	unbinders, ok := t.unbindInboundSocketMessage[serviceName]
	if !ok {
		unbinders = []func() error{}
	}
	unbinders = append(unbinders, func() error {
		return sub.Unsubscribe()
	})
	t.unbindInboundSocketMessage[serviceName] = unbinders

	return nil
}

func (t *NatsTransport) handleInboundSocketMessage(msg *nats.Msg, handler func(socketID string, messageData []byte) error) error {
	messageHeader := &InboundMessageHeader{}
	if err := msgpack.Unmarshal(msg.Data, messageHeader); err != nil {
		return fmt.Errorf("failed to unmarshal message header: %w", err)
	}

	chunkSubject := nats.NewInbox()

	messageAckData, err := msgpack.Marshal(InboundMessageAck{
		ChunkSubject: chunkSubject,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal message ack: %w", err)
	}

	chunkSub, err := t.NatsConnection.SubscribeSync(chunkSubject)
	if err != nil {
		return fmt.Errorf("failed to subscribe to chunk subject: %w", err)
	}

	if err := t.NatsConnection.Publish(messageHeader.AckSubject, messageAckData); err != nil {
		return fmt.Errorf("failed to publish message ack: %w", err)
	}

	messageBuf := bytes.Buffer{}
	for {
		chunk, err := chunkSub.NextMsg(InboundSocketMessageTimeout)
		if err != nil {
			return fmt.Errorf("failed to receive chunk message: %w", err)
		}

		messageChunk := &InboundMessageChunk{}
		if err := msgpack.Unmarshal(chunk.Data, messageChunk); err != nil {
			return fmt.Errorf("failed to unmarshal message chunk: %w", err)
		}

		if _, err := messageBuf.Write(messageChunk.Data); err != nil {
			return fmt.Errorf("failed to write message chunk data: %w", err)
		}

		if messageChunk.IsEOF {
			break
		}
	}

	if err := chunkSub.Unsubscribe(); err != nil {
		return fmt.Errorf("failed to unsubscribe from chunk subject: %w", err)
	}

	func() {
		defer func() {
			if r := recover(); r != nil {
				// fixme: handle panic
			}
		}()

		err := handler(messageHeader.SocketID, messageBuf.Bytes())
		if err != nil {
			panic(err)
		}
	}()

	return nil
}

func (t *NatsTransport) UnbindInboundSocketMessage(serviceName string) error {
	unbinders, ok := t.unbindInboundSocketMessage[serviceName]
	if !ok {
		return nil
	}

	for _, unbinder := range unbinders {
		if err := unbinder(); err != nil {
			return fmt.Errorf("failed to unbind inbound socket message: %w", err)
		}
	}

	delete(t.unbindInboundSocketMessage, serviceName)

	return nil
}
