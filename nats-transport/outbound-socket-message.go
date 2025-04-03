package natstransport

import (
	"bytes"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/vmihailenco/msgpack/v5"
)

const OutboundSocketMessageTimeout = 30 * time.Second
const OutboundMessageChunkSize = InboundMessageChunkSize

type OutboundMessageHeader struct {
	AckSubject string `msgpack:"ackSubject"`
}

type OutboundMessageAck struct {
	ChunkSubject string `msgpack:"chunkSubject"`
}

type OutboundMessageChunk struct {
	Index int    `msgpack:"index"`
	Data  []byte `msgpack:"data"`
	Error string `msgpack:"error"`
	IsEOF bool   `msgpack:"end"`
}

func (t *NatsTransport) OutboundSocketMessage(socketID string, messageData []byte) error {
	outboundMessageSubject := namespace("socket", socketID)

	ackSubject := nats.NewInbox()

	messageHeaderData, err := msgpack.Marshal(OutboundMessageHeader{
		AckSubject: ackSubject,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal message header: %w", err)
	}

	ackSub, err := t.NatsConnection.SubscribeSync(ackSubject)
	if err != nil {
		return fmt.Errorf("failed to subscribe to ack subject: %w", err)
	}

	if err := t.NatsConnection.Publish(outboundMessageSubject, messageHeaderData); err != nil {
		return fmt.Errorf("failed to publish message header: %w", err)
	}

	ack, err := ackSub.NextMsg(OutboundSocketMessageTimeout)
	if err != nil {
		return fmt.Errorf("failed to receive ack message: %w", err)
	}
	if err := ackSub.Unsubscribe(); err != nil {
		return fmt.Errorf("failed to unsubscribe from ack subject: %w", err)
	}

	messageAck := &OutboundMessageAck{}
	if err := msgpack.Unmarshal(ack.Data, messageAck); err != nil {
		return fmt.Errorf("failed to unmarshal ack message: %w", err)
	}

	for i := 0; ; i += 1 {
		chunkInnerDataLen := min(len(messageData), OutboundMessageChunkSize)
		chunkInnerData := messageData[:chunkInnerDataLen]
		messageData = messageData[chunkInnerDataLen:]
		isEOF := len(messageData) == 0

		chunkData, err := msgpack.Marshal(OutboundMessageChunk{
			Index: i,
			Data:  chunkInnerData,
			IsEOF: isEOF,
		})
		if err != nil {
			return fmt.Errorf("failed to marshal chunk data: %w", err)
		}
		if err := t.NatsConnection.Publish(messageAck.ChunkSubject, chunkData); err != nil {
			return fmt.Errorf("failed to publish chunk data: %w", err)
		}

		if isEOF {
			break
		}
	}

	return nil
}

func (t *NatsTransport) BindOutboundSocketMessage(socketID string, handler func(messageData []byte) error) error {
	outboundMessageSubject := namespace("socket", socketID)

	sub, err := t.NatsConnection.Subscribe(outboundMessageSubject, func(msg *nats.Msg) {
		if err := t.handleOutboundSocketMessage(msg, handler); err != nil {
			panic(err)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to bind outbound socket message: %w", err)
	}

	unbinders, ok := t.unbindOutboundSocketMessage[socketID]
	if !ok {
		unbinders = []func() error{}
	}
	unbinders = append(unbinders, func() error {
		return sub.Unsubscribe()
	})
	t.unbindOutboundSocketMessage[socketID] = unbinders

	return nil
}

func (t *NatsTransport) handleOutboundSocketMessage(msg *nats.Msg, handler func(messageData []byte) error) error {
	messageHeader := &OutboundMessageHeader{}
	if err := msgpack.Unmarshal(msg.Data, messageHeader); err != nil {
		return fmt.Errorf("failed to unmarshal message header: %w", err)
	}

	chunkSubject := nats.NewInbox()

	messageAckData, err := msgpack.Marshal(OutboundMessageAck{
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
		chunk, err := chunkSub.NextMsg(OutboundSocketMessageTimeout)
		if err != nil {
			return fmt.Errorf("failed to receive chunk message: %w", err)
		}

		messageChunk := &OutboundMessageChunk{}
		if err := msgpack.Unmarshal(chunk.Data, messageChunk); err != nil {
			return fmt.Errorf("failed to unmarshal chunk message: %w", err)
		}

		if _, err := messageBuf.Write(messageChunk.Data); err != nil {
			return fmt.Errorf("failed to write message chunk: %w", err)
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

		err := handler(messageBuf.Bytes())
		if err != nil {
			panic(err)
		}
	}()

	return nil
}

func (t *NatsTransport) UnbindOutboundSocketMessage(socketID string) error {
	unbinders, ok := t.unbindOutboundSocketMessage[socketID]
	if !ok {
		return nil
	}

	for _, unbinder := range unbinders {
		if err := unbinder(); err != nil {
			return fmt.Errorf("failed to unbind outbound socket message: %w", err)
		}
	}

	delete(t.unbindOutboundSocketMessage, socketID)

	return nil
}
