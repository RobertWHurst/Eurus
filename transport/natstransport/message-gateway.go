package natstransport

import (
	"github.com/RobertWHurst/velaros"
	"github.com/coder/websocket"
	"github.com/nats-io/nats.go"
	"github.com/vmihailenco/msgpack/v5"
)

type GatewayMessageHeader struct {
	SocketID                string                `msgpack:"socketID"`
	MessageType             websocket.MessageType `msgpack:"messageType"`
	SocketAssociatedValues  map[string]any        `msgpack:"socketAssociatedValues"`
	MessageAssociatedValues map[string]any        `msgpack:"messageAssociatedValues"`
}

// MessageGateway sends a message from service to gateway
func (t *NatsTransport) MessageGateway(gatewayID string, socketID string, msg *velaros.SocketMessage) error {
	transportNatsMessageDebug.Tracef("Sending message to gateway %s for socket %s", gatewayID, socketID)

	subject := namespace("gateway", gatewayID, "message")

	header := &GatewayMessageHeader{
		SocketID:                socketID,
		MessageType:             msg.Type,
		SocketAssociatedValues:  msg.SocketAssociatedValues,
		MessageAssociatedValues: msg.MessageAssociatedValues,
	}

	headerBytes, err := msgpack.Marshal(header)
	if err != nil {
		transportNatsMessageDebug.Tracef("Failed to marshal header: %v", err)
		return err
	}

	transportNatsMessageDebug.Trace("Sending message header, waiting for ack")
	ackMsg, err := t.NatsConnection.Request(subject, headerBytes, MessageTimeout)
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

	transportNatsMessageDebug.Tracef("Streaming %d bytes", len(msg.Data))
	offset := 0
	index := 0
	for offset < len(msg.Data) {
		end := offset + MaxChunkSize
		if end > len(msg.Data) {
			end = len(msg.Data)
		}

		chunk := &MessageChunk{
			Index: index,
			Data:  msg.Data[offset:end],
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

	sub, err := t.NatsConnection.Subscribe(subject, func(msg *nats.Msg) {
		if err := t.handleGatewayMessage(msg, handler); err != nil {
			transportNatsMessageDebug.Tracef("Error handling gateway message: %v", err)
		}
	})

	if err != nil {
		transportNatsMessageDebug.Tracef("Failed to subscribe: %v", err)
		return err
	}

	t.unbindMessageGateway[gatewayID] = func() error {
		transportNatsMessageDebug.Tracef("Unbinding message handler for gateway %s", gatewayID)
		return sub.Unsubscribe()
	}

	transportNatsMessageDebug.Trace("Message handler bound successfully")
	return nil
}

func (t *NatsTransport) handleGatewayMessage(msg *nats.Msg, handler func(socketID string, msg *velaros.SocketMessage)) error {
	header := &GatewayMessageHeader{}
	if err := msgpack.Unmarshal(msg.Data, header); err != nil {
		transportNatsMessageDebug.Tracef("Failed to unmarshal header: %v", err)
		return err
	}

	transportNatsMessageDebug.Tracef("Received message header for socket %s", header.SocketID)

	chunkSubject := nats.NewInbox()
	chunkSub, err := t.NatsConnection.SubscribeSync(chunkSubject)
	if err != nil {
		transportNatsMessageDebug.Tracef("Failed to subscribe to chunks: %v", err)
		return err
	}
	defer chunkSub.Unsubscribe()

	ack := &MessageAck{
		ChunkSubject: chunkSubject,
	}
	ackBytes, err := msgpack.Marshal(ack)
	if err != nil {
		transportNatsMessageDebug.Tracef("Failed to marshal ack: %v", err)
		return err
	}

	transportNatsMessageDebug.Tracef("Sending ack with chunk subject: %s", chunkSubject)
	if err := t.NatsConnection.Publish(msg.Reply, ackBytes); err != nil {
		transportNatsMessageDebug.Tracef("Failed to send ack: %v", err)
		return err
	}

	var assembled []byte
	for {
		chunkMsg, err := chunkSub.NextMsg(MessageTimeout)
		if err != nil {
			transportNatsMessageDebug.Tracef("Error receiving chunk: %v", err)
			return err
		}
		chunk := &MessageChunk{}
		if err := msgpack.Unmarshal(chunkMsg.Data, chunk); err != nil {
			transportNatsMessageDebug.Tracef("Failed to unmarshal chunk: %v", err)
			return err
		}

		chunkAck := &MessageChunkAck{
			Index: chunk.Index,
			IsEOF: chunk.IsEOF,
		}
		chunkAckBytes, err := msgpack.Marshal(chunkAck)
		if err != nil {
			transportNatsMessageDebug.Tracef("Failed to marshal chunk ack: %v", err)
			return err
		}
		if err := t.NatsConnection.Publish(chunkMsg.Reply, chunkAckBytes); err != nil {
			transportNatsMessageDebug.Tracef("Failed to send chunk ack: %v", err)
			return err
		}

		transportNatsMessageDebug.Tracef("Received chunk %d (%d bytes, EOF: %v)", chunk.Index, len(chunk.Data), chunk.IsEOF)

		assembled = append(assembled, chunk.Data...)

		if chunk.IsEOF {
			transportNatsMessageDebug.Tracef("Received EOF, delivering assembled message (%d bytes)", len(assembled))
			break
		}
	}

	handler(header.SocketID, &velaros.SocketMessage{
		Type:                    header.MessageType,
		Data:                    assembled,
		SocketAssociatedValues:  header.SocketAssociatedValues,
		MessageAssociatedValues: header.MessageAssociatedValues,
	})

	transportNatsMessageDebug.Trace("Message delivered successfully")
	return nil
}

// UnbindMessageGateway unbinds the message handler for a gateway
func (t *NatsTransport) UnbindMessageGateway(gatewayID string) error {
	if unbind, ok := t.unbindMessageGateway[gatewayID]; ok {
		return unbind()
	}
	return nil
}
