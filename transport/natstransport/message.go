package natstransport

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/RobertWHurst/eurus/v2"
	"github.com/coder/websocket"
	"github.com/nats-io/nats.go"
	"github.com/telemetryos/go-debug/debug"
	"github.com/vmihailenco/msgpack/v5"
)

var (
	transportNatsDebug        = debug.Bind("eurus:transport:nats")
	transportNatsMessageDebug = debug.Bind("eurus:transport:nats:message")
)

const MaxChunkSize = 1024 * 16
const MessageTimeout = 30 * time.Second

// slowConsumerReply is the ack error used when a socket's dispatch queue is
// full. Senders translate it to eurus.ErrSlowConsumer.
const slowConsumerReply = "slow consumer"

// slowConsumerStatus is the close status broadcast when a socket is dropped
// for overflowing its dispatch queue.
const slowConsumerStatus = websocket.StatusPolicyViolation

// MessageAck is the reply to a message envelope request. An empty ack means
// the message was accepted. ChunkSubject is set when the sender must stream
// the payload over the chunk protocol. Error is set when the message was
// rejected.
type MessageAck struct {
	ChunkSubject string `msgpack:"chunkSubject,omitempty"`
	Error        string `msgpack:"error,omitempty"`
}

type MessageChunk struct {
	Index int    `msgpack:"index"`
	Data  []byte `msgpack:"data"`
	IsEOF bool   `msgpack:"eof"`
}

type MessageChunkAck struct {
	Index int  `msgpack:"index"`
	IsEOF bool `msgpack:"eof"`
}

// sendEnvelope delivers a message envelope. When the envelope (with its
// payload inline) fits in a single NATS message it is sent with one
// request/reply round trip. Otherwise the payload is streamed over the chunk
// protocol negotiated by the initial request.
//
// makeEnvelope builds the envelope: with the payload embedded when streamed
// is false, or without it when streamed is true.
func (t *NatsTransport) sendEnvelope(subject string, makeEnvelope func(inlinePayload []byte, streamed bool) any, payloadBytes []byte) error {
	inlineEnvelopeBytes, err := msgpack.Marshal(makeEnvelope(payloadBytes, false))
	if err != nil {
		return err
	}

	if int64(len(inlineEnvelopeBytes)) <= t.NatsConnection.MaxPayload() {
		transportNatsMessageDebug.Tracef("Sending inline message (%d bytes)", len(inlineEnvelopeBytes))
		ackMsg, err := t.NatsConnection.Request(subject, inlineEnvelopeBytes, MessageTimeout)
		if err != nil {
			return err
		}
		_, err = parseMessageAck(ackMsg)
		return err
	}

	streamedEnvelopeBytes, err := msgpack.Marshal(makeEnvelope(nil, true))
	if err != nil {
		return err
	}

	transportNatsMessageDebug.Tracef("Payload too large for inline delivery, streaming %d bytes", len(payloadBytes))
	ackMsg, err := t.NatsConnection.Request(subject, streamedEnvelopeBytes, MessageTimeout)
	if err != nil {
		return err
	}
	ack, err := parseMessageAck(ackMsg)
	if err != nil {
		return err
	}
	if ack.ChunkSubject == "" {
		return errors.New("streamed message ack missing chunk subject")
	}

	return t.streamChunks(ack.ChunkSubject, payloadBytes)
}

func parseMessageAck(ackMsg *nats.Msg) (*MessageAck, error) {
	ack := &MessageAck{}
	if err := msgpack.Unmarshal(ackMsg.Data, ack); err != nil {
		return nil, err
	}
	if ack.Error == slowConsumerReply {
		return nil, eurus.ErrSlowConsumer
	}
	if ack.Error != "" {
		return nil, fmt.Errorf("message rejected: %s", ack.Error)
	}
	return ack, nil
}

// streamChunks sends payloadBytes over the chunk protocol, one acked chunk at
// a time, terminated by an EOF chunk.
func (t *NatsTransport) streamChunks(chunkSubject string, payloadBytes []byte) error {
	index := 0
	for offset := 0; offset < len(payloadBytes); index++ {
		end := min(offset+MaxChunkSize, len(payloadBytes))
		if err := t.sendChunk(chunkSubject, &MessageChunk{Index: index, Data: payloadBytes[offset:end]}); err != nil {
			return err
		}
		offset = end
	}
	return t.sendChunk(chunkSubject, &MessageChunk{Index: index, IsEOF: true})
}

func (t *NatsTransport) sendChunk(chunkSubject string, chunk *MessageChunk) error {
	chunkBytes, err := msgpack.Marshal(chunk)
	if err != nil {
		return err
	}
	ackMsg, err := t.NatsConnection.Request(chunkSubject, chunkBytes, MessageTimeout)
	if err != nil {
		return err
	}
	chunkAck := &MessageChunkAck{}
	return msgpack.Unmarshal(ackMsg.Data, chunkAck)
}

// receiveChunks performs the receiving side of the chunk protocol: it opens a
// private inbox, acks the initial request with the inbox subject, and
// assembles the streamed payload. The context bounds the whole transfer.
func (t *NatsTransport) receiveChunks(ctx context.Context, replySubject string) ([]byte, error) {
	chunkSubject := nats.NewInbox()
	chunkSub, err := t.NatsConnection.SubscribeSync(chunkSubject)
	if err != nil {
		return nil, err
	}
	defer func() { _ = chunkSub.Unsubscribe() }()

	ackBytes, err := msgpack.Marshal(&MessageAck{ChunkSubject: chunkSubject})
	if err != nil {
		return nil, err
	}
	if err := t.NatsConnection.Publish(replySubject, ackBytes); err != nil {
		return nil, err
	}

	transferCtx, cancel := context.WithTimeout(ctx, MessageTimeout)
	defer cancel()

	var assembled []byte
	for {
		chunkMsg, err := chunkSub.NextMsgWithContext(transferCtx)
		if err != nil {
			return nil, err
		}
		chunk := &MessageChunk{}
		if err := msgpack.Unmarshal(chunkMsg.Data, chunk); err != nil {
			return nil, err
		}

		chunkAckBytes, err := msgpack.Marshal(&MessageChunkAck{Index: chunk.Index, IsEOF: chunk.IsEOF})
		if err != nil {
			return nil, err
		}
		if err := t.NatsConnection.Publish(chunkMsg.Reply, chunkAckBytes); err != nil {
			return nil, err
		}

		assembled = append(assembled, chunk.Data...)

		if chunk.IsEOF {
			transportNatsMessageDebug.Tracef("Received EOF, assembled message (%d bytes)", len(assembled))
			return assembled, nil
		}
	}
}

// respondAck publishes a MessageAck to a reply subject, ignoring publish
// failures — the sender will time out and treat the message as undelivered.
func (t *NatsTransport) respondAck(replySubject string, ack *MessageAck) {
	if replySubject == "" {
		return
	}
	ackBytes, err := msgpack.Marshal(ack)
	if err != nil {
		transportNatsMessageDebug.Tracef("Failed to marshal ack: %v", err)
		return
	}
	if err := t.NatsConnection.Publish(replySubject, ackBytes); err != nil {
		transportNatsMessageDebug.Tracef("Failed to publish ack: %v", err)
	}
}

// serveMessages pumps a sync subscription, decoding each envelope with
// decode and dispatching it to a per-socket worker. Inline messages are
// acked from the pump on enqueue; streamed messages are acked by the worker
// when it begins the chunk transfer.
func (t *NatsTransport) serveMessages(
	ctx context.Context,
	sub *nats.Subscription,
	subject string,
	decode func(msg *nats.Msg) (socketID string, streamed bool, deliver func(ctx context.Context), decodeErr error),
) error {
	defer func() { _ = sub.Unsubscribe() }()

	dispatcher := newSocketDispatcher(t.dispatchQueueSize(), func(socketID string) {
		if err := t.ClosedSocket(socketID, slowConsumerStatus, "Slow consumer"); err != nil {
			transportNatsMessageDebug.Tracef("Failed to broadcast slow consumer closure for %s: %v", socketID, err)
		}
	})
	defer dispatcher.wait()

	for {
		msg, err := sub.NextMsgWithContext(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			transportNatsMessageDebug.Tracef("Message stream failed for %s: %v", subject, err)
			return err
		}

		socketID, streamed, deliver, err := decode(msg)
		if err != nil {
			transportNatsMessageDebug.Tracef("Error decoding message envelope: %v", err)
			t.respondAck(msg.Reply, &MessageAck{Error: "malformed envelope"})
			continue
		}

		accepted := dispatcher.dispatch(socketID, func() { deliver(ctx) })
		if !accepted {
			t.respondAck(msg.Reply, &MessageAck{Error: slowConsumerReply})
			continue
		}
		if !streamed {
			t.respondAck(msg.Reply, &MessageAck{})
		}
	}
}
