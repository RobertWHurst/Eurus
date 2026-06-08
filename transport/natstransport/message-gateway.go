package natstransport

import (
	"context"
	"sync"

	"github.com/RobertWHurst/velaros"
	"github.com/coder/websocket"
	"github.com/nats-io/nats.go"
	"github.com/vmihailenco/msgpack/v5"
)

type GatewayMessageHeader struct {
	SocketID    string                `msgpack:"socketID"`
	MessageType websocket.MessageType `msgpack:"messageType"`
}

type GatewayMessagePayload struct {
	RawData []byte         `msgpack:"rawData"`
	Data    []byte         `msgpack:"data"`
	Meta    map[string]any `msgpack:"meta"`
}

// GatewayMessageInit is the first frame of a service->gateway message. When
// Stream is false the marshaled GatewayMessagePayload is carried inline in
// Payload, delivering the whole message in a single request/reply round trip.
// When Stream is true, Payload is empty and the chunk-streaming protocol
// follows on the subject returned in the MessageAck — used only for payloads
// larger than MaxChunkSize.
type GatewayMessageInit struct {
	Header  *GatewayMessageHeader `msgpack:"header"`
	Stream  bool                  `msgpack:"stream"`
	Payload []byte                `msgpack:"payload,omitempty"`
}

// MessageGateway sends a message from service to gateway
func (t *NatsTransport) MessageGateway(gatewayID string, socketID string, msg *velaros.SocketMessage) error {
	transportNatsMessageDebug.Tracef("Sending message to gateway %s for socket %s", gatewayID, socketID)

	subject := namespace("gateway", gatewayID, "message")

	header := &GatewayMessageHeader{
		SocketID:    socketID,
		MessageType: msg.Type,
	}

	payload := &GatewayMessagePayload{
		RawData: msg.RawData,
		Data:    msg.Data,
		Meta:    msg.Meta,
	}
	payloadBytes, err := msgpack.Marshal(payload)
	if err != nil {
		transportNatsMessageDebug.Tracef("Failed to marshal payload: %v", err)
		return err
	}

	// Fast path: payloads that fit in a single chunk are delivered inline with
	// the header in one request/reply round trip, avoiding the per-chunk
	// handshake and the per-message ephemeral subscription.
	if len(payloadBytes) <= MaxChunkSize {
		initBytes, err := msgpack.Marshal(&GatewayMessageInit{
			Header:  header,
			Stream:  false,
			Payload: payloadBytes,
		})
		if err != nil {
			transportNatsMessageDebug.Tracef("Failed to marshal inline init: %v", err)
			return err
		}
		transportNatsMessageDebug.Tracef("Sending inline message (%d bytes)", len(payloadBytes))
		if _, err := t.NatsConnection.Request(subject, initBytes, MessageTimeout); err != nil {
			transportNatsMessageDebug.Tracef("Inline message failed: %v", err)
			return err
		}
		transportNatsMessageDebug.Trace("Inline message sent successfully")
		return nil
	}

	// Streaming path: announce intent to stream and obtain a chunk subject.
	initBytes, err := msgpack.Marshal(&GatewayMessageInit{Header: header, Stream: true})
	if err != nil {
		transportNatsMessageDebug.Tracef("Failed to marshal streaming init: %v", err)
		return err
	}

	transportNatsMessageDebug.Trace("Sending message header, waiting for ack")
	ackMsg, err := t.NatsConnection.Request(subject, initBytes, MessageTimeout)
	if err != nil {
		transportNatsMessageDebug.Tracef("Failed to get ack: %v", err)
		return err
	}

	ack := &MessageAck{}
	if err := msgpack.Unmarshal(ackMsg.Data, ack); err != nil {
		transportNatsMessageDebug.Tracef("Failed to unmarshal ack: %v", err)
		return err
	}

	chunkSubject := ack.ChunkSubject
	transportNatsMessageDebug.Tracef("Received chunk subject: %s", chunkSubject)

	transportNatsMessageDebug.Tracef("Streaming %d bytes", len(payloadBytes))
	offset := 0
	index := 0
	for offset < len(payloadBytes) {
		end := offset + MaxChunkSize
		if end > len(payloadBytes) {
			end = len(payloadBytes)
		}

		chunk := &MessageChunk{
			Index: index,
			Data:  payloadBytes[offset:end],
			IsEOF: false,
		}

		chunkBytes, err := msgpack.Marshal(chunk)
		if err != nil {
			transportNatsMessageDebug.Tracef("Failed to marshal chunk: %v", err)
			return err
		}

		transportNatsMessageDebug.Tracef("Sending chunk %d (%d bytes)", index, len(chunk.Data))
		messageChunkAckMsg, err := t.NatsConnection.Request(chunkSubject, chunkBytes, MessageTimeout)
		if err != nil {
			transportNatsMessageDebug.Tracef("Failed to publish chunk: %v", err)
			return err
		}

		messageChunkAck := &MessageChunkAck{}
		if err := msgpack.Unmarshal(messageChunkAckMsg.Data, messageChunkAck); err != nil {
			transportNatsMessageDebug.Tracef("Failed to unmarshal chunk ack: %v", err)
			return err
		}

		transportNatsMessageDebug.Tracef("Received chunk ack for index %d (EOF: %v)", messageChunkAck.Index, messageChunkAck.IsEOF)

		offset = end
		index++
	}

	eofChunk := &MessageChunk{
		Index: index,
		IsEOF: true,
	}
	eofBytes, err := msgpack.Marshal(eofChunk)
	if err != nil {
		transportNatsMessageDebug.Tracef("Failed to marshal EOF chunk: %v", err)
		return err
	}

	transportNatsMessageDebug.Trace("Sending EOF chunk")
	messageChunkAckMsg, err := t.NatsConnection.Request(chunkSubject, eofBytes, MessageTimeout)
	if err != nil {
		transportNatsMessageDebug.Tracef("Failed to publish EOF chunk: %v", err)
		return err
	}

	messageChunkAck := &MessageChunkAck{}
	if err := msgpack.Unmarshal(messageChunkAckMsg.Data, messageChunkAck); err != nil {
		transportNatsMessageDebug.Tracef("Failed to unmarshal EOF chunk ack: %v", err)
		return err
	}

	transportNatsMessageDebug.Trace("Message sent successfully")
	return nil
}

// BindMessageGateway binds handler for messages coming to a gateway
func (t *NatsTransport) BindMessageGateway(gatewayID string, handler func(socketID string, msg *velaros.SocketMessage)) error {
	transportNatsMessageDebug.Tracef("Binding message handler for gateway %s", gatewayID)

	subject := namespace("gateway", gatewayID, "message")

	sub, err := t.NatsConnection.SubscribeSync(subject)
	if err != nil {
		transportNatsMessageDebug.Tracef("Failed to subscribe: %v", err)
		return err
	}

	// Each inbound message triggers a synchronous, multi-round-trip chunk
	// reassembly bounded by MessageTimeout. A single goroutine would let one
	// slow/stalled reply block delivery to every other socket on this gateway
	// (head-of-line blocking), so dispatch across a pool of workers with a
	// bounded channel for backpressure. This mirrors BindMessageService.
	const messageWorkerCount = 32

	workerCh := make(chan *nats.Msg, messageWorkerCount)
	var workerWg sync.WaitGroup
	workerWg.Add(messageWorkerCount)
	for range messageWorkerCount {
		go func() {
			defer workerWg.Done()
			for msg := range workerCh {
				assembled, header, err := t.parseGatewayMessage(msg)
				if err != nil {
					transportNatsMessageDebug.Tracef("Error parsing gateway message: %v", err)
					continue
				}
				t.handleGatewayMessage(assembled, header, handler)
			}
		}()
	}

	doneCh := make(chan struct{})

	go func() {
		defer close(doneCh)
		defer close(workerCh)
		for {
			msg, err := sub.NextMsgWithContext(context.Background())
			if err != nil {
				break
			}
			workerCh <- msg
		}
	}()

	t.mu.Lock()
	// Capture any prior unbind so a re-bind without an intervening Unbind does
	// not leak the old subscription and worker pool. The closure blocks on
	// <-doneCh + workerWg.Wait(), so it must be invoked after releasing t.mu.
	previousUnbind := t.unbindMessageGateway[gatewayID]
	t.unbindMessageGateway[gatewayID] = func() error {
		transportNatsMessageDebug.Tracef("Unbinding message handler for gateway %s", gatewayID)
		err := sub.Unsubscribe()
		// Wait for the reader loop to exit (which closes workerCh) and then for
		// all workers to drain any buffered/in-flight messages, preserving the
		// pre-pool guarantee that unbind blocks until parse/handle work finishes.
		<-doneCh
		workerWg.Wait()
		return err
	}
	t.mu.Unlock()

	if previousUnbind != nil {
		if err := previousUnbind(); err != nil {
			transportNatsMessageDebug.Tracef("Failed to unbind previous message handler for gateway %s: %v", gatewayID, err)
		}
	}

	transportNatsMessageDebug.Trace("Message handler bound successfully")
	return nil
}

func (t *NatsTransport) parseGatewayMessage(msg *nats.Msg) ([]byte, *GatewayMessageHeader, error) {
	init := &GatewayMessageInit{}
	if err := msgpack.Unmarshal(msg.Data, init); err != nil {
		transportNatsMessageDebug.Tracef("Failed to unmarshal init: %v", err)
		return nil, nil, err
	}
	header := init.Header
	if header == nil {
		transportNatsMessageDebug.Trace("Rejecting gateway message with nil header")
		return nil, nil, errNilMessageHeader
	}

	transportNatsMessageDebug.Tracef("Received message header for socket %s", header.SocketID)

	// Inline fast path: payload arrived with the header. Ack and return it.
	if !init.Stream {
		if err := t.NatsConnection.Publish(msg.Reply, nil); err != nil {
			transportNatsMessageDebug.Tracef("Failed to ack inline message: %v", err)
			return nil, nil, err
		}
		transportNatsMessageDebug.Tracef("Received inline message (%d bytes)", len(init.Payload))
		return init.Payload, header, nil
	}

	chunkSubject := nats.NewInbox()
	chunkSub, err := t.NatsConnection.SubscribeSync(chunkSubject)
	if err != nil {
		transportNatsMessageDebug.Tracef("Failed to subscribe to chunks: %v", err)
		return nil, nil, err
	}
	defer chunkSub.Unsubscribe()

	ack := &MessageAck{
		ChunkSubject: chunkSubject,
	}
	ackBytes, err := msgpack.Marshal(ack)
	if err != nil {
		transportNatsMessageDebug.Tracef("Failed to marshal ack: %v", err)
		return nil, nil, err
	}

	transportNatsMessageDebug.Tracef("Sending ack with chunk subject: %s", chunkSubject)
	if err := t.NatsConnection.Publish(msg.Reply, ackBytes); err != nil {
		transportNatsMessageDebug.Tracef("Failed to send ack: %v", err)
		return nil, nil, err
	}

	var assembled []byte
	for {
		chunkMsg, err := chunkSub.NextMsg(MessageTimeout)
		if err != nil {
			transportNatsMessageDebug.Tracef("Error receiving chunk: %v", err)
			return nil, nil, err
		}
		chunk := &MessageChunk{}
		if err := msgpack.Unmarshal(chunkMsg.Data, chunk); err != nil {
			transportNatsMessageDebug.Tracef("Failed to unmarshal chunk: %v", err)
			return nil, nil, err
		}

		chunkAck := &MessageChunkAck{
			Index: chunk.Index,
			IsEOF: chunk.IsEOF,
		}
		chunkAckBytes, err := msgpack.Marshal(chunkAck)
		if err != nil {
			transportNatsMessageDebug.Tracef("Failed to marshal chunk ack: %v", err)
			return nil, nil, err
		}
		if err := t.NatsConnection.Publish(chunkMsg.Reply, chunkAckBytes); err != nil {
			transportNatsMessageDebug.Tracef("Failed to send chunk ack: %v", err)
			return nil, nil, err
		}

		transportNatsMessageDebug.Tracef("Received chunk %d (%d bytes, EOF: %v)", chunk.Index, len(chunk.Data), chunk.IsEOF)

		assembled = append(assembled, chunk.Data...)

		if chunk.IsEOF {
			transportNatsMessageDebug.Tracef("Received EOF, parsed message (%d bytes)", len(assembled))
			break
		}
	}

	return assembled, header, nil
}

func (t *NatsTransport) handleGatewayMessage(assembled []byte, header *GatewayMessageHeader, handler func(socketID string, msg *velaros.SocketMessage)) {
	payload := &GatewayMessagePayload{}
	if err := msgpack.Unmarshal(assembled, payload); err != nil {
		transportNatsMessageDebug.Tracef("Failed to unmarshal payload: %v", err)
		return
	}

	handler(header.SocketID, &velaros.SocketMessage{
		Type:    header.MessageType,
		RawData: payload.RawData,
		Data:    payload.Data,
		Meta:    payload.Meta,
	})

	transportNatsMessageDebug.Trace("Message delivered successfully")
}

// UnbindMessageGateway unbinds the message handler for a gateway
func (t *NatsTransport) UnbindMessageGateway(gatewayID string) error {
	t.mu.Lock()
	unbind, ok := t.unbindMessageGateway[gatewayID]
	if ok {
		delete(t.unbindMessageGateway, gatewayID)
	}
	t.mu.Unlock()
	if !ok {
		return nil
	}
	return unbind()
}
